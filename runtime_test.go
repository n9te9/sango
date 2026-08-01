package sango_test

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
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
