package adapter

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero/api"
)

type quickJSAdapter struct {
	mod       api.Module
	allocFunc api.Function
	evalFunc  api.Function
}

var _ (Adapter) = (*quickJSAdapter)(nil)

func NewJSAdapter(mod api.Module) (*quickJSAdapter, error) {
	allocFunc := mod.ExportedFunction("allocate")
	if allocFunc == nil {
		return nil, fmt.Errorf("allocate function not found")
	}

	evalFunc := mod.ExportedFunction("eval")
	if evalFunc == nil {
		return nil, fmt.Errorf("eval function not found")
	}

	return &quickJSAdapter{
		mod:       mod,
		allocFunc: allocFunc,
		evalFunc:  evalFunc,
	}, nil
}

func (q *quickJSAdapter) Allocate(ctx context.Context, size uint32) (uint32, error) {
	results, err := q.allocFunc.Call(ctx, uint64(size))
	if err != nil {
		return 0, fmt.Errorf("failed to call allocate: %w", err)
	}

	return uint32(results[0]), nil
}

func (q *quickJSAdapter) Eval(ctx context.Context, code []byte) ([]byte, error) {
	codeLen := uint32(len(code))

	ptr, err := q.Allocate(ctx, uint32(codeLen))
	if err != nil {
		return nil, fmt.Errorf("allocate failed: %w", err)
	}

	if q.mod.Memory().Write(ptr, code) {
		return nil, fmt.Errorf("failed to write code to wasm memory out of bounds")
	}

	results, err := q.evalFunc.Call(ctx, uint64(ptr), uint64(codeLen))
	if err != nil {
		return nil, fmt.Errorf("eval execution failed: %w", err)
	}

	retVal := results[0]
	retPtr := uint32(retVal >> 32)
	retLen := uint32(retVal)

	resultBytes, ok := q.mod.Memory().Read(retPtr, retLen)
	if !ok {
		return nil, fmt.Errorf("failed to read result from wasm memory")
	}

	res := make([]byte, retLen)
	copy(res, resultBytes)

	return res, nil
}
