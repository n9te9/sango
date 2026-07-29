# sango

**A forkable, embeddable code-execution sandbox for AI agents — pure Go, no CGO, no containers.**

sango runs untrusted, LLM-generated JavaScript and Python inside a WebAssembly
linear memory on your Go heap. Instances are handed out from a pre-initialized
pool in microseconds, and any execution state can be snapshotted, restored,
and **forked** — turning tree search, speculative execution, and
pause/resume into cheap memory operations.

```
Apple M4 Max                    CPython 3.13 (numpy + pandas linked in)
──────────────────────────────────────────────────────────────────────
Eval   1 + 1                     33 µs
Eval   json.dumps                70 µs
Eval   numpy, 1M-element sum    438 µs
Acquire   from a warm pool      877 µs   / 39 MB, 76k allocs
Acquire   with no pool          2.2 ms
Fork      restore a snapshot    2.3 ms
Fork      restore a numpy set   2.4 ms
Snapshot                        441 µs   / 19.4 MB
Snapshot  after import numpy    605 µs   / 26.0 MB
import numpy   (per session)    372 ms   ← see "Keep the import off the request path"
Cold init      (once, at New)   7.6 s    (single sample; use -benchtime 5x)

Apple M2 Pro                    QuickJS
──────────────────────────────────────────────────────────────────────
Eval  (1 + 1)                    5.7 µs
Acquire  (clean instance)        173 µs
Fork  (restore a snapshot)       158 µs
Snapshot                         72 µs / 1.3 MB
Cold init  (paid once, at New)   264 ms
```

The QuickJS column has not been re-measured on the same machine as the CPython
column; do not compare rows across the two tables.

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
2. **Sub-millisecond provisioning from a warm pool.** Interpreter
   initialization runs once, at `New`. The resulting memory image is the
   *golden snapshot* (think: base image). A warm `Acquire` copies that image
   at roughly memcpy speed — 877 µs for a 19 MB CPython image, ~8,600×
   cheaper than the 7.6 s cold init, and fast enough to sit on a web request
   path.
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

`import numpy` costs **372 ms** on a fresh instance. That is container
latency, and it is paid by every session that touches numpy. It is also
entirely avoidable: import once at startup, snapshot, and `Restore` per
session instead of `Acquire`.

```go
warm, _ := rt.Acquire(ctx)
warm.Eval(ctx, []byte("import numpy as np\nimport pandas as pd"))
base, _ := rt.Snapshot(warm)   // 26 MB with numpy imported
warm.Release()

// per session, forever after:
sess, _ := rt.Restore(ctx, base)   // 2.4 ms — 152× cheaper than re-importing
defer sess.Release()
```

Restore scales sub-linearly with image size: growing the snapshot from 19.4 MB
to 26.0 MB (+34%) costs only +8% in restore time, because a fixed
instantiation cost dominates. Pre-importing is close to free; re-importing is
not.

If your workload is stdlib-only, plain `Acquire` remains the fast path at
877 µs and there is nothing to do.

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

For Python the argument is sharper than for JS. A fork costs 2.3 ms, so
forking beats re-execution whenever replaying the session costs more than
that — and any session with a scientific import already costs 372 ms to
replay. Fork is how a numpy session stops being expensive.

Fan-out is flat up to about 16 concurrent live forks (≈2.2 ms per branch at
widths 1, 4 and 16) and degrades beyond it (≈5.0 ms per branch at width 64,
with 2.5 GB of allocation churn per round). Bound your live branch width;
release forks as soon as a candidate loses.

Snapshots carry a header (adapter ID + wasm build hash) and restoring one
against the wrong runtime is rejected explicitly rather than corrupting
silently.

## Cost model

The numbers that determine whether sango fits your workload:

- **Copying dominates.** Snapshot runs at ~44 GB/s and a warm `Acquire` at
  ~45 GB/s — both at single-core memcpy speed on this machine. The only
  lever on provisioning latency is a smaller image.
- **The pool hides ~1.3 ms of instantiation.** A cold `Acquire` (2.2 ms) and
  a `Restore` (2.3 ms) both run at ~18 GB/s effective, versus 45 GB/s warm.
  The work is not eliminated by pooling, only moved off the critical path —
  which is why `Restore`, having no pool behind it, is slower than a warm
  `Acquire`.
- **Acquire does not scale with cores.** 1,140 acquires/s serial, 1,732/s
  across 14 cores — a 1.5× speedup. Each acquire costs ~39 MB and ~76k
  allocations, so throughput is bounded by memory bandwidth and GC, not by
  parallelism. Size your pool for latency, not for throughput, and expect a
  low four-figure ceiling per process.
- **Cold init is 7.6 s.** Call `New` at process startup, never lazily on a
  request. This figure is a single sample; re-measure with `-benchtime 5x`
  if it matters to you.
- **numpy in wasm is scalar.** The 128×128 matmul runs at ~2.1 GFLOPS and a
  1M-element sum at ~18 GB/s — no SIMD, no BLAS. Correct, portable, and one
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
  size, a 7.6 s cold init, and a 19 MB baseline image that every acquire
  copies.
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
- **goccy/wasmify** — compiles *trusted* C/C++ libraries into Go packages
  via wasm (build-time import). sango runs *untrusted* code arriving at
  runtime (execution quarantine). Same port, opposite cargo.
- **wazero** — the pure-Go wasm runtime sango stands on.

## Status

APIs may still shift before v1. The conformance suite
(`adapter/adaptertest`) defines the contract every language must satisfy:
eval, guest-error separation, session persistence, clean acquires,
snapshot/restore fidelity, fork isolation, post-fork imports.

## License

MIT