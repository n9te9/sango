package sango

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

type Runtime struct {
	wazeroRT   wazero.Runtime
	compiled   wazero.CompiledModule
	adapter    Adapter
	moduleHash [32]byte
	closed     atomic.Bool

	golden Snapshot
	warm   chan *Instance
}

type Option func(*config)

type config struct {
	poolSize int
	wasi     bool
}

func WithPoolSize(n int) Option { return func(c *config) { c.poolSize = n } }

func WithWASI() Option { return func(c *config) { c.wasi = true } }

func New(ctx context.Context, wasmBinary []byte, adapter Adapter, opts ...Option) (*Runtime, error) {
	cfg := &config{}
	for _, o := range opts {
		o(cfg)
	}

	wrt := wazero.NewRuntime(ctx)

	if cfg.wasi {
		wasi_snapshot_preview1.MustInstantiate(ctx, wrt)
	}

	compiled, err := wrt.CompileModule(ctx, wasmBinary)
	if err != nil {
		wrt.Close(ctx)
		return nil, fmt.Errorf("sango: compile module: %w", err)
	}

	rt := &Runtime{
		wazeroRT:   wrt,
		compiled:   compiled,
		adapter:    adapter,
		moduleHash: sha256.Sum256(wasmBinary),
		warm:       make(chan *Instance, max(cfg.poolSize, 1)),
	}

	seed, err := rt.instantiate(ctx)
	if err != nil {
		wrt.Close(ctx)
		return nil, err
	}
	if err := adapter.Initialize(ctx, seed.mod); err != nil {
		seed.Release()
		wrt.Close(ctx)
		return nil, fmt.Errorf("sango: adapter initialize: %w", err)
	}
	golden, err := rt.Snapshot(seed)
	if err != nil {
		seed.Release()
		wrt.Close(ctx)
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
	if adapterID != r.adapter.ID() {
		return nil, fmt.Errorf("sango: snapshot adapter %q does not match runtime adapter %q",
			adapterID, r.adapter.ID())
	}
	if hash != r.moduleHash {
		return nil, fmt.Errorf("sango: snapshot was taken on a different wasm module build")
	}

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
			return r.wazeroRT.Close(ctx)
		}
	}
}

func (r *Runtime) instantiate(ctx context.Context) (*Instance, error) {
	if r.closed.Load() {
		return nil, errors.New("runtime is already closed")
	}
	mod, err := r.wazeroRT.InstantiateModule(ctx, r.compiled,
		wazero.NewModuleConfig().WithName("").WithStartFunctions())
	if err != nil {
		return nil, fmt.Errorf("sango: instantiate: %w", err)
	}
	return &Instance{mod: mod, adapter: r.adapter}, nil
}
