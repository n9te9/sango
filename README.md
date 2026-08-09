# sango

**A forkable, embeddable code-execution sandbox for AI agents — pure Go, no CGO, no containers.**

sango runs untrusted, LLM-generated JavaScript and Python inside a WebAssembly
linear memory on your Go heap. Instances are handed out from a pre-initialized
pool in about a millisecond, and any execution state can be snapshotted,
restored, and **forked** — turning tree search, speculative execution, and
pause/resume into cheap memory operations.

```
Apple M2 Pro                    CPython 3.13 (numpy + pandas linked in)
──────────────────────────────────────────────────────────────────────
Eval   1 + 1                     47 µs
Eval   json.dumps               106 µs
Eval   numpy, 1M-element sum    587 µs
Acquire   from a warm pool      1.43 ms  / 39.6 MB, 76k allocs
Acquire   with no pool          3.52 ms
Fork      restore a snapshot    3.32 ms
Fork      restore a numpy set   3.93 ms
Snapshot                        751 µs   / 19.4 MB
Snapshot  after import numpy    1.04 ms  / 26.0 MB
import numpy   (per session)    523 ms   ← see "Keep the import off the request path"
Cold init      (once, at New)   ~10 s    (single samples, 10.0–11.1 s)
Cold init      (cached)         916 ms   ← see "Cache the compilation"

Apple M2 Pro                    QuickJS
──────────────────────────────────────────────────────────────────────
Eval  (1 + 1)                    5.7 µs
Acquire  (clean instance)        173 µs
Fork  (restore a snapshot)       158 µs
Snapshot                         72 µs / 1.3 MB
Cold init  (paid once, at New)   264 ms
```

All numbers were measured on the same Apple M2 Pro.
Reproduce with `go test -bench . -benchmem -run '^$' ./adapter/...`.

## Why

AI agents generate code you cannot trust, dozens of times per task. Your
options today are running it in-process (one prompt injection away from
disaster), spinning containers or microVMs (hundreds of ms, heavy ops), or
calling a remote sandbox API (latency, cost, your data leaves the building).

sango is the fourth option: `go get` a library, and your existing Go process
grows an execution space where cross-tenant leaks are structurally
impossible. Three properties fall out of one design decision — *the entire
execution state is a `[]byte` of linear memory*:

1. **Isolation by construction.** The guest has no filesystem, no network,
   no syscalls unless you explicitly grant them (default deny). Every
   `Acquire` starts from a pristine golden snapshot; `Release` destroys the
   instance. Nothing survives between sessions.
2. **Millisecond provisioning from a warm pool.** Interpreter
   initialization runs once, at `New`. The resulting memory image is the
   *golden snapshot* (think: base image). A warm `Acquire` copies that image
   at memcpy speed — 1.43 ms for a 19 MB CPython image, ~7,000× cheaper
   than the ~10 s cold init, and fast enough to sit on a web request path.
3. **Execution state as a value.** `Snapshot` returns bytes you can store,
   ship, and `Restore` into as many forks as you like. Branch an agent's
   session, try candidates in parallel, keep the winner, discard the rest.

## Quick start

```go
package main

import (
	"context"
	"fmt"

	"github.com/n9te9/sango"
	"github.com/n9te9/sango/adapter/quickjs"
)

func main() {
	ctx := context.Background()

	rt, _ := sango.New(ctx, quickjs.Wasm(), quickjs.QuickJS(), sango.WithWASI())
	defer rt.Close(ctx)

	inst, _ := rt.Acquire(ctx) // clean instance from the pool
	defer inst.Release()

	res, _ := inst.Eval(ctx, []byte(`[..."sango"].reverse().join("")`))
	fmt.Println(string(res.Value)) // "ognas"
}
```

Python works the same way — sessions keep state across evals:

```go
stdlib, _ := cpython.WithStdlib()
rt, _ := sango.New(ctx, cpython.Wasm(), cpython.CPython(),
	sango.WithWASI(), stdlib)

sess, _ := rt.Acquire(ctx)
defer sess.Release()

sess.Eval(ctx, []byte(`data = [3, 1, 4, 1, 5]`))
res, _ := sess.Eval(ctx, []byte(`sum(data) / len(data)`))
// res.Value == "2.8" — state lives in the instance's linear memory
```

numpy and pandas are linked into the same build — no extra option, no pip:

```go
sess.Eval(ctx, []byte(`import numpy as np`))
res, _ := sess.Eval(ctx, []byte(`float(np.fft.fft(np.arange(4096.0)).real.sum())`))
```

Guest errors are values, not Go errors — a broken snippet is a *normal*
outcome for LLM-generated code, and `res.Err` is exactly what you feed back
to the model:

```go
res, err := inst.Eval(ctx, code)
if err != nil       { /* infra problem: runtime, memory, trap        */ }
if !res.OK()        { /* the code was wrong: send res.Err to the LLM */ }
```

## Keep the import off the request path

`import numpy` costs **523 ms** on a fresh instance. That is container
latency, and it is paid by every session that touches numpy. It is also
entirely avoidable: import once at startup, snapshot, and `Restore` per
session instead of `Acquire`.

```go
warm, _ := rt.Acquire(ctx)
warm.Eval(ctx, []byte("import numpy as np\nimport pandas as pd"))
base, _ := rt.Snapshot(warm)   // 26 MB with numpy imported
warm.Release()

// per session, forever after:
sess, _ := rt.Restore(ctx, base)   // 3.93 ms — 133× cheaper than re-importing
defer sess.Release()
```

Restore scales sub-linearly with image size: growing the snapshot from
19.4 MB to 26.0 MB (+34%) costs +18% in restore time, because a fixed
instantiation cost dominates. Pre-importing is close to free; re-importing
is not.

If your workload is stdlib-only, plain `Acquire` remains the fast path at
1.43 ms and there is nothing to do.

## Cache the compilation

Cold init is dominated by wazero compiling the wasm module, and wazero can
cache the compiled form on disk:

```go
rt, _ := sango.New(ctx, cpython.Wasm(), cpython.CPython(),
	sango.WithWASI(), stdlib,
	sango.WithCompilationCacheDir(cacheDir))
```

The first `New` pays the full ~10 s and writes the cache. Every later `New`
— including in a freshly started process — reads it back in **916 ms**, an
11× cut. If your deployment restarts processes at all, use this; it turns
"call `New` once at startup, never lazily" from a hard requirement into a
mild preference.

## Fork

```go
inst.Eval(ctx, []byte(`x = 40`))
snap, _ := rt.Snapshot(inst)      // freeze the session: just bytes

fork, _ := rt.Restore(ctx, snap)  // duplicate it
fork.Eval(ctx, []byte(`x = -999`))
fork.Release()                    // discard the experiment

inst.Eval(ctx, []byte(`x`))       // "40" — the original is untouched
```

Closures, imports, interpreter heap — everything rides along, because
everything lives in the linear memory. `examples/03-tree` runs the same
search tree with and without fork; at depth 6 the fork strategy does
**5.1× fewer evals in 5.1× less wall time**, and the gap grows with depth.

For Python the argument is sharper than for JS. A fork costs 3.3 ms, so
forking beats re-execution whenever replaying the session costs more than
that — and any session with a scientific import already costs 523 ms to
replay. Fork is how a numpy session stops being expensive.

Fan-out cost depends on whether forks stay alive. Forks that are created,
used, and released are flat: 3.4–3.5 ms per fork whether you make 10 or
1,000 of them. Forks held live are a memory question: each one keeps its own
39.6 MB of linear memory, nothing is shared, and a thousand live forks hold
39.6 GB while per-fork time degrades to ~13 ms under the GC pressure.
Release forks as soon as a candidate loses; bound your *live* width, not
your total fork count.

Snapshots carry a header (adapter ID + wasm build hash) and restoring one
against the wrong runtime is rejected explicitly rather than corrupting
silently.

## Cost model

The numbers that determine whether sango fits your workload:

- **Copying dominates.** Snapshot runs at 25–28 GB/s — single-core memcpy
  speed on this machine. The only lever on provisioning latency is a
  smaller image.
- **The pool hides ~2 ms of instantiation.** A cold `Acquire` (3.52 ms) does
  the same copy as a warm one (1.43 ms) plus instantiation. The work is not
  eliminated by pooling, only moved off the critical path — which is why
  `Restore` (3.32 ms), having no pool behind it, is slower than a warm
  `Acquire`.
- **Acquire does not scale with cores.** ~700 acquires/s serial, and no gain
  from running acquires in parallel (1.52 ms/op across 12 cores vs 1.43 ms
  serial). Each acquire costs ~39.6 MB and ~76k allocations, so throughput
  is bounded by memory bandwidth and GC, not by parallelism. Fork-and-eval
  *does* parallelize — 3.5 ms serial vs 1.5 ms/op across cores — because
  the eval work overlaps. Size your pool for latency, not for throughput.
- **Cold init is ~10 s uncached, 916 ms cached.** Call `New` at process
  startup and pass `WithCompilationCacheDir`. The uncached figure comes
  from single samples (10.0–11.1 s); re-measure with `-benchtime 5x` if it
  matters to you.
- **numpy in wasm is scalar.** The 128×128 matmul runs at ~1.5 GFLOPS and a
  1M-element sum at ~14 GB/s — no SIMD, no BLAS. Correct, portable, and one
  to two orders of magnitude off native. Use it for the analysis an agent
  actually writes, not for numerical throughput.

## Security model

- The guest is a wasm module executed by [wazero](https://wazero.io); its
  world is a linear memory on the Go heap. There is no path to the host OS.
- **Default deny.** No preopened directories, no environment, no network.
  `WithWASI()` grants only the benign syscall surface (clock, random) that
  wasi-libc needs. The CPython stdlib is mounted read-only from an embedded
  zip — the host filesystem is never touched.
- QuickJS is built **without** `quickjs-libc` (no `std`/`os` modules): the
  guest lacks even the vocabulary to reach for files or processes.
- Every `Acquire` starts from the golden snapshot; instances are destroyed
  on `Release`, never scrubbed and reused. Session-to-session leaks are a
  structural impossibility, not a cleanup discipline.
- The committed `.wasm` binaries are reproducible from the C sources in
  `wasm/` and verified in CI (rebuild + byte-for-byte diff).
- Statically linked C extensions run in the same linear memory and gain no
  additional host capability. They do widen the trap surface: a memory
  safety bug in an extension becomes instance termination, not host
  compromise. A trapped instance is not recoverable — discard it and restore
  from a snapshot.

## Scope, honestly

- **One Python build, numpy and pandas included.** There is no stdlib-only
  variant; every CPython user carries the scientific stack. That buys a
  single supported configuration and no build matrix, and it costs binary
  size, a ~10 s uncached cold init, and a 19 MB baseline image that every
  acquire copies.
- **Forks don't share memory.** Every live fork is a full copy of its
  linear memory. There is no copy-on-write between forks today, so
  concurrent live forks cost 39.6 MB each — plan capacity around live
  width, and release aggressively.
- **Packages are chosen at build time, not by pip.** Pure-Python modules can
  be added to the VFS; anything with a compiled component means building
  your own wasm. If you need arbitrary `pip install` at runtime, a remote
  heavyweight sandbox (E2B, Daytona, …) is the right tool — sango is the
  fast path next to it, not a replacement.
- **Determinism cuts both ways.** `numpy.random`'s default generator seeds
  itself from OS entropy at import time, so a snapshot taken after import
  gives every fork the identical stream. Good for reproducible rollouts;
  reseed explicitly after `Restore` if you need independent draws.
- **Bring your own build.** The wasm binary is an ordinary argument to
  `sango.New`. If you build a custom CPython with extra modules baked in,
  sango will run it; the snapshot header keeps builds from mixing.
- **Sizes.** QuickJS adds ~1 MB to your binary; CPython with numpy and
  pandas adds TBD. They are separate packages — import only what you use.
- **Long-running / adversarial code.** Use `context` deadlines on `Eval`
  (wazero interrupts on cancellation) and memory limits for hostile guests.
  Native code sections interrupt at coarser granularity than Python
  bytecode: a long ndarray operation is stopped by terminating the instance,
  not by raising into the guest. Hardening options are being expanded — see
  the issues.

## How it fits together

```
your Go app
└── sango (core: Runtime / Instance / Snapshot — 5 methods)
    ├── adapter/quickjs   QuickJS-ng built for wasm32-wasi, //go:embed'd
    ├── adapter/cpython   CPython 3.13 + stdlib zip + numpy + pandas, //go:embed'd
    └── wazero            pure-Go wasm runtime — no CGO anywhere
```

The core knows no language. Each adapter's guest implements a four-function
ABI (`allocate` / `deallocate` / `initialize` / `eval`, tagged results), so
the host is a thin pipe: code bytes in, result bytes out. Adding a language
means writing one C wrapper and passing a conformance test suite — the
interpreter state lives entirely in linear memory, which is what makes
snapshot/fork work.

Rebuilding the wasm from source:

```bash
brew install binaryen   # wasm-opt, needed for the exception-handling transform
make -C wasm/quickjs install   # pin + fetch wasi-sdk (once)
make -C wasm/quickjs           # clone quickjs-ng, build, emit adapter/quickjs/quickjs.wasm
make -C wasm/cpython install && make -C wasm/cpython
```

## Examples

| | | |
|---|---|---|
| [`examples/01-linear`](./examples/01-linear) | JS | the agent glue loop, µs per step, error feedback |
| [`examples/02-session`](./examples/02-session) | Python | Code-Interpreter sessions + clean-slate isolation |
| [`examples/03-tree`](./examples/03-tree) | JS | fork vs re-execution: the measured order effect |

All run offline with `go run` — no API keys. Each marks the single line
where your LLM call plugs in.

## Related projects

- **E2B / Daytona / microVM sandboxes** — full computers for agents
  (pip, browsers, long jobs). Heavier, remote, ms–s provisioning. sango is
  the in-process fast path for the other 80% of executions.
- **langchain quickjs-rs** — the same linear-memory-snapshot insight,
  Python/Rust ecosystem, built for pause/resume of Deep Agents.
- **wazero** — the pure-Go wasm runtime sango stands on.

## Status

APIs may still shift before v1. The conformance suite
(`adapter/adaptertest`) defines the contract every language must satisfy:
eval, guest-error separation, session persistence, clean acquires,
snapshot/restore fidelity, fork isolation, post-fork imports.

## License

MIT
