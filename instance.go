package sango

import (
	"context"
	"errors"
	"sync"

	"github.com/tetratelabs/wazero/api"
)

var (
	ErrReleased       = errors.New("sango: instance already released")
	ErrConcurrentEval = errors.New("sango: concurrent eval on single instance")
)

type Instance struct {
	mod     api.Module
	adapter Adapter

	mu       sync.Mutex
	busy     bool
	released bool
}

func (i *Instance) Eval(ctx context.Context, code []byte) (Result, error) {
	i.mu.Lock()
	if i.released {
		i.mu.Unlock()
		return Result{}, ErrReleased
	}
	if i.busy {
		i.mu.Unlock()
		return Result{}, ErrConcurrentEval
	}
	i.busy = true
	i.mu.Unlock()

	defer func() {
		i.mu.Lock()
		i.busy = false
		i.mu.Unlock()
	}()

	return i.adapter.Eval(ctx, i.mod, code)
}

func (i *Instance) Release() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.released {
		return ErrReleased
	}
	i.released = true
	return i.mod.Close(context.Background())
}
