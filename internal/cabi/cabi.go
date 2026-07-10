package cabi

import (
	"context"
	"fmt"

	"github.com/n9te9/sango"
	"github.com/tetratelabs/wazero/api"
)

const (
	tagOK    = 0x00
	tagError = 0x01
)

func Initialize(ctx context.Context, mod api.Module) error {
	fn, err := exported(mod, "initialize")
	if err != nil {
		return err
	}
	results, err := fn.Call(ctx)
	if err != nil {
		return fmt.Errorf("initialize call failed: %w", err)
	}
	if len(results) == 0 || results[0] != 0 {
		return fmt.Errorf("guest initialize failed: code=%v", results)
	}
	return nil
}

func Eval(ctx context.Context, mod api.Module, code []byte) (sango.Result, error) {
	alloc, err := exported(mod, "allocate")
	if err != nil {
		return sango.Result{}, err
	}
	dealloc, err := exported(mod, "deallocate")
	if err != nil {
		return sango.Result{}, err
	}
	eval, err := exported(mod, "eval")
	if err != nil {
		return sango.Result{}, err
	}

	codeLen := uint32(len(code))

	ptrRes, err := alloc.Call(ctx, uint64(codeLen))
	if err != nil {
		return sango.Result{}, fmt.Errorf("allocate failed: %w", err)
	}
	if len(ptrRes) == 0 {
		return sango.Result{}, fmt.Errorf("allocate returned no results")
	}
	ptr := uint32(ptrRes[0])
	defer dealloc.Call(ctx, uint64(ptr), uint64(codeLen))

	if !mod.Memory().Write(ptr, code) {
		return sango.Result{}, fmt.Errorf("write code to wasm memory: out of bounds")
	}

	evalRes, err := eval.Call(ctx, uint64(ptr), uint64(codeLen))
	if err != nil {
		return sango.Result{}, fmt.Errorf("eval call failed: %w", err)
	}
	if len(evalRes) == 0 {
		return sango.Result{}, fmt.Errorf("eval returned no results")
	}

	retPtr := uint32(evalRes[0] >> 32)
	retLen := uint32(evalRes[0])

	raw, ok := mod.Memory().Read(retPtr, retLen)
	if !ok {
		return sango.Result{}, fmt.Errorf("read result from wasm memory: out of bounds")
	}
	buf := make([]byte, retLen)
	copy(buf, raw)
	defer dealloc.Call(ctx, uint64(retPtr), uint64(retLen))

	if len(buf) == 0 {
		return sango.Result{}, fmt.Errorf("guest returned empty result (missing tag byte)")
	}
	switch buf[0] {
	case tagOK:
		return sango.Result{Value: buf[1:]}, nil
	case tagError:
		return sango.Result{Err: &sango.EvalError{Message: buf[1:]}}, nil
	default:
		return sango.Result{}, fmt.Errorf("unknown result tag: 0x%02x", buf[0])
	}
}

func exported(mod api.Module, name string) (api.Function, error) {
	fn := mod.ExportedFunction(name)
	if fn == nil {
		return nil, fmt.Errorf("exported function %q not found", name)
	}

	return fn, nil
}
