package quickjs

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/n9te9/sango"
	"github.com/n9te9/sango/internal/cabi"
	"github.com/tetratelabs/wazero/api"
)

//go:embed quickjs.wasm
var wasmBinary []byte

// Wasm returns the embedded QuickJS wasm binary. Callers pass it to
// sango.New to construct a Runtime.
func Wasm() []byte { return wasmBinary }

type quickJSAdapter struct{}

var _ (sango.Adapter) = (*quickJSAdapter)(nil)

func QuickJS() sango.Adapter { return &quickJSAdapter{} }

func (*quickJSAdapter) ID() string { return "quickjs" }

func (q *quickJSAdapter) Initialize(ctx context.Context, mod api.Module) error {
	return cabi.Initialize(ctx, mod)
}

func (q *quickJSAdapter) Eval(ctx context.Context, mod api.Module, code []byte) (sango.Result, error) {
	return cabi.Eval(ctx, mod, code)
}

func DefaultCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("quickjs: locate user cache dir: %w", err)
	}
	return filepath.Join(base, "sango", "quickjs"), nil
}

func WithDefaultCache() (sango.Option, error) {
	dir, err := DefaultCacheDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("quickjs: create cache dir %q: %w", dir, err)
	}
	return sango.WithCompilationCacheDir(dir), nil
}
