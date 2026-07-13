package sango_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/n9te9/sango"
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
