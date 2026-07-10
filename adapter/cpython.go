package adapter

import (
	"context"

	"github.com/n9te9/sango"
	"github.com/n9te9/sango/internal/cabi"
	"github.com/tetratelabs/wazero/api"
)

type cpythonAdapter struct{}

var _ sango.Adapter = (*cpythonAdapter)(nil)

func CPython() sango.Adapter { return &cpythonAdapter{} }

func (c *cpythonAdapter) ID() string { return "cpython" }

func (c *cpythonAdapter) Initialize(ctx context.Context, mod api.Module) error {
	return cabi.Initialize(ctx, mod)
}

func (c *cpythonAdapter) Eval(ctx context.Context, mod api.Module, code []byte) (sango.Result, error) {
	return cabi.Eval(ctx, mod, code)
}
