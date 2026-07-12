package sango_test

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/n9te9/sango"
	"github.com/tetratelabs/wazero/api"
)

type fakeAdapter struct {
	initializeError error
	evalResult      sango.Result
	evalError       error
}

func (f *fakeAdapter) ID() string { return "fake" }

func (f *fakeAdapter) Initialize(ctx context.Context, mod api.Module) error {
	return f.initializeError
}

func (f *fakeAdapter) Eval(ctx context.Context, mod api.Module, code []byte) (sango.Result, error) {
	return f.evalResult, f.evalError
}

var minimalWasm = []byte{
	0x00, 0x61, 0x73, 0x6D, // magic "\0asm"
	0x01, 0x00, 0x00, 0x00, // version 1
	0x05, 0x03, 0x01, 0x00, 0x01, // memory section: 1 memory, min=1 page
	0x07, 0x0A, 0x01, // export section: 1 entry
	0x06, 0x6D, 0x65, 0x6D, 0x6F, 0x72, 0x79, // name "memory"
	0x02, 0x00, // kind=memory, index=0
}

func TestInstance_Eval(t *testing.T) {
	tests := []struct {
		name       string
		instanceFn func(ctx context.Context) *sango.Instance
		code       []byte
		want       sango.Result
		wantErr    error
	}{
		{
			name: "success: instance.Eval() success and return the result for given code",
			instanceFn: func(ctx context.Context) *sango.Instance {
				adptr := &fakeAdapter{
					initializeError: nil,
					evalError:       nil,
					evalResult: sango.Result{
						Value: []byte(`example`),
						Err:   nil,
					},
				}

				rt, _ := sango.New(ctx, minimalWasm, adptr, sango.WithPoolSize(1))
				ist, _ := rt.Acquire(ctx)
				return ist
			},
			code: []byte("console.log(\"example\")"),
			want: sango.Result{
				Value: []byte(`example`),
			},
			wantErr: nil,
		},
	}

	t.Parallel()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ins := tt.instanceFn(t.Context())
			got, gotErr := ins.Eval(t.Context(), tt.code)
			if d := cmp.Diff(gotErr, tt.wantErr); d != "" {
				t.Fatal(d)
			}

			if d := cmp.Diff(got, tt.want); d != "" {
				t.Fatal(d)
			}
		})
	}
}

func TestInstance_Release(t *testing.T) {

}
