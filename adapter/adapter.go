package adapter

import "context"

type Adapter interface {
	Allocate(ctx context.Context, size uint32) (uint32, error)
	Eval(ctx context.Context, code []byte) ([]byte, error)
}
