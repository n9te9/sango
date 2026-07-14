package quickjs

import (
	"context"
	_ "embed"

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
