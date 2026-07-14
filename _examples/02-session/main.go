// 02-session: a Code Interpreter-style session.
//
// A "session" in sango is not a feature — it is simply holding on to an
// Instance. Variables, imports, and function definitions persist across
// Eval calls because they live in the instance's linear memory. Release
// the instance and acquire a new one, and you are guaranteed a clean
// slate: the next session cannot see the previous one's data. That
// isolation is the security boundary for multi-tenant use.
//
// This example uses CPython: define data in one step, aggregate it in the
// next — the cell-by-cell workflow agents use for data questions.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/n9te9/sango"
	"github.com/n9te9/sango/adapter/cpython"
)

func main() {
	ctx := context.Background()

	stdlibOpt, err := cpython.WithStdlib()
	if err != nil {
		log.Fatal(err)
	}

	rt, err := sango.New(ctx, cpython.Wasm(), cpython.CPython(),
		sango.WithWASI(), stdlibOpt, sango.WithPoolSize(4))
	if err != nil {
		log.Fatal(err)
	}
	defer rt.Close(ctx)

	// ---- Session A: state persists across steps -------------------------
	session, err := rt.Acquire(ctx)
	if err != nil {
		log.Fatal(err)
	}

	steps := [][]byte{
		[]byte(`data = [3, 1, 4, 1, 5, 9, 2, 6]`), // cell 1: define
		[]byte(`import statistics`),               // cell 2: import once
		[]byte(`statistics.mean(data)`),           // cell 3: uses cell 1's data
		[]byte(`sorted(data)[-3:]`),               // cell 4: still there
	}
	for i, code := range steps {
		res, err := session.Eval(ctx, code)
		if err != nil {
			log.Fatal(err)
		}
		if !res.OK() {
			log.Fatalf("cell %d: %s", i+1, res.Err)
		}
		fmt.Printf("cell %d  %-35s -> %s\n", i+1, code, res.Value)
	}
	session.Release()

	// ---- Session B: a fresh acquire is a clean slate --------------------
	fresh, err := rt.Acquire(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer fresh.Release()

	res, err := fresh.Eval(ctx, []byte(`data`))
	if err != nil {
		log.Fatal(err)
	}
	if res.OK() {
		log.Fatalf("isolation broken: previous session leaked: %s", res.Value)
	}
	fmt.Printf("\nnew session sees no leftover state -> %s\n", res.Err)
}
