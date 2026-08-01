package sango

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

type Runtime struct {
	wazeroRT        wazero.Runtime
	compiled        wazero.CompiledModule
	adapter         Adapter
	moduleHash      [32]byte
	closed          atomic.Bool
	moduleConfigMod func(wazero.ModuleConfig) wazero.ModuleConfig
	ownedCache      wazero.CompilationCache

	golden Snapshot
	warm   chan *Instance
}

type Option func(*config)

type config struct {
	poolSize            int
	wasi                bool
	moduleConfigMod     func(wazero.ModuleConfig) wazero.ModuleConfig
	compilationCache    wazero.CompilationCache
	compilationCacheDir string
	compilationWorkers  int
	forkParallelism     int
}

func WithPoolSize(n int) Option { return func(c *config) { c.poolSize = n } }

func WithWASI() Option { return func(c *config) { c.wasi = true } }

// WithModuleConfigModifier registers a hook that lets an adapter customise the
// wazero ModuleConfig used when a guest instance is (re)instantiated (e.g. to
// attach an FSConfig for preopened directories).
func WithModuleConfigModifier(f func(wazero.ModuleConfig) wazero.ModuleConfig) Option {
	return func(c *config) { c.moduleConfigMod = f }
}

func WithCompilationCache(cache wazero.CompilationCache) Option {
	return func(c *config) { c.compilationCache = cache }
}

func WithCompilationCacheDir(dir string) Option {
	return func(c *config) { c.compilationCacheDir = dir }
}

func WithCompilationWorkers(n int) Option {
	return func(c *config) { c.compilationWorkers = n }
}

func WithForkParallelism(n int) Option {
	return func(c *config) { c.forkParallelism = n }
}

func New(ctx context.Context, wasmBinary []byte, adapter Adapter, opts ...Option) (*Runtime, error) {
	cfg := &config{}
	for _, o := range opts {
		o(cfg)
	}

	cache := cfg.compilationCache
	cacheOwned := false
	if cache == nil && cfg.compilationCacheDir != "" {
		c, err := wazero.NewCompilationCacheWithDir(cfg.compilationCacheDir)
		if err != nil {
			return nil, fmt.Errorf("sango: open compilation cache dir %q: %w", cfg.compilationCacheDir, err)
		}
		cache = c
		cacheOwned = true
	}

	rtCfg := wazero.NewRuntimeConfig().
		WithCoreFeatures(api.CoreFeaturesV2 | experimental.CoreFeaturesExceptionHandling)
	if cache != nil {
		rtCfg = rtCfg.WithCompilationCache(cache)
	}
	wrt := wazero.NewRuntimeWithConfig(ctx, rtCfg)

	if cfg.wasi {
		wasi_snapshot_preview1.MustInstantiate(ctx, wrt)
	}

	compileCtx := ctx
	if cfg.compilationWorkers > 0 {
		compileCtx = experimental.WithCompilationWorkers(ctx, cfg.compilationWorkers)
	}
	compiled, err := wrt.CompileModule(compileCtx, wasmBinary)
	if err != nil {
		wrt.Close(ctx)
		if cacheOwned {
			cache.Close(ctx)
		}
		return nil, fmt.Errorf("sango: compile module: %w", err)
	}

	rt := &Runtime{
		wazeroRT:        wrt,
		compiled:        compiled,
		adapter:         adapter,
		moduleHash:      sha256.Sum256(wasmBinary),
		moduleConfigMod: cfg.moduleConfigMod,
		warm:            make(chan *Instance, max(cfg.poolSize, 1)),
	}
	if cacheOwned {
		rt.ownedCache = cache
	}

	seed, err := rt.instantiate(ctx)
	if err != nil {
		wrt.Close(ctx)
		if cacheOwned {
			cache.Close(ctx)
		}
		return nil, err
	}
	if err := adapter.Initialize(ctx, seed.mod); err != nil {
		seed.Release()
		wrt.Close(ctx)
		if cacheOwned {
			cache.Close(ctx)
		}
		return nil, fmt.Errorf("sango: adapter initialize: %w", err)
	}
	golden, err := rt.Snapshot(seed)
	if err != nil {
		seed.Release()
		wrt.Close(ctx)
		if cacheOwned {
			cache.Close(ctx)
		}
		return nil, fmt.Errorf("sango: golden snapshot: %w", err)
	}
	rt.golden = golden
	seed.Release()

	for i := 0; i < cfg.poolSize; i++ {
		inst, err := rt.Restore(ctx, rt.golden)
		if err != nil {
			rt.Close(ctx)
			return nil, fmt.Errorf("sango: warm pool: %w", err)
		}
		rt.warm <- inst
	}

	return rt, nil
}

func (r *Runtime) Snapshot(instance *Instance) (Snapshot, error) {
	instance.mu.Lock()
	defer instance.mu.Unlock()
	if instance.released {
		return nil, ErrReleased
	}
	if instance.busy {
		return nil, errors.New("sango: cannot snapshot while eval in progress")
	}

	mem := instance.mod.Memory()
	size := mem.Size()
	data, ok := mem.Read(0, size)
	if !ok {
		return nil, errors.New("sango: failed to read linear memory")
	}
	return encodeSnapshot(instance.adapter.ID(), r.moduleHash, data), nil
}

func (r *Runtime) Acquire(ctx context.Context) (*Instance, error) {
	select {
	case inst := <-r.warm:
		go func() {
			if refill, err := r.Restore(context.Background(), r.golden); err == nil {
				select {
				case r.warm <- refill:
				default:
					refill.Release()
				}
			}
		}()
		return inst, nil
	default:
		return r.Restore(ctx, r.golden)
	}
}

func (r *Runtime) Restore(ctx context.Context, s Snapshot) (*Instance, error) {
	adapterID, hash, memory, err := decodeSnapshot(s)
	if err != nil {
		return nil, err
	}
	if err := r.checkSnapshotIdentity(adapterID, hash); err != nil {
		return nil, err
	}
	return r.restoreDecoded(ctx, memory)
}

func (r *Runtime) checkSnapshotIdentity(adapterID string, hash [32]byte) error {
	if adapterID != r.adapter.ID() {
		return fmt.Errorf("sango: snapshot adapter %q does not match runtime adapter %q",
			adapterID, r.adapter.ID())
	}
	if hash != r.moduleHash {
		return fmt.Errorf("sango: snapshot was taken on a different wasm module build")
	}
	return nil
}

func (r *Runtime) restoreDecoded(ctx context.Context, memory []byte) (*Instance, error) {
	inst, err := r.instantiate(ctx)
	if err != nil {
		return nil, err
	}

	mem := inst.mod.Memory()
	if cur := mem.Size(); cur < uint32(len(memory)) {
		const pageSize = 65536
		delta := (uint32(len(memory)) - cur + pageSize - 1) / pageSize
		if _, ok := mem.Grow(delta); !ok {
			inst.Release()
			return nil, fmt.Errorf("sango: failed to grow memory for restore")
		}
	}
	if !mem.Write(0, memory) {
		inst.Release()
		return nil, fmt.Errorf("sango: failed to write snapshot into linear memory")
	}
	return inst, nil
}

func (r *Runtime) Close(ctx context.Context) error {
	r.closed.Store(true)
	for {
		select {
		case inst := <-r.warm:
			inst.Release()
		default:
			err := r.wazeroRT.Close(ctx)
			if r.ownedCache != nil {
				if cerr := r.ownedCache.Close(ctx); cerr != nil && err == nil {
					err = cerr
				}
				r.ownedCache = nil
			}
			return err
		}
	}
}

func (r *Runtime) instantiate(ctx context.Context) (*Instance, error) {
	if r.closed.Load() {
		return nil, errors.New("runtime is already closed")
	}
	modCfg := wazero.NewModuleConfig().WithName("").WithStartFunctions()
	if r.moduleConfigMod != nil {
		modCfg = r.moduleConfigMod(modCfg)
	}
	mod, err := r.wazeroRT.InstantiateModule(ctx, r.compiled, modCfg)
	if err != nil {
		return nil, fmt.Errorf("sango: instantiate: %w", err)
	}
	return &Instance{mod: mod, adapter: r.adapter}, nil
}
