package sango_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/n9te9/sango"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name           string
		adapter        sango.Adapter
		opts           []sango.Option
		instanceFn     func(ctx context.Context) *sango.Instance
		initializeWasm []byte
		code           []byte
		want           *sango.Runtime
		wantErr        error
	}{
		{
			name: "success: sango.New() success and return sango runtime is pool size 1 and error is nil",
			adapter: &fakeAdapter{
				initializeError: nil,
				evalError:       nil,
				evalResult: sango.Result{
					Value: []byte(`example`),
					Err:   nil,
				},
			},
			opts:           []sango.Option{sango.WithPoolSize(1)},
			initializeWasm: minimalWasm,
			code:           []byte("console.log(\"example\")"),
			want:           &sango.Runtime{},
			wantErr:        nil,
		}, {
			name: "success: sango.New() success and return sango runtime has WASI and error is nil",
			adapter: &fakeAdapter{
				initializeError: nil,
				evalError:       nil,
				evalResult: sango.Result{
					Value: []byte(`example`),
					Err:   nil,
				},
			},
			opts:           []sango.Option{sango.WithWASI()},
			initializeWasm: minimalWasm,
			code:           []byte("console.log(\"example\")"),
			want:           &sango.Runtime{},
			wantErr:        nil,
		}, {
			name: "fail: sango.New() fail and return an error is failed to initialize code",
			adapter: &fakeAdapter{
				initializeError: nil,
				evalError:       nil,
				evalResult: sango.Result{
					Value: []byte(`example`),
					Err:   nil,
				},
			},
			initializeWasm: []byte(``),
			code:           []byte("console.log(\"example\")"),
			want:           nil,
			wantErr:        errors.New("sango: compile module: invalid magic number"),
		},
	}

	t.Parallel()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := sango.New(t.Context(), tt.initializeWasm, tt.adapter, tt.opts...)
			if err != nil {
				if tt.wantErr == nil {
					t.Fatalf("error expected nil, but got %v\n", err)
				}

				if err.Error() != tt.wantErr.Error() {
					t.Fatalf("error expected %v, but got %v\n", tt.wantErr, err)
				}
			}

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("error expected %v, but got nil\n", tt.wantErr)
				}
			}

			if d := cmp.Diff(got, tt.want, cmpopts.IgnoreUnexported(sango.Runtime{})); d != "" {
				t.Fatal(d)
			}
		})
	}
}

func TestRuntime_RestoreN(t *testing.T) {
	tests := []struct {
		name        string
		n           int
		parallelism int
		wantLen     int
	}{
		{name: "n=0 returns nil", n: 0, wantLen: 0},
		{name: "n=1 single fork", n: 1, wantLen: 1},
		{name: "n=8 fan-out with default parallelism", n: 8, wantLen: 8},
		{name: "n=8 with parallelism=2", n: 8, parallelism: 2, wantLen: 8},
	}

	t.Parallel()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			adptr := &fakeAdapter{evalResult: sango.Result{Value: []byte(`ok`)}}
			opts := []sango.Option{sango.WithPoolSize(0)}
			if tt.parallelism > 0 {
				opts = append(opts, sango.WithForkParallelism(tt.parallelism))
			}
			rt, err := sango.New(t.Context(), minimalWasm, adptr, opts...)
			if err != nil {
				t.Fatalf("New failed: %v", err)
			}

			seed, err := rt.Acquire(t.Context())
			if err != nil {
				t.Fatalf("Acquire failed: %v", err)
			}
			snap, err := rt.Snapshot(seed)
			if err != nil {
				t.Fatalf("Snapshot failed: %v", err)
			}
			seed.Release()

			forks, err := rt.RestoreN(t.Context(), snap, tt.n)
			if err != nil {
				t.Fatalf("RestoreN failed: %v", err)
			}
			if got := len(forks); got != tt.wantLen {
				t.Fatalf("len(forks)=%d, want %d", got, tt.wantLen)
			}
			for _, f := range forks {
				if f == nil {
					t.Fatal("RestoreN returned nil instance")
				}
				f.Release()
			}
		})
	}
}

func TestRuntime_AcquireN(t *testing.T) {
	adptr := &fakeAdapter{evalResult: sango.Result{Value: []byte(`ok`)}}
	rt, err := sango.New(t.Context(), minimalWasm, adptr, sango.WithPoolSize(2))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	insts, err := rt.AcquireN(t.Context(), 5)
	if err != nil {
		t.Fatalf("AcquireN failed: %v", err)
	}
	if len(insts) != 5 {
		t.Fatalf("len(insts)=%d, want 5", len(insts))
	}
	for _, ins := range insts {
		if ins == nil {
			t.Fatal("AcquireN returned nil instance")
		}
		ins.Release()
	}
}

func TestRuntime_CompilationCacheDir(t *testing.T) {
	dir := t.TempDir()
	adptr := &fakeAdapter{evalResult: sango.Result{Value: []byte(`ok`)}}

	rt, err := sango.New(t.Context(), minimalWasm, adptr,
		sango.WithPoolSize(0), sango.WithCompilationCacheDir(dir))
	if err != nil {
		t.Fatalf("New with cache dir failed: %v", err)
	}
	rt.Close(t.Context())

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("expected cache dir populated, got empty")
	}

	rt2, err := sango.New(t.Context(), minimalWasm, adptr,
		sango.WithPoolSize(0), sango.WithCompilationCacheDir(dir))
	if err != nil {
		t.Fatalf("New reusing cache dir failed: %v", err)
	}
	rt2.Close(t.Context())
}

func TestRuntime_CompilationCache_Shared(t *testing.T) {
	cache := wazero.NewCompilationCache()
	defer cache.Close(t.Context())

	adptr := &fakeAdapter{evalResult: sango.Result{Value: []byte(`ok`)}}
	for i := 0; i < 3; i++ {
		rt, err := sango.New(t.Context(), minimalWasm, adptr,
			sango.WithPoolSize(0), sango.WithCompilationCache(cache))
		if err != nil {
			t.Fatalf("iteration %d: New failed: %v", i, err)
		}
		rt.Close(t.Context())
	}
}

type memAdapter struct{}

func (memAdapter) ID() string { return "memiso" }

func (memAdapter) Initialize(ctx context.Context, mod api.Module) error { return nil }

func (memAdapter) Eval(ctx context.Context, mod api.Module, code []byte) (sango.Result, error) {
	if len(code) < 1 {
		return sango.Result{}, errors.New("memAdapter: empty code")
	}
	mem := mod.Memory()
	switch code[0] {
	case 'S':
		size := mem.Size()
		out := make([]byte, 4)
		binary.BigEndian.PutUint32(out, size)
		return sango.Result{Value: out}, nil
	case 'G':
		if len(code) < 5 {
			return sango.Result{}, errors.New("memAdapter: G needs delta")
		}
		delta := binary.BigEndian.Uint32(code[1:5])
		if _, ok := mem.Grow(delta); !ok {
			return sango.Result{}, errors.New("memAdapter: grow failed")
		}
		return sango.Result{Value: []byte("ok")}, nil
	}
	if len(code) < 5 {
		return sango.Result{}, errors.New("memAdapter: code too short")
	}
	off := binary.BigEndian.Uint32(code[1:5])
	switch code[0] {
	case 'W':
		if !mem.Write(off, code[5:]) {
			return sango.Result{}, errors.New("memAdapter: write out of range")
		}
		return sango.Result{Value: []byte("ok")}, nil
	case 'R':
		if len(code) < 9 {
			return sango.Result{}, errors.New("memAdapter: R needs length")
		}
		n := binary.BigEndian.Uint32(code[5:9])
		b, ok := mem.Read(off, n)
		if !ok {
			return sango.Result{}, errors.New("memAdapter: read out of range")
		}
		out := make([]byte, len(b))
		copy(out, b)
		return sango.Result{Value: out}, nil
	default:
		return sango.Result{}, errors.New("memAdapter: unknown op")
	}
}

func encodeWrite(off uint32, payload []byte) []byte {
	buf := make([]byte, 5+len(payload))
	buf[0] = 'W'
	binary.BigEndian.PutUint32(buf[1:5], off)
	copy(buf[5:], payload)
	return buf
}

func encodeRead(off, n uint32) []byte {
	buf := make([]byte, 9)
	buf[0] = 'R'
	binary.BigEndian.PutUint32(buf[1:5], off)
	binary.BigEndian.PutUint32(buf[5:9], n)
	return buf
}

func encodeGrow(delta uint32) []byte {
	buf := make([]byte, 5)
	buf[0] = 'G'
	binary.BigEndian.PutUint32(buf[1:5], delta)
	return buf
}

func encodeSize() []byte {
	return []byte{'S'}
}

func TestRuntime_RestoreN_MemoryIsolation(t *testing.T) {
	t.Parallel()

	const (
		n           = 8
		offset      = uint32(1024)
		payloadSize = 64
	)

	rt, err := sango.New(t.Context(), minimalWasm, memAdapter{}, sango.WithPoolSize(0))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	t.Cleanup(func() { rt.Close(t.Context()) })

	seed, err := rt.Acquire(t.Context())
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	snap, err := rt.Snapshot(seed)
	if err != nil {
		t.Fatalf("Snapshot failed: %v", err)
	}
	seed.Release()

	forks, err := rt.RestoreN(t.Context(), snap, n)
	if err != nil {
		t.Fatalf("RestoreN failed: %v", err)
	}
	if len(forks) != n {
		t.Fatalf("len(forks)=%d, want %d", len(forks), n)
	}
	t.Cleanup(func() {
		for _, f := range forks {
			f.Release()
		}
	})

	sentinelOf := func(i int) []byte {
		return bytes.Repeat([]byte{byte(i + 1)}, payloadSize)
	}

	var wg sync.WaitGroup
	writeErrs := make([]error, n)
	for i, f := range forks {
		i, f := i, f
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.Eval(t.Context(), encodeWrite(offset, sentinelOf(i)))
			writeErrs[i] = err
		}()
	}
	wg.Wait()
	for i, err := range writeErrs {
		if err != nil {
			t.Fatalf("fork %d write: %v", i, err)
		}
	}

	readErrs := make([]error, n)
	got := make([][]byte, n)
	for i, f := range forks {
		i, f := i, f
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := f.Eval(t.Context(), encodeRead(offset, payloadSize))
			if err != nil {
				readErrs[i] = err
				return
			}
			got[i] = res.Value
		}()
	}
	wg.Wait()
	for i, err := range readErrs {
		if err != nil {
			t.Fatalf("fork %d read: %v", i, err)
		}
	}

	for i, g := range got {
		want := sentinelOf(i)
		if !bytes.Equal(g, want) {
			t.Fatalf("fork %d isolation broken at offset %d: got %v, want %v (first 8 bytes)",
				i, offset, g[:min(8, len(g))], want[:8])
		}
		for j, other := range got {
			if i == j {
				continue
			}
			if bytes.Equal(g, other) {
				t.Fatalf("fork %d and fork %d see identical memory (isolation broken)", i, j)
			}
		}
	}

	untouchedOff := offset + payloadSize
	untouchedLen := uint32(256)
	for i, f := range forks {
		res, err := f.Eval(t.Context(), encodeRead(untouchedOff, untouchedLen))
		if err != nil {
			t.Fatalf("fork %d untouched read: %v", i, err)
		}
		for j, b := range res.Value {
			if b != 0 {
				t.Fatalf("fork %d: expected zeros at untouched region, got byte[%d]=%#x", i, j, b)
			}
		}
	}

	for i, f := range forks {
		res, err := f.Eval(t.Context(), encodeRead(offset, payloadSize))
		if err != nil {
			t.Fatalf("fork %d recheck read: %v", i, err)
		}
		want := sentinelOf(i)
		if !bytes.Equal(res.Value, want) {
			t.Fatalf("fork %d value drifted after peer reads: got %v, want %v (first 8 bytes)",
				i, res.Value[:min(8, len(res.Value))], want[:8])
		}
	}
}

func TestRuntime_RestoreN_MultiRoundIsolation(t *testing.T) {
	t.Parallel()

	const (
		n           = 8
		rounds      = 5
		offset      = uint32(1024)
		payloadSize = 64
	)

	rt, err := sango.New(t.Context(), minimalWasm, memAdapter{}, sango.WithPoolSize(0))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	t.Cleanup(func() { rt.Close(t.Context()) })

	seed, err := rt.Acquire(t.Context())
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	snap, err := rt.Snapshot(seed)
	if err != nil {
		t.Fatalf("Snapshot failed: %v", err)
	}
	seed.Release()

	forks, err := rt.RestoreN(t.Context(), snap, n)
	if err != nil {
		t.Fatalf("RestoreN failed: %v", err)
	}
	t.Cleanup(func() {
		for _, f := range forks {
			f.Release()
		}
	})

	sentinelOf := func(round, i int) []byte {
		return bytes.Repeat([]byte{byte(round*n + i + 1)}, payloadSize)
	}

	for r := 0; r < rounds; r++ {
		var wg sync.WaitGroup
		writeErrs := make([]error, n)
		for i, f := range forks {
			i, f := i, f
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := f.Eval(t.Context(), encodeWrite(offset, sentinelOf(r, i)))
				writeErrs[i] = err
			}()
		}
		wg.Wait()
		for i, err := range writeErrs {
			if err != nil {
				t.Fatalf("round %d fork %d write: %v", r, i, err)
			}
		}

		readErrs := make([]error, n)
		got := make([][]byte, n)
		for i, f := range forks {
			i, f := i, f
			wg.Add(1)
			go func() {
				defer wg.Done()
				res, err := f.Eval(t.Context(), encodeRead(offset, payloadSize))
				if err != nil {
					readErrs[i] = err
					return
				}
				got[i] = res.Value
			}()
		}
		wg.Wait()
		for i, err := range readErrs {
			if err != nil {
				t.Fatalf("round %d fork %d read: %v", r, i, err)
			}
		}
		for i, g := range got {
			want := sentinelOf(r, i)
			if !bytes.Equal(g, want) {
				t.Fatalf("round %d fork %d: got %v, want %v (first 8 bytes)",
					r, i, g[:min(8, len(g))], want[:8])
			}
		}
	}
}

func TestRuntime_RestoreN_UniqueOffsetIsolation(t *testing.T) {
	t.Parallel()

	const (
		n           = 8
		baseOffset  = uint32(1024)
		stride      = uint32(256)
		payloadSize = 64
	)

	rt, err := sango.New(t.Context(), minimalWasm, memAdapter{}, sango.WithPoolSize(0))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	t.Cleanup(func() { rt.Close(t.Context()) })

	seed, err := rt.Acquire(t.Context())
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	snap, err := rt.Snapshot(seed)
	if err != nil {
		t.Fatalf("Snapshot failed: %v", err)
	}
	seed.Release()

	forks, err := rt.RestoreN(t.Context(), snap, n)
	if err != nil {
		t.Fatalf("RestoreN failed: %v", err)
	}
	t.Cleanup(func() {
		for _, f := range forks {
			f.Release()
		}
	})

	offsetOf := func(i int) uint32 { return baseOffset + uint32(i)*stride }
	sentinelOf := func(i int) []byte { return bytes.Repeat([]byte{byte(i + 1)}, payloadSize) }

	var wg sync.WaitGroup
	writeErrs := make([]error, n)
	for i, f := range forks {
		i, f := i, f
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.Eval(t.Context(), encodeWrite(offsetOf(i), sentinelOf(i)))
			writeErrs[i] = err
		}()
	}
	wg.Wait()
	for i, err := range writeErrs {
		if err != nil {
			t.Fatalf("fork %d write at %d: %v", i, offsetOf(i), err)
		}
	}

	for i, f := range forks {
		for j := 0; j < n; j++ {
			res, err := f.Eval(t.Context(), encodeRead(offsetOf(j), payloadSize))
			if err != nil {
				t.Fatalf("fork %d read at %d: %v", i, offsetOf(j), err)
			}
			if i == j {
				want := sentinelOf(i)
				if !bytes.Equal(res.Value, want) {
					t.Fatalf("fork %d self read: got %v, want %v (first 8)",
						i, res.Value[:min(8, len(res.Value))], want[:8])
				}
				continue
			}
			for k, b := range res.Value {
				if b != 0 {
					t.Fatalf("fork %d peer offset %d leak: byte[%d]=%#x", i, offsetOf(j), k, b)
				}
			}
		}
	}
}

func TestRuntime_RestoreN_GrowIndependence(t *testing.T) {
	t.Parallel()

	const (
		n          = 4
		growDelta  = uint32(1)
		pageSize   = uint32(65536)
		probeBytes = uint32(64)
	)

	rt, err := sango.New(t.Context(), minimalWasm, memAdapter{}, sango.WithPoolSize(0))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	t.Cleanup(func() { rt.Close(t.Context()) })

	seed, err := rt.Acquire(t.Context())
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	snap, err := rt.Snapshot(seed)
	if err != nil {
		t.Fatalf("Snapshot failed: %v", err)
	}
	seed.Release()

	forks, err := rt.RestoreN(t.Context(), snap, n)
	if err != nil {
		t.Fatalf("RestoreN failed: %v", err)
	}
	t.Cleanup(func() {
		for _, f := range forks {
			f.Release()
		}
	})

	res, err := forks[0].Eval(t.Context(), encodeSize())
	if err != nil {
		t.Fatalf("initial size query: %v", err)
	}
	origSize := binary.BigEndian.Uint32(res.Value)

	if _, err := forks[0].Eval(t.Context(), encodeGrow(growDelta)); err != nil {
		t.Fatalf("fork 0 grow: %v", err)
	}

	res, err = forks[0].Eval(t.Context(), encodeSize())
	if err != nil {
		t.Fatalf("fork 0 size after grow: %v", err)
	}
	newSize := binary.BigEndian.Uint32(res.Value)
	if newSize != origSize+growDelta*pageSize {
		t.Fatalf("fork 0 grown size=%d, want %d", newSize, origSize+growDelta*pageSize)
	}

	for i := 1; i < n; i++ {
		res, err := forks[i].Eval(t.Context(), encodeSize())
		if err != nil {
			t.Fatalf("fork %d size: %v", i, err)
		}
		got := binary.BigEndian.Uint32(res.Value)
		if got != origSize {
			t.Fatalf("fork %d size=%d, want %d (peer grow leaked)", i, got, origSize)
		}
	}

	probeOffset := origSize + 128
	if _, err := forks[0].Eval(t.Context(), encodeRead(probeOffset, probeBytes)); err != nil {
		t.Fatalf("fork 0 read in grown region: %v", err)
	}

	for i := 1; i < n; i++ {
		if _, err := forks[i].Eval(t.Context(), encodeRead(probeOffset, probeBytes)); err == nil {
			t.Fatalf("fork %d unexpectedly read %d bytes at offset %d (peer's grown region leaked)",
				i, probeBytes, probeOffset)
		}
	}
}

func TestRuntime_RestoreN_SnapshotAfterDiverge(t *testing.T) {
	t.Parallel()

	const (
		n           = 4
		offset      = uint32(1024)
		payloadSize = 64
	)

	rt, err := sango.New(t.Context(), minimalWasm, memAdapter{}, sango.WithPoolSize(0))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	t.Cleanup(func() { rt.Close(t.Context()) })

	seed, err := rt.Acquire(t.Context())
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	golden, err := rt.Snapshot(seed)
	if err != nil {
		t.Fatalf("Snapshot failed: %v", err)
	}
	seed.Release()

	forks, err := rt.RestoreN(t.Context(), golden, n)
	if err != nil {
		t.Fatalf("RestoreN failed: %v", err)
	}
	t.Cleanup(func() {
		for _, f := range forks {
			f.Release()
		}
	})

	sentinelOf := func(i int) []byte { return bytes.Repeat([]byte{byte(i + 1)}, payloadSize) }

	for i, f := range forks {
		if _, err := f.Eval(t.Context(), encodeWrite(offset, sentinelOf(i))); err != nil {
			t.Fatalf("fork %d initial write: %v", i, err)
		}
	}

	snap0, err := rt.Snapshot(forks[0])
	if err != nil {
		t.Fatalf("Snapshot(fork0) failed: %v", err)
	}

	for i, f := range forks {
		res, err := f.Eval(t.Context(), encodeRead(offset, payloadSize))
		if err != nil {
			t.Fatalf("fork %d post-snapshot read: %v", i, err)
		}
		if !bytes.Equal(res.Value, sentinelOf(i)) {
			t.Fatalf("fork %d value corrupted after Snapshot(fork0)", i)
		}
	}

	restored, err := rt.Restore(t.Context(), snap0)
	if err != nil {
		t.Fatalf("Restore(snap0) failed: %v", err)
	}
	defer restored.Release()

	res, err := restored.Eval(t.Context(), encodeRead(offset, payloadSize))
	if err != nil {
		t.Fatalf("restored read: %v", err)
	}
	if !bytes.Equal(res.Value, sentinelOf(0)) {
		t.Fatalf("restored instance did not carry fork0 sentinel: got %v",
			res.Value[:min(8, len(res.Value))])
	}

	overwrite := bytes.Repeat([]byte{0xFF}, payloadSize)
	if _, err := restored.Eval(t.Context(), encodeWrite(offset, overwrite)); err != nil {
		t.Fatalf("restored write: %v", err)
	}

	for i, f := range forks {
		res, err := f.Eval(t.Context(), encodeRead(offset, payloadSize))
		if err != nil {
			t.Fatalf("fork %d post-overwrite read: %v", i, err)
		}
		if !bytes.Equal(res.Value, sentinelOf(i)) {
			t.Fatalf("fork %d contaminated by restored instance overwrite: got %v",
				i, res.Value[:min(8, len(res.Value))])
		}
	}

	fresh, err := rt.Restore(t.Context(), golden)
	if err != nil {
		t.Fatalf("Restore(golden) failed: %v", err)
	}
	defer fresh.Release()
	res, err = fresh.Eval(t.Context(), encodeRead(offset, payloadSize))
	if err != nil {
		t.Fatalf("golden-restored read: %v", err)
	}
	for j, b := range res.Value {
		if b != 0 {
			t.Fatalf("golden Snapshot contaminated by fork writes: byte[%d]=%#x", j, b)
		}
	}
}

func TestRuntime_RestoreN_SnapshotChainDepth(t *testing.T) {
	t.Parallel()

	const (
		depth       = 3
		offset      = uint32(1024)
		payloadSize = 64
	)

	rt, err := sango.New(t.Context(), minimalWasm, memAdapter{}, sango.WithPoolSize(0))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	t.Cleanup(func() { rt.Close(t.Context()) })

	seed, err := rt.Acquire(t.Context())
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	golden, err := rt.Snapshot(seed)
	if err != nil {
		t.Fatalf("golden Snapshot failed: %v", err)
	}
	seed.Release()

	sentinelOf := func(gen int) []byte {
		return bytes.Repeat([]byte{byte(0xA0 | gen)}, payloadSize)
	}

	insts := make([]*sango.Instance, 0, depth)
	snaps := make([]sango.Snapshot, 0, depth)
	current := golden

	for g := 0; g < depth; g++ {
		inst, err := rt.Restore(t.Context(), current)
		if err != nil {
			t.Fatalf("gen %d Restore: %v", g, err)
		}
		if _, err := inst.Eval(t.Context(), encodeWrite(offset, sentinelOf(g))); err != nil {
			t.Fatalf("gen %d write: %v", g, err)
		}
		snap, err := rt.Snapshot(inst)
		if err != nil {
			t.Fatalf("gen %d Snapshot: %v", g, err)
		}
		insts = append(insts, inst)
		snaps = append(snaps, snap)
		current = snap
	}
	t.Cleanup(func() {
		for _, ins := range insts {
			ins.Release()
		}
	})

	for g, ins := range insts {
		res, err := ins.Eval(t.Context(), encodeRead(offset, payloadSize))
		if err != nil {
			t.Fatalf("gen %d instance read: %v", g, err)
		}
		if !bytes.Equal(res.Value, sentinelOf(g)) {
			t.Fatalf("gen %d instance drifted: got %v, want %v (first 8)",
				g, res.Value[:min(8, len(res.Value))], sentinelOf(g)[:8])
		}
	}

	for g, snap := range snaps {
		ins, err := rt.Restore(t.Context(), snap)
		if err != nil {
			t.Fatalf("re-restore gen %d: %v", g, err)
		}
		res, err := ins.Eval(t.Context(), encodeRead(offset, payloadSize))
		if err != nil {
			ins.Release()
			t.Fatalf("re-restore gen %d read: %v", g, err)
		}
		if !bytes.Equal(res.Value, sentinelOf(g)) {
			ins.Release()
			t.Fatalf("re-restore gen %d wrong sentinel: got %v, want %v (first 8)",
				g, res.Value[:min(8, len(res.Value))], sentinelOf(g)[:8])
		}
		if _, err := ins.Eval(t.Context(), encodeWrite(offset, bytes.Repeat([]byte{0xFF}, payloadSize))); err != nil {
			ins.Release()
			t.Fatalf("re-restore gen %d overwrite: %v", g, err)
		}
		ins.Release()
	}

	for g, ins := range insts {
		res, err := ins.Eval(t.Context(), encodeRead(offset, payloadSize))
		if err != nil {
			t.Fatalf("gen %d post-overwrite read: %v", g, err)
		}
		if !bytes.Equal(res.Value, sentinelOf(g)) {
			t.Fatalf("gen %d original contaminated by re-restore overwrite", g)
		}
	}

	fresh, err := rt.Restore(t.Context(), golden)
	if err != nil {
		t.Fatalf("golden re-Restore: %v", err)
	}
	defer fresh.Release()
	res, err := fresh.Eval(t.Context(), encodeRead(offset, payloadSize))
	if err != nil {
		t.Fatalf("golden re-Restore read: %v", err)
	}
	for j, b := range res.Value {
		if b != 0 {
			t.Fatalf("golden contaminated after chain: byte[%d]=%#x", j, b)
		}
	}
}

func TestRuntime_RestoreN_BatchIndependence(t *testing.T) {
	t.Parallel()

	const (
		n           = 4
		offset      = uint32(1024)
		payloadSize = 64
	)

	rt, err := sango.New(t.Context(), minimalWasm, memAdapter{}, sango.WithPoolSize(0))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	t.Cleanup(func() { rt.Close(t.Context()) })

	seed, err := rt.Acquire(t.Context())
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	snap, err := rt.Snapshot(seed)
	if err != nil {
		t.Fatalf("Snapshot failed: %v", err)
	}
	seed.Release()

	batchA, err := rt.RestoreN(t.Context(), snap, n)
	if err != nil {
		t.Fatalf("RestoreN batch A: %v", err)
	}
	t.Cleanup(func() {
		for _, f := range batchA {
			f.Release()
		}
	})

	sentinelA := func(i int) []byte { return bytes.Repeat([]byte{byte(0xA0 | i)}, payloadSize) }
	sentinelB := func(i int) []byte { return bytes.Repeat([]byte{byte(0xB0 | i)}, payloadSize) }

	for i, f := range batchA {
		if _, err := f.Eval(t.Context(), encodeWrite(offset, sentinelA(i))); err != nil {
			t.Fatalf("batch A fork %d write: %v", i, err)
		}
	}

	batchB, err := rt.RestoreN(t.Context(), snap, n)
	if err != nil {
		t.Fatalf("RestoreN batch B: %v", err)
	}
	t.Cleanup(func() {
		for _, f := range batchB {
			f.Release()
		}
	})

	for i, f := range batchB {
		res, err := f.Eval(t.Context(), encodeRead(offset, payloadSize))
		if err != nil {
			t.Fatalf("batch B fork %d initial read: %v", i, err)
		}
		for j, b := range res.Value {
			if b != 0 {
				t.Fatalf("batch B fork %d not clean at start: byte[%d]=%#x", i, j, b)
			}
		}
	}

	for i, f := range batchB {
		if _, err := f.Eval(t.Context(), encodeWrite(offset, sentinelB(i))); err != nil {
			t.Fatalf("batch B fork %d write: %v", i, err)
		}
	}

	for i, f := range batchA {
		res, err := f.Eval(t.Context(), encodeRead(offset, payloadSize))
		if err != nil {
			t.Fatalf("batch A fork %d post-B read: %v", i, err)
		}
		if !bytes.Equal(res.Value, sentinelA(i)) {
			t.Fatalf("batch A fork %d contaminated by batch B writes: got %v",
				i, res.Value[:min(8, len(res.Value))])
		}
	}

	for i, f := range batchB {
		res, err := f.Eval(t.Context(), encodeRead(offset, payloadSize))
		if err != nil {
			t.Fatalf("batch B fork %d recheck read: %v", i, err)
		}
		if !bytes.Equal(res.Value, sentinelB(i)) {
			t.Fatalf("batch B fork %d drifted: got %v", i, res.Value[:min(8, len(res.Value))])
		}
	}
}
