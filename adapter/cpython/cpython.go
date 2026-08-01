package cpython

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/n9te9/sango"
	"github.com/n9te9/sango/internal/cabi"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

//go:embed cpython.wasm
var wasmBinary []byte

//go:embed python3.13.zip
var stdlibZip []byte

// stdlibGuestPath is where the embedded stdlib is preopened inside the guest.
// It must match module_search_paths configured in sango_cpython.c.
const stdlibGuestPath = "/lib/python3.13"

type cpythonAdapter struct{}

var _ sango.Adapter = (*cpythonAdapter)(nil)

func CPython() sango.Adapter { return &cpythonAdapter{} }

// Wasm returns the embedded CPython wasm binary. Callers pass it to
// sango.New to construct a Runtime.
func Wasm() []byte { return wasmBinary }

// WithStdlib returns a sango.Option that mounts the embedded Python standard
// library zip as a read-only preopen inside the guest at the path expected by
// sango_cpython.c. It must be used together with sango.WithWASI.
func WithStdlib() (sango.Option, error) {
	zr, err := zip.NewReader(bytes.NewReader(stdlibZip), int64(len(stdlibZip)))
	if err != nil {
		return nil, fmt.Errorf("cpython: open embedded stdlib zip: %w", err)
	}

	fsys, err := newMemFSFromZip(zr)
	if err != nil {
		return nil, fmt.Errorf("cpython: decompress embedded stdlib: %w", err)
	}

	return sango.WithModuleConfigModifier(func(c wazero.ModuleConfig) wazero.ModuleConfig {
		return c.WithFSConfig(wazero.NewFSConfig().WithFSMount(fsys, stdlibGuestPath))
	}), nil
}

func DefaultCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cpython: locate user cache dir: %w", err)
	}
	return filepath.Join(base, "sango", "cpython"), nil
}

func WithDefaultCache() (sango.Option, error) {
	dir, err := DefaultCacheDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("cpython: create cache dir %q: %w", dir, err)
	}
	return sango.WithCompilationCacheDir(dir), nil
}

func (c *cpythonAdapter) ID() string { return "cpython" }

func (c *cpythonAdapter) Initialize(ctx context.Context, mod api.Module) error {
	return cabi.Initialize(ctx, mod)
}

func (c *cpythonAdapter) Eval(ctx context.Context, mod api.Module, code []byte) (sango.Result, error) {
	return cabi.Eval(ctx, mod, code)
}
