package cpython_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/n9te9/sango"
	"github.com/n9te9/sango/adapter/cpython"
	"github.com/tetratelabs/wazero/api"
)

type fakeMemory struct {
	api.Memory
	data []byte
}

type fakeFunc struct {
	api.Function
	fn func(ctx context.Context, params []uint64) ([]uint64, error)
}

func (f *fakeFunc) Call(ctx context.Context, params ...uint64) ([]uint64, error) {
	return f.fn(ctx, params)
}

type fakeModule struct {
	api.Module
	mem   *fakeMemory
	funcs map[string]api.Function
}

func (m *fakeModule) Memory() api.Memory { return m.mem }

func (m *fakeModule) ExportedFunction(name string) api.Function {
	return m.funcs[name]
}

func TestCPython_ID(t *testing.T) {
	tests := []struct {
		name    string
		adapter sango.Adapter
		want    string
	}{
		{
			name:    "ok: ID returned \"cpython\"",
			adapter: cpython.CPython(),
			want:    "cpython",
		},
	}

	t.Parallel()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.adapter.ID()
			if d := cmp.Diff(got, tt.want); d != "" {
				t.Fatalf("%v", d)
			}
		})
	}
}

func (m *fakeMemory) Size() uint32 { return uint32(len(m.data)) }

func (m *fakeMemory) Read(offset, count uint32) ([]byte, bool) {
	if uint64(offset)+uint64(count) > uint64(len(m.data)) {
		return nil, false
	}

	return m.data[offset : offset+count], true
}

func (m *fakeMemory) Write(offset uint32, v []byte) bool {
	if uint64(offset)+uint64(len(v)) > uint64(len(m.data)) {
		return false
	}

	copy(m.data[offset:], v)
	return true
}

func TestCPython_Initialize(t *testing.T) {
	tests := []struct {
		name    string
		adapter sango.Adapter
		mod     api.Module
		wantErr error
	}{
		{
			name:    "ok: Initialize success and return nil",
			adapter: cpython.CPython(),
			mod: func() api.Module {
				mem := &fakeMemory{data: make([]byte, 64*1024)}
				mod := &fakeModule{mem: mem, funcs: map[string]api.Function{}}
				mod.funcs["initialize"] = &fakeFunc{fn: func(_ context.Context, p []uint64) ([]uint64, error) {
					return []uint64{0}, nil
				}}

				return mod
			}(),
			wantErr: nil,
		},
		{
			name:    "fail: Initialize fail and return error",
			adapter: cpython.CPython(),
			mod: func() api.Module {
				mem := &fakeMemory{data: make([]byte, 64*1024)}
				mod := &fakeModule{mem: mem, funcs: map[string]api.Function{}}
				mod.funcs["initialize"] = &fakeFunc{fn: func(_ context.Context, p []uint64) ([]uint64, error) {
					return []uint64{0}, errors.New("failed to initialize")
				}}

				return mod
			}(),
			wantErr: errors.New("initialize call failed: failed to initialize"),
		},
	}

	t.Parallel()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.adapter.Initialize(t.Context(), tt.mod)
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
		})
	}
}

func TestCPython_Eval(t *testing.T) {
	tests := []struct {
		name    string
		adapter sango.Adapter
		mod     api.Module
		wantErr error
	}{
		{
			name:    "ok: eval success and return nil",
			adapter: cpython.CPython(),
			mod: func() api.Module {
				mem := &fakeMemory{data: make([]byte, 64*1024)}
				mod := &fakeModule{mem: mem, funcs: map[string]api.Function{}}
				mod.funcs["initialize"] = &fakeFunc{fn: func(_ context.Context, p []uint64) ([]uint64, error) {
					return []uint64{0}, nil
				}}

				mod.funcs["allocate"] = &fakeFunc{fn: func(_ context.Context, p []uint64) ([]uint64, error) {
					return []uint64{0}, nil
				}}
				mod.funcs["deallocate"] = &fakeFunc{fn: func(_ context.Context, p []uint64) ([]uint64, error) {
					return []uint64{0}, nil
				}}
				mod.funcs["initialize"] = &fakeFunc{fn: func(_ context.Context, p []uint64) ([]uint64, error) {
					return []uint64{0}, nil
				}}
				mod.funcs["eval"] = &fakeFunc{fn: func(_ context.Context, p []uint64) ([]uint64, error) {
					return []uint64{0}, nil
				}}

				return mod
			}(),
			wantErr: nil,
		},
		{
			name:    "fail: eval fail and return error",
			adapter: cpython.CPython(),
			mod: func() api.Module {
				mem := &fakeMemory{data: make([]byte, 64*1024)}
				mod := &fakeModule{mem: mem, funcs: map[string]api.Function{}}
				mod.funcs["initialize"] = &fakeFunc{fn: func(_ context.Context, p []uint64) ([]uint64, error) {
					return []uint64{0}, errors.New("failed to initialize")
				}}

				return mod
			}(),
			wantErr: errors.New("initialize call failed: failed to initialize"),
		},
	}

	t.Parallel()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.adapter.Initialize(t.Context(), tt.mod)
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
		})
	}
}
