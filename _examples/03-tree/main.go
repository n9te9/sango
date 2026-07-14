// 03-tree: where the order of growth kicks in.
//
// Agents that explore — try several candidate actions, keep the promising
// branches, backtrack from dead ends — need to branch *stateful* execution.
// sango makes a branch cheap: Snapshot is a copy of linear memory, Restore
// is (mostly) a memcpy. This example runs the same search tree with two
// strategies and prints the measured gap:
//
//	fork:  branch by restoring the parent's snapshot        (cost ~ constant)
//	naive: re-acquire and re-execute the prefix every time  (cost ~ depth)
//
// The eval-count gap grows linearly with depth and the wall-clock gap with
// it — increase maxDepth and watch. That growth is the argument for
// fork-able sandboxes in tree search, speculative execution, and RL
// rollouts.
//
// Candidate code is hardcoded (no API key needed); in production each
// depth's candidates come from one LLM call.
package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/n9te9/sango"
	"github.com/n9te9/sango/adapter/quickjs"
)

const (
	branching = 4 // candidates per depth (= candidates you ask the LLM for)
	maxDepth  = 6
)

// tryStep(c) validates candidate c and, if viable, advances the state —
// destructively. That is the point: trying a sibling requires returning to
// the pre-try state, which is exactly what Snapshot/Restore provides.
// The inner loop stands in for real work (agent code computes things).
var setup = []byte(`
	let path = [];
	function tryStep(c) {
		let acc = 0;
		for (let i = 0; i < 50000; i++) acc += i % 7;
		if ((c + path.length) % 2 === 0) { path.push(c); return true; }
		return false;
	}
	function score() { return path.reduce((a, b) => a * 10 + b, 0); }
`)

func candidate(c int) []byte { return []byte(fmt.Sprintf("tryStep(%d)", c)) }

type stats struct {
	evals     int
	instances int
	best      int
}

func evalOK(ctx context.Context, inst *sango.Instance, code []byte, st *stats) (sango.Result, error) {
	res, err := inst.Eval(ctx, code)
	st.evals++
	if err != nil {
		return res, err
	}
	if !res.OK() {
		return res, fmt.Errorf("guest error on %q: %s", code, res.Err)
	}
	return res, nil
}

// ---- fork strategy: branch via Snapshot/Restore -----------------------------

func exploreFork(ctx context.Context, rt *sango.Runtime, parent sango.Snapshot, depth int, st *stats) error {
	for c := 0; c < branching; c++ {
		inst, err := rt.Restore(ctx, parent) // fork: duplicate parent state
		if err != nil {
			return err
		}
		st.instances++

		res, err := evalOK(ctx, inst, candidate(c), st)
		if err != nil {
			inst.Release()
			return err
		}
		if string(res.Value) == "true" { // viable branch
			if depth+1 == maxDepth {
				if err := recordScore(ctx, inst, st); err != nil {
					inst.Release()
					return err
				}
			} else {
				snap, err := rt.Snapshot(inst)
				if err != nil {
					inst.Release()
					return err
				}
				if err := exploreFork(ctx, rt, snap, depth+1, st); err != nil {
					inst.Release()
					return err
				}
			}
		}
		inst.Release()
	}
	return nil
}

// ---- naive strategy: re-execute the prefix from a clean instance ------------

func exploreNaive(ctx context.Context, rt *sango.Runtime, prefix []int, st *stats) error {
	for c := 0; c < branching; c++ {
		inst, err := rt.Acquire(ctx)
		if err != nil {
			return err
		}
		st.instances++

		// The replay loop below is what fork replaces with one memcpy.
		if _, err := evalOK(ctx, inst, setup, st); err != nil {
			inst.Release()
			return err
		}
		for _, p := range prefix {
			if _, err := evalOK(ctx, inst, candidate(p), st); err != nil {
				inst.Release()
				return err
			}
		}

		res, err := evalOK(ctx, inst, candidate(c), st)
		if err != nil {
			inst.Release()
			return err
		}
		if string(res.Value) == "true" {
			if len(prefix)+1 == maxDepth {
				if err := recordScore(ctx, inst, st); err != nil {
					inst.Release()
					return err
				}
			} else {
				if err := exploreNaive(ctx, rt, append(prefix, c), st); err != nil {
					inst.Release()
					return err
				}
			}
		}
		inst.Release()
	}
	return nil
}

func recordScore(ctx context.Context, inst *sango.Instance, st *stats) error {
	res, err := evalOK(ctx, inst, []byte(`score()`), st)
	if err != nil {
		return err
	}
	v, err := strconv.Atoi(string(res.Value))
	if err != nil {
		return fmt.Errorf("unexpected score %q: %w", res.Value, err)
	}
	if v > st.best {
		st.best = v
	}
	return nil
}

func main() {
	ctx := context.Background()

	rt, err := sango.New(ctx, quickjs.Wasm(), quickjs.QuickJS(), sango.WithWASI())
	if err != nil {
		log.Fatal(err)
	}
	defer rt.Close(ctx)

	// Root of the tree: a snapshot of the session with setup applied.
	root, err := rt.Acquire(ctx)
	if err != nil {
		log.Fatal(err)
	}
	var rootStats stats
	if _, err := evalOK(ctx, root, setup, &rootStats); err != nil {
		log.Fatal(err)
	}
	rootSnap, err := rt.Snapshot(root)
	if err != nil {
		log.Fatal(err)
	}
	root.Release()

	fmt.Printf("tree: branching=%d depth=%d (2 viable per depth -> %d full paths)\n\n",
		branching, maxDepth, 1<<maxDepth)

	var fs stats
	t0 := time.Now()
	if err := exploreFork(ctx, rt, rootSnap, 0, &fs); err != nil {
		log.Fatal("fork: ", err)
	}
	forkDur := time.Since(t0)

	var ns stats
	t0 = time.Now()
	if err := exploreNaive(ctx, rt, nil, &ns); err != nil {
		log.Fatal("naive: ", err)
	}
	naiveDur := time.Since(t0)

	fmt.Printf("%-22s %10s %12s %12s %8s\n", "strategy", "evals", "instances", "time", "best")
	fmt.Printf("%-22s %10d %12d %12v %8d\n", "fork (snapshot)", fs.evals, fs.instances, forkDur.Round(time.Millisecond), fs.best)
	fmt.Printf("%-22s %10d %12d %12v %8d\n", "naive (re-execute)", ns.evals, ns.instances, naiveDur.Round(time.Millisecond), ns.best)
	fmt.Printf("\nre-execution overhead: %.1fx evals, %.1fx time\n",
		float64(ns.evals)/float64(fs.evals), float64(naiveDur)/float64(forkDur))
	fmt.Println("increase maxDepth and watch the gap grow — that growth is the order effect")
}
