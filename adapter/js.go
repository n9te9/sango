package adapter

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero/api"
)

type jsAdapter struct {
	mod       api.Module
	allocFunc api.Function
	evalFunc  api.Function
}

var _ (Adapter) = (*jsAdapter)(nil)

func NewJSAdapter(mod api.Module) (*jsAdapter, error) {
	allocFunc := mod.ExportedFunction("allocate")
	if allocFunc == nil {
		return nil, fmt.Errorf("allocate function not found")
	}

	evalFunc := mod.ExportedFunction("eval")
	if evalFunc == nil {
		return nil, fmt.Errorf("eval function not found")
	}

	return &jsAdapter{
		mod:       mod,
		allocFunc: allocFunc,
		evalFunc:  evalFunc,
	}, nil
}

func (j *jsAdapter) Allocate(ctx context.Context, size uint32) (uint32, error) {
	return 0, nil
}

func (j *jsAdapter) Eval(ctx context.Context, code []byte) ([]byte, error) {
	return nil, nil
}
