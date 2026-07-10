package cabi_test

import (
	"bytes"
	"context"
	"errors"
	"math"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/n9te9/sango"
	"github.com/n9te9/sango/internal/cabi"
	"github.com/tetratelabs/wazero/api"
)

type fakeMemory struct {
	api.Memory
	data []byte
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

type deallocCall struct{ ptr, size uint32 }

type guest struct {
	mod      *fakeModule
	next     uint32
	deallocs []deallocCall
	evalFn   func(code []byte) ([]byte, error)
}

const heapBase = 1024

func newGuest(evalFn func(code []byte) ([]byte, error)) *guest {
	g := &guest{
		next:   heapBase,
		evalFn: evalFn,
	}
	mem := &fakeMemory{data: make([]byte, 64*1024)}
	g.mod = &fakeModule{mem: mem, funcs: map[string]api.Function{}}

	g.mod.funcs["allocate"] = &fakeFunc{
		fn: func(_ context.Context, p []uint64) ([]uint64, error) {
			ptr := g.next
			g.next += uint32(p[0])
			return []uint64{uint64(ptr)}, nil
		},
	}
	g.mod.funcs["deallocate"] = &fakeFunc{fn: func(_ context.Context, p []uint64) ([]uint64, error) {
		g.deallocs = append(g.deallocs, deallocCall{uint32(p[0]), uint32(p[1])})
		return nil, nil
	}}
	g.mod.funcs["initialize"] = &fakeFunc{fn: func(_ context.Context, p []uint64) ([]uint64, error) {
		return []uint64{0}, nil
	}}
	g.mod.funcs["eval"] = &fakeFunc{fn: func(_ context.Context, p []uint64) ([]uint64, error) {
		ptr, length := uint32(p[0]), uint32(p[1])
		code, _ := mem.Read(ptr, length)
		out, err := g.evalFn(bytes.Clone(code))
		if err != nil {
			return nil, err
		}
		outPtr := g.next
		g.next += uint32(len(out))
		copy(mem.data[outPtr:], out)
		return []uint64{uint64(outPtr)<<32 | uint64(uint32(len(out)))}, nil
	}}

	return g
}

func TestInitialize(t *testing.T) {
	tests := []struct {
		name    string
		mod     func(ctx context.Context) api.Module
		wantErr error
	}{
		{
			name:    "ok: initialize function in wazero api.module",
			mod:     func(ctx context.Context) api.Module { return newGuest(nil).mod },
			wantErr: nil,
		},
		{
			name: "fail: failed to call initialize function",
			mod: func(ctx context.Context) api.Module {
				g := newGuest(nil)
				g.mod.funcs["initialize"] = &fakeFunc{fn: func(ctx context.Context, _ []uint64) ([]uint64, error) {
					return nil, errors.New("failed to call initialize")
				}}
				return g.mod
			},
			wantErr: errors.New("initialize call failed: failed to call initialize"),
		},
		{
			name: "fail: initialize function non zero status",
			mod: func(ctx context.Context) api.Module {
				g := newGuest(nil)
				g.mod.funcs["initialize"] = &fakeFunc{fn: func(ctx context.Context, _ []uint64) ([]uint64, error) {
					return []uint64{2}, nil
				}}
				return g.mod
			},
			wantErr: errors.New("guest initialize failed: code=[2]"),
		},
		{
			name: "fail: initialize function is not defined in wasm",

			mod: func(ctx context.Context) api.Module {
				g := newGuest(nil)
				g.mod.funcs["initialize"] = nil
				return g.mod
			},
			wantErr: errors.New("exported function \"initialize\" not found"),
		},
	}

	t.Parallel()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			mod := tt.mod(ctx)
			err := cabi.Initialize(ctx, mod)
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

func TestEval(t *testing.T) {
	tests := []struct {
		name    string
		mod     func(ctx context.Context) api.Module
		code    []byte
		want    sango.Result
		wantErr error
	}{
		{
			name: "ok: eval given code and return result",
			mod: func(ctx context.Context) api.Module {
				evalFn := func(code []byte) ([]byte, error) {
					return append([]byte{0x00}, "hogehoge"...), nil
				}
				return newGuest(evalFn).mod
			},
			code: []byte(``),
			want: sango.Result{
				Value: []byte(`hogehoge`),
				Err:   nil,
			},
			wantErr: nil,
		},
		{
			name: "fail: allocate function is not defined in wasm",
			mod: func(ctx context.Context) api.Module {
				g := newGuest(nil)
				g.mod.funcs["allocate"] = nil
				return g.mod
			},
			wantErr: errors.New("exported function \"allocate\" not found"),
		},
		{
			name: "fail: deallocate function is not defined in wasm",
			mod: func(ctx context.Context) api.Module {
				g := newGuest(nil)
				g.mod.funcs["deallocate"] = nil
				return g.mod
			},
			wantErr: errors.New("exported function \"deallocate\" not found"),
		},
		{
			name: "fail: eval function is not defined in wasm",
			mod: func(ctx context.Context) api.Module {
				g := newGuest(nil)
				g.mod.funcs["eval"] = nil
				return g.mod
			},
			wantErr: errors.New("exported function \"eval\" not found"),
		},
		{
			name: "fail: failed to call allocate function",
			mod: func(ctx context.Context) api.Module {
				g := newGuest(nil)
				g.mod.funcs["allocate"] = &fakeFunc{
					fn: func(_ context.Context, p []uint64) ([]uint64, error) {
						return nil, errors.New("failed to call allocate")
					},
				}

				return g.mod
			},
			wantErr: errors.New("allocate failed: failed to call allocate"),
		},
		{
			name: "fail: call allocate no results",
			mod: func(ctx context.Context) api.Module {
				g := newGuest(nil)
				g.mod.funcs["allocate"] = &fakeFunc{
					fn: func(_ context.Context, p []uint64) ([]uint64, error) {
						return []uint64{}, nil
					},
				}

				return g.mod
			},
			wantErr: errors.New("allocate returned no results"),
		},
		{
			name: "fail: write code to wasm memory",
			mod: func(ctx context.Context) api.Module {
				g := newGuest(nil)
				g.mod.funcs["allocate"] = &fakeFunc{
					fn: func(_ context.Context, p []uint64) ([]uint64, error) {
						return []uint64{math.MaxInt64}, nil
					},
				}

				return g.mod
			},
			wantErr: errors.New("write code to wasm memory: out of bounds"),
		},
		{
			name: "fail: failed to call eval function",
			mod: func(ctx context.Context) api.Module {
				g := newGuest(nil)
				g.mod.funcs["eval"] = &fakeFunc{
					fn: func(_ context.Context, p []uint64) ([]uint64, error) {
						return nil, errors.New("failed to call eval")
					},
				}

				return g.mod
			},
			wantErr: errors.New("eval call failed: failed to call eval"),
		},
		{
			name: "fail: call eval no results",
			mod: func(ctx context.Context) api.Module {
				g := newGuest(nil)
				g.mod.funcs["eval"] = &fakeFunc{
					fn: func(_ context.Context, p []uint64) ([]uint64, error) {
						return []uint64{}, nil
					},
				}

				return g.mod
			},
			wantErr: errors.New("eval returned no results"),
		},
		{
			name: "fail: guest returned empty result",
			mod: func(ctx context.Context) api.Module {
				g := newGuest(nil)
				g.mod.funcs["eval"] = &fakeFunc{
					fn: func(_ context.Context, p []uint64) ([]uint64, error) {
						return []uint64{0}, nil
					},
				}

				return g.mod
			},
			wantErr: errors.New("guest returned empty result (missing tag byte)"),
		},
		{
			name: "fail: guest execution result include error tag",
			mod: func(ctx context.Context) api.Module {
				evalFn := func(code []byte) ([]byte, error) {
					return append([]byte{0x01}, "hogehoge"...), nil
				}
				return newGuest(evalFn).mod
			},
			code: []byte(``),
			want: sango.Result{
				Value: nil,
				Err:   &sango.EvalError{Message: append([]byte{}, "hogehoge"...)},
			},
			wantErr: nil,
		},
		{
			name: "fail: guest execution result include unknown tag",
			mod: func(ctx context.Context) api.Module {
				evalFn := func(code []byte) ([]byte, error) {
					return append([]byte{}, "hogehoge"...), nil
				}
				return newGuest(evalFn).mod
			},
			code:    []byte(``),
			want:    sango.Result{},
			wantErr: errors.New("unknown result tag: 0x68"),
		},
	}

	t.Parallel()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			mod := tt.mod(ctx)
			got, err := cabi.Eval(ctx, mod, tt.code)
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

			if d := cmp.Diff(got, tt.want); d != "" {
				t.Fatalf("%v", d)
			}
		})
	}
}

func TestEval_BothBuffersDeallocated(t *testing.T) {
	g := newGuest(func([]byte) ([]byte, error) {
		return append([]byte{0x00}, []byte("ok")...), nil
	})
	code := []byte("some code")
	if _, err := cabi.Eval(context.Background(), g.mod, code); err != nil {
		t.Fatal(err)
	}
	if len(g.deallocs) != 2 {
		t.Fatalf("dealloc calls = %d, want 2 (input + result)", len(g.deallocs))
	}
	// 入力バッファ: heapBase に code 長で確保されたはず
	if g.deallocs[0].size != uint32(len(code)) && g.deallocs[1].size != uint32(len(code)) {
		t.Fatalf("input buffer not freed with correct size: %+v", g.deallocs)
	}
}

func TestEval_ResultIsCopied(t *testing.T) {
	g := newGuest(func([]byte) ([]byte, error) {
		return append([]byte{0x00}, []byte("AAAA")...), nil
	})
	res, err := cabi.Eval(context.Background(), g.mod, []byte("1"))
	if err != nil {
		t.Fatal(err)
	}
	// 線形メモリを汚しても Result.Value が変わらないこと
	for i := range g.mod.mem.data {
		g.mod.mem.data[i] = 0xEE
	}
	if string(res.Value) != "AAAA" {
		t.Fatalf("Result.Value aliases linear memory: %q", res.Value)
	}
}
