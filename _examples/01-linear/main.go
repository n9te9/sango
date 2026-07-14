// 01-linear: the bread-and-butter agent loop.
//
// An agent offloads small computations to code — math the LLM would get
// wrong, string/JSON transforms, date arithmetic. Each snippet runs in a
// fresh, isolated instance acquired from the pool: no state leaks between
// executions, and each acquire is a golden-snapshot restore (µs–ms), not a
// cold interpreter boot.
//
// LLM calls are stubbed with hardcoded snippets so this example runs
// offline, with no API key, deterministically. In production, replace
// `llmOutputs` with your model call.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/n9te9/sango"
	"github.com/n9te9/sango/adapter/quickjs"
)

func main() {
	ctx := context.Background()

	rt, err := sango.New(ctx, quickjs.Wasm(), quickjs.QuickJS(),
		sango.WithWASI(), sango.WithPoolSize(4))
	if err != nil {
		log.Fatal(err)
	}
	defer rt.Close(ctx)

	// Pretend these came back from the LLM, one per agent step.
	// Note the last one is broken on purpose: LLMs write broken code,
	// and the guest error message is exactly what you feed back to the
	// model so it can fix itself.
	llmOutputs := [][]byte{
		[]byte(`(1234 * 5678) % 97`),
		[]byte(`[..."sango"].reverse().join("")`),
		[]byte(`[3,1,4,1,5,9,2,6].sort((a,b)=>a-b).join(",")`),
		[]byte(`JSON.stringify({users: 3, active: 2, ratio: 2/3})`),
		[]byte(`nosuchvariable + 1`),
	}

	for i, code := range llmOutputs {
		start := time.Now()

		inst, err := rt.Acquire(ctx) // clean instance, every time
		if err != nil {
			log.Fatal(err) // infra error: something is wrong with the runtime
		}

		res, err := inst.Eval(ctx, code)
		if err != nil {
			inst.Release()
			log.Fatal(err)
		}
		inst.Release()

		elapsed := time.Since(start)
		if res.OK() {
			fmt.Printf("step %d  %-55s -> %-20s (%v)\n", i+1, code, res.Value, elapsed)
		} else {
			// Guest error ≠ infra error. This is a *normal* outcome for
			// LLM-generated code — send res.Err back to the model.
			fmt.Printf("step %d  %-55s -> guest error: %s (%v)\n", i+1, code, res.Err, elapsed)
		}
	}
}
