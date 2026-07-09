package sango

import (
	"context"

	"github.com/tetratelabs/wazero/api"
)

type Adapter interface {
	ID() string
	Initialize(ctx context.Context, mod api.Module) error
	Eval(ctx context.Context, mod api.Module, code []byte) (Result, error)
}

type Result struct {
	Value []byte
	Err   *EvalError
}

func (r Result) OK() bool { return r.Err == nil }

type EvalError struct {
	Message []byte
}

func (e *EvalError) Error() string { return string(e.Message) }
