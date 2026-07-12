package sango_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/n9te9/sango"
)

func TestResult_OK(t *testing.T) {
	tests := []struct {
		name   string
		result sango.Result
		want   bool
	}{
		{
			name:   "success: result has no error and, OK() return true",
			result: sango.Result{},
			want:   true,
		},
		{
			name: "success: result has an error and, OK() return false",
			result: sango.Result{
				Err: &sango.EvalError{
					Message: []byte(`something error`),
				},
			},
			want: false,
		},
	}

	t.Parallel()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.result.OK()
			if d := cmp.Diff(got, tt.want); d != "" {
				t.Fatalf("expected %v, but got %v", got, tt.want)
			}
		})
	}
}

func TestEvalError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *sango.EvalError
		want string
	}{
		{
			name: "success: evalResult Error return \"(empty string)\"",
			err: &sango.EvalError{
				Message: []byte(``),
			},
			want: "",
		},
		{
			name: "success: evalResult Error return \"something error\"",
			err: &sango.EvalError{
				Message: []byte(`something error`),
			},
			want: "something error",
		},
	}

	t.Parallel()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.err.Error()
			if d := cmp.Diff(got, tt.want); d != "" {
				t.Fatalf("expected %v, but got %v", got, tt.want)
			}
		})
	}
}
