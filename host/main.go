package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

func main() {
	ctx := context.Background()

	r := wazero.NewRuntime(ctx)
	defer r.Close(ctx)

	wasi_snapshot_preview1.MustInstantiate(ctx, r)

	wasmBytes, err := os.ReadFile("main.wasm")
	if err != nil {
		panic(fmt.Sprintf("failed to read wasm file: %v", err))
	}

	compiledModule, err := r.CompileModule(ctx, wasmBytes)
	if err != nil {
		panic(fmt.Sprintf("failed to compile module: %v", err))
	}

	config := wazero.NewModuleConfig().
		WithStdout(os.Stdout).
		WithStderr(os.Stderr).
		WithRandSource(rand.Reader)

	mod, err := r.InstantiateModule(ctx, compiledModule, config)
	if err != nil {
		panic(fmt.Sprintf("failed to instantiate module: %v", err))
	}

	defer mod.Close(ctx)

	initFunc := mod.ExportedFunction("_initialize")
	if initFunc != nil {
		if _, err := initFunc.Call(ctx); err != nil {
			panic(fmt.Sprintf("failed to initialize wasm reactor: %v", err))
		}
	}

	addFunc := mod.ExportedFunction("add")
	if addFunc == nil {
		panic("function 'add' not found in wasm")
	}

	results, err := addFunc.Call(ctx, 10, 20)
	if err != nil {
		panic(fmt.Sprintf("failed to call function: %v", err))
	}

	if len(results) > 0 {
		fmt.Printf("Sango Sandbox Result (10 + 20): %d\n", results[0])
	}
}
