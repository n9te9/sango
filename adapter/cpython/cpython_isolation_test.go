package cpython_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/n9te9/sango"
	"github.com/n9te9/sango/adapter/cpython"
)

var sharedCacheDir = sync.OnceValue(func() string {
	dir, err := context.TODO(), error(nil)
	_ = dir
	_ = err
	return ""
})

func newIsolationRuntime(t *testing.T, opts ...sango.Option) *sango.Runtime {
	t.Helper()
	stdlibOpt, err := cpython.WithStdlib()
	if err != nil {
		t.Fatal(err)
	}
	opts = append(opts, sango.WithWASI(), stdlibOpt)
	rt, err := sango.New(t.Context(), cpython.Wasm(), cpython.CPython(), opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rt.Close(t.Context()) })
	return rt
}

func mustRelease(t *testing.T, inst *sango.Instance) {
	t.Helper()
	if err := inst.Release(); err != nil {
		t.Fatalf("release failed: %v", err)
	}
}

type sangoSnapshot = *sango.Snapshot

func TestCoW_SiblingIsolation(t *testing.T) {
	rt := newIsolationRuntime(t, sango.WithPoolSize(0))
	ctx := t.Context()

	parent, err := rt.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer mustRelease(t, parent)

	evalOK(t, parent, `base = 100`)
	snap, err := rt.Snapshot(parent)
	if err != nil {
		t.Fatal(err)
	}

	f1, err := rt.Restore(ctx, snap)
	if err != nil {
		t.Fatal(err)
	}
	defer mustRelease(t, f1)

	f2, err := rt.Restore(ctx, snap)
	if err != nil {
		t.Fatal(err)
	}
	defer mustRelease(t, f2)

	evalOK(t, f1, `secret = "from_f1"`)
	evalOK(t, f1, `base = 111`)

	if got := evalOK(t, f2, `str("secret" in dir())`); got != "'False'" {
		t.Fatalf("sibling leak: f2 sees f1's variable (got %s)", got)
	}
	if got := evalOK(t, f2, `base`); got != "100" {
		t.Fatalf("sibling leak: f2 sees f1's mutation (got %s, want 100)", got)
	}

	evalOK(t, f2, `base = 222`)
	if got := evalOK(t, f1, `base`); got != "111" {
		t.Fatalf("sibling leak: f1 clobbered by f2 (got %s, want 111)", got)
	}

	if got := evalOK(t, parent, `base`); got != "100" {
		t.Fatalf("parent mutated by children (got %s, want 100)", got)
	}
}

func TestCoW_NoResidueAfterRelease(t *testing.T) {
	rt := newIsolationRuntime(t, sango.WithPoolSize(4))
	ctx := t.Context()

	parent, err := rt.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer mustRelease(t, parent)

	evalOK(t, parent, `base = 1`)
	snap, err := rt.Snapshot(parent)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 16; i++ {
		f, err := rt.Restore(ctx, snap)
		if err != nil {
			t.Fatal(err)
		}

		if got := evalOK(t, f, `str("residue" in dir())`); got != "'False'" {
			t.Fatalf("iteration %d: residue from a previous session (got %s)", i, got)
		}
		if got := evalOK(t, f, `base`); got != "1" {
			t.Fatalf("iteration %d: base polluted (got %s, want 1)", i, got)
		}

		evalOK(t, f, fmt.Sprintf(`residue = %d`, i))
		mustRelease(t, f)
	}
}

func TestCoW_SnapshotIsIndependentOfLaterParentMutation(t *testing.T) {
	rt := newIsolationRuntime(t, sango.WithPoolSize(0))
	ctx := t.Context()

	parent, err := rt.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer mustRelease(t, parent)

	evalOK(t, parent, `marker = "at_snapshot_time"`)
	evalOK(t, parent, `value = 7`)

	snap, err := rt.Snapshot(parent)
	if err != nil {
		t.Fatal(err)
	}

	evalOK(t, parent, `marker = "after_snapshot"`)
	evalOK(t, parent, `value = 999`)
	evalOK(t, parent, `padding = bytearray(2 * 1024 * 1024)`)

	f, err := rt.Restore(ctx, snap)
	if err != nil {
		t.Fatal(err)
	}
	defer mustRelease(t, f)

	if got := evalOK(t, f, `marker`); got != `'at_snapshot_time'` {
		t.Fatalf("snapshot captured later parent state (got %s)", got)
	}
	if got := evalOK(t, f, `value`); got != "7" {
		t.Fatalf("snapshot captured later parent state (got %s, want 7)", got)
	}
	if got := evalOK(t, f, `str("padding" in dir())`); got != "'False'" {
		t.Fatalf("post-snapshot parent allocation leaked into fork (got %s)", got)
	}
}

func TestCoW_PageBoundaryIsolation(t *testing.T) {
	const (
		wasmPage = 64 * 1024
		nPages   = 64
		nForks   = 8
	)

	rt := newIsolationRuntime(t, sango.WithPoolSize(0))
	ctx := t.Context()

	parent, err := rt.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer mustRelease(t, parent)

	evalOK(t, parent, fmt.Sprintf(`buf = bytearray(%d)`, nPages*wasmPage))
	snap, err := rt.Snapshot(parent)
	if err != nil {
		t.Fatal(err)
	}

	forks := make([]*sango.Instance, 0, nForks)
	defer func() {
		for _, f := range forks {
			mustRelease(t, f)
		}
	}()

	for i := 0; i < nForks; i++ {
		f, err := rt.Restore(ctx, snap)
		if err != nil {
			t.Fatal(err)
		}
		forks = append(forks, f)

		mark := byte(i + 1)
		evalOK(t, f, fmt.Sprintf(`buf[%d] = %d`, i*wasmPage, mark))
		evalOK(t, f, fmt.Sprintf(`buf[%d] = %d`, (i+1)*wasmPage-1, mark))
	}

	for i, f := range forks {
		mark := i + 1

		if got := evalOK(t, f, fmt.Sprintf(`buf[%d]`, i*wasmPage)); got != fmt.Sprint(mark) {
			t.Fatalf("fork %d lost its own write at page head (got %s, want %d)", i, got, mark)
		}
		if got := evalOK(t, f, fmt.Sprintf(`buf[%d]`, (i+1)*wasmPage-1)); got != fmt.Sprint(mark) {
			t.Fatalf("fork %d lost its own write at page tail (got %s, want %d)", i, got, mark)
		}

		for j := 0; j < nForks; j++ {
			if j == i {
				continue
			}
			if got := evalOK(t, f, fmt.Sprintf(`buf[%d]`, j*wasmPage)); got != "0" {
				t.Fatalf("fork %d sees fork %d's write at page %d (got %s)", i, j, j, got)
			}
		}
	}

	if got := evalOK(t, parent, `str(any(buf))`); got != "'False'" {
		t.Fatalf("parent buffer mutated by forks (got %s)", got)
	}
}

func TestCoW_MemoryGrowInForkDoesNotAffectOthers(t *testing.T) {
	rt := newIsolationRuntime(t, sango.WithPoolSize(0))
	ctx := t.Context()

	parent, err := rt.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer mustRelease(t, parent)

	evalOK(t, parent, `canary = "intact"`)
	evalOK(t, parent, `small = bytearray(1024); small[0] = 42`)
	snap, err := rt.Snapshot(parent)
	if err != nil {
		t.Fatal(err)
	}

	grower, err := rt.Restore(ctx, snap)
	if err != nil {
		t.Fatal(err)
	}
	defer mustRelease(t, grower)

	quiet, err := rt.Restore(ctx, snap)
	if err != nil {
		t.Fatal(err)
	}
	defer mustRelease(t, quiet)

	// grower does a memory.grow and writes to the new allocation.  quiet should not see any of it.
	evalOK(t, grower, `big = bytearray(64 * 1024 * 1024)`)
	evalOK(t, grower, `big[0] = 1; big[-1] = 2`)
	evalOK(t, grower, `canary = "clobbered"`)

	if got := evalOK(t, quiet, `canary`); got != `'intact'` {
		t.Fatalf("memory.grow in one fork clobbered another (got %s)", got)
	}
	if got := evalOK(t, quiet, `small[0]`); got != "42" {
		t.Fatalf("memory.grow corrupted a sibling's buffer (got %s, want 42)", got)
	}
	if got := evalOK(t, quiet, `str("big" in dir())`); got != "'False'" {
		t.Fatalf("grown allocation leaked into a sibling (got %s)", got)
	}

	// parent does not see the fork's mutation
	if got := evalOK(t, parent, `canary`); got != `'intact'` {
		t.Fatalf("memory.grow in a fork clobbered the parent (got %s)", got)
	}

	// grower sees its own mutation
	if got := evalOK(t, grower, `big[0]`); got != "1" {
		t.Fatalf("grower lost its own write (got %s)", got)
	}
}

func TestCoW_ForkOfForkChain(t *testing.T) {
	rt := newIsolationRuntime(t, sango.WithPoolSize(0))
	ctx := t.Context()

	root, err := rt.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer mustRelease(t, root)

	evalOK(t, root, `depth = 0; trail = "root"`)
	snap0, err := rt.Snapshot(root)
	if err != nil {
		t.Fatal(err)
	}

	child, err := rt.Restore(ctx, snap0)
	if err != nil {
		t.Fatal(err)
	}
	evalOK(t, child, `depth = 1; trail = trail + ">child"`)
	snap1, err := rt.Snapshot(child)
	if err != nil {
		t.Fatal(err)
	}

	grandchild, err := rt.Restore(ctx, snap1)
	if err != nil {
		t.Fatal(err)
	}
	defer mustRelease(t, grandchild)
	evalOK(t, grandchild, `depth = 2; trail = trail + ">grandchild"`)

	mustRelease(t, child)

	if got := evalOK(t, grandchild, `depth`); got != "2" {
		t.Fatalf("grandchild broken after intermediate release (got %s)", got)
	}
	if got := evalOK(t, grandchild, `trail`); got != `'root>child>grandchild'` {
		t.Fatalf("grandchild state corrupted (got %s)", got)
	}

	revived, err := rt.Restore(ctx, snap1)
	if err != nil {
		t.Fatalf("restore from a snapshot whose source was released: %v", err)
	}
	defer mustRelease(t, revived)

	if got := evalOK(t, revived, `trail`); got != `'root>child'` {
		t.Fatalf("snapshot invalidated by releasing its source (got %s)", got)
	}

	// 大元は無傷
	if got := evalOK(t, root, `depth`); got != "0" {
		t.Fatalf("root mutated by descendants (got %s)", got)
	}
}

func TestCoW_ConcurrentForkCorrectness(t *testing.T) {
	const (
		workers = 16
		rounds  = 8
	)

	rt := newIsolationRuntime(t, sango.WithPoolSize(0))
	ctx := t.Context()

	parent, err := rt.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer mustRelease(t, parent)

	evalOK(t, parent, `base = 1000; buf = bytearray(256 * 1024)`)
	snap, err := rt.Snapshot(parent)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, workers*rounds)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				want := w*1000 + r

				f, err := rt.Restore(ctx, snap)
				if err != nil {
					errCh <- fmt.Errorf("worker %d round %d: restore: %w", w, r, err)
					return
				}

				if _, err := f.Eval(ctx, []byte(fmt.Sprintf(`mine = %d`, want))); err != nil {
					f.Release()
					errCh <- fmt.Errorf("worker %d round %d: eval: %w", w, r, err)
					return
				}
				// other workers should not see this write
				if _, err := f.Eval(ctx, []byte(`buf[0] = 1`)); err != nil {
					f.Release()
					errCh <- fmt.Errorf("worker %d round %d: eval buf: %w", w, r, err)
					return
				}

				res, err := f.Eval(ctx, []byte(`mine`))
				if err != nil {
					f.Release()
					errCh <- fmt.Errorf("worker %d round %d: read back: %w", w, r, err)
					return
				}
				if !res.OK() || string(res.Value) != fmt.Sprint(want) {
					f.Release()
					errCh <- fmt.Errorf("worker %d round %d: cross-contamination: got %q, want %d",
						w, r, res.Value, want)
					return
				}

				// base は誰も書き換えていないので不変であるべき
				res, err = f.Eval(ctx, []byte(`base`))
				if err != nil {
					f.Release()
					errCh <- fmt.Errorf("worker %d round %d: read base: %w", w, r, err)
					return
				}
				if string(res.Value) != "1000" {
					f.Release()
					errCh <- fmt.Errorf("worker %d round %d: base corrupted: got %q", w, r, res.Value)
					return
				}

				if err := f.Release(); err != nil {
					errCh <- fmt.Errorf("worker %d round %d: release: %w", w, r, err)
					return
				}
			}
		}(w)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}

	if got := evalOK(t, parent, `base`); got != "1000" {
		t.Fatalf("parent corrupted by concurrent forks (got %s)", got)
	}
	if got := evalOK(t, parent, `buf[0]`); got != "0" {
		t.Fatalf("parent buffer written by a fork (got %s)", got)
	}
}

func TestCoW_NumpyBufferIsolation(t *testing.T) {
	rt := newIsolationRuntime(t, sango.WithPoolSize(0))
	ctx := t.Context()

	parent, err := rt.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer mustRelease(t, parent)

	evalOK(t, parent, `import numpy as np`)
	evalOK(t, parent, `arr = np.zeros(1_000_000, dtype=np.int64)`)
	snap, err := rt.Snapshot(parent)
	if err != nil {
		t.Fatal(err)
	}

	f1, err := rt.Restore(ctx, snap)
	if err != nil {
		t.Fatal(err)
	}
	defer mustRelease(t, f1)

	f2, err := rt.Restore(ctx, snap)
	if err != nil {
		t.Fatal(err)
	}
	defer mustRelease(t, f2)

	// fi re-writes the numpy buffer.  f2 and parent should not see it.
	evalOK(t, f1, `arr[:] = 7`)

	if got := evalOK(t, f1, `str(arr.sum())`); got != "'7000000'" {
		t.Fatalf("f1 lost its own write (got %s)", got)
	}
	if got := evalOK(t, f2, `str(arr.sum())`); got != "'0'" {
		t.Fatalf("numpy buffer leaked across siblings (got %s, want '0')", got)
	}
	if got := evalOK(t, parent, `str(arr.sum())`); got != "'0'" {
		t.Fatalf("numpy buffer leaked into parent (got %s, want '0')", got)
	}
}
