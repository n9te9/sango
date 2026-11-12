package sango

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// HostFunc is a function the host lends to the guest. The guest calls it by
// name with an opaque byte payload (typically JSON) and receives the returned
// bytes, or the error message as an exception on the guest side.
//
// The ctx is the one passed to Instance.Eval, so a deadline set for the
// evaluation also bounds the host call.
type HostFunc func(ctx context.Context, arg []byte) ([]byte, error)

// WithHostFunc lends fn to guests under name. Guests have no network or file
// access of their own; a host function is the only way for guest code to reach
// the outside world, and only the functions registered here are reachable.
// Calling an unregistered name fails inside the guest.
func WithHostFunc(name string, fn HostFunc) Option {
	return func(c *config) {
		if c.hostFuncs == nil {
			c.hostFuncs = map[string]HostFunc{}
		}
		c.hostFuncs[name] = fn
	}
}

const (
	hostModuleName = "sango"
	hostTagOK      = 0x00
	hostTagError   = 0x01
)

// instantiateHostModule defines the "sango" import module. It is always
// defined so that guests built with host call support can be instantiated
// even when no host function is registered.
//
// Guest signature:
//
//	call(name_ptr, name_len, arg_ptr, arg_len i32) -> i64
//
// The result packs (ptr << 32 | len) of a buffer allocated with the guest's
// exported "allocate". Its first byte is a tag (0x00 ok, 0x01 error) followed
// by the payload. The guest owns and frees the buffer.
func instantiateHostModule(ctx context.Context, wrt wazero.Runtime, funcs map[string]HostFunc) error {
	call := func(ctx context.Context, mod api.Module, stack []uint64) {
		stack[0] = hostCall(ctx, mod, funcs,
			uint32(stack[0]), uint32(stack[1]), uint32(stack[2]), uint32(stack[3]))
	}
	_, err := wrt.NewHostModuleBuilder(hostModuleName).
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(call),
			[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32},
			[]api.ValueType{api.ValueTypeI64}).
		Export("call").
		Instantiate(ctx)
	if err != nil {
		return fmt.Errorf("sango: instantiate host module: %w", err)
	}
	return nil
}

func hostCall(ctx context.Context, mod api.Module, funcs map[string]HostFunc, namePtr, nameLen, argPtr, argLen uint32) uint64 {
	mem := mod.Memory()
	name, ok := mem.Read(namePtr, nameLen)
	if !ok {
		return writeHostResult(ctx, mod, hostTagError, []byte("sango: host call name out of bounds"))
	}
	fn, found := funcs[string(name)]
	if !found {
		return writeHostResult(ctx, mod, hostTagError, fmt.Appendf(nil, "sango: host function %q is not provided", name))
	}
	rawArg, ok := mem.Read(argPtr, argLen)
	if !ok {
		return writeHostResult(ctx, mod, hostTagError, []byte("sango: host call argument out of bounds"))
	}
	// Copy: the view aliases guest memory, which the host function must not
	// retain or observe changing.
	arg := append([]byte(nil), rawArg...)

	out, err := fn(ctx, arg)
	if err != nil {
		return writeHostResult(ctx, mod, hostTagError, []byte(err.Error()))
	}
	return writeHostResult(ctx, mod, hostTagOK, out)
}

// writeHostResult copies tag+payload into a guest-allocated buffer. It returns
// 0 when the guest cannot allocate; the guest treats that as a failed call.
func writeHostResult(ctx context.Context, mod api.Module, tag byte, payload []byte) uint64 {
	alloc := mod.ExportedFunction("allocate")
	if alloc == nil {
		return 0
	}
	n := uint32(len(payload) + 1)
	res, err := alloc.Call(ctx, uint64(n))
	if err != nil || len(res) == 0 || uint32(res[0]) == 0 {
		return 0
	}
	ptr := uint32(res[0])
	mem := mod.Memory()
	if !mem.WriteByte(ptr, tag) || !mem.Write(ptr+1, payload) {
		return 0
	}
	return uint64(ptr)<<32 | uint64(n)
}
