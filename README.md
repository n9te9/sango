# sango

**A forkable, embeddable code-execution sandbox for AI agents — pure Go, no CGO, no containers.**

sango runs untrusted, LLM-generated JavaScript and Python inside a WebAssembly
linear memory on your Go heap. Instances are handed out from a pre-initialized
pool in microseconds, and any execution state can be snapshotted, restored,
and **forked** — turning tree search, speculative execution, and
pause/resume into cheap memory operations.

```
Apple M2 Pro                     QuickJS (JS)    CPython 3.13    CPython + numpy
──────────────────────────────────────────────────────────────────────────────
Eval  (1 + 1)                    5.7 µs          17 µs           TBD
Eval  (stdlib, json.dumps)      —                48 µs           TBD
Acquire  (clean instance)        173 µs          1.5 ms          TBD
Fork  (restore a snapshot)       158 µs          1.4 ms          TBD
Snapshot                         72 µs / 1.3 MB  0.5 ms / 13 MB  TBD
Cold init  (paid once, at New)   264 ms          1.7 s           TBD
```

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
2. **Microsecond–millisecond provisioning.** Interpreter initialization runs
   once, at `New`. The resulting memory image is the *golden snapshot*
   (think: base image). Acquire = restore = mostly a memcpy — ~1,000×
   cheaper than cold init, and fast enough to sit on a web request path.
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
rt, _ := sango.New(ctx, cpython.Wasm(), cpython.CPython(),
	sango.WithWASI(), cpython.WithStdlib())

sess, _ := rt.Acquire(ctx)
defer sess.Release()

sess.Eval(ctx, []byte(`data = [3, 1, 4, 1, 5]`))
res, _ := sess.Eval(ctx, []byte(`sum(data) / len(data)`))
// res.Value == "2.8" — state lives in the instance's linear memory
```

Guest errors are values, not Go errors — a broken snippet is a *normal*
outcome for LLM-generated code, and `res.Err` is exactly what you feed back
to the model:

```go
res, err := inst.Eval(ctx, code)
if err != nil       { /* infra problem: runtime, memory, trap        */ }
if !res.OK()        { /* the code was wrong: send res.Err to the LLM */ }
```

## Fork

```go
inst.Eval(ctx, []byte(`x = 40`))
snap, _ := rt.Snapshot(inst)      // freeze the session: just bytes

fork, _ := rt.Restore(ctx, snap)  // duplicate it (~memcpy)
fork.Eval(ctx, []byte(`x = -999`))
fork.Release()                    // discard the experiment

inst.Eval(ctx, []byte(`x`))       // "40" — the original is untouched
```

Closures, imports, interpreter heap — everything rides along, because
everything lives in the linear memory. `examples/03-tree` runs the same
search tree with and without fork; at depth 6 the fork strategy does
**5.1× fewer evals in 5.1× less wall time**, and the gap grows with depth.
That growth is the argument for forkable sandboxes in tree search,
speculative execution, and RL rollouts.

Snapshots carry a header (adapter ID + wasm build hash) and restoring one
against the wrong runtime is rejected explicitly rather than corrupting
silently.

## The scientific stack

numpy is available as a separate CPython build with the C extensions
statically linked into the wasm module. Same five methods, same
snapshot/fork semantics:

```go
rt, _ := sango.New(ctx, cpython.WasmNumPy(), cpython.CPython(),
	sango.WithWASI(), cpython.WithStdlib())

sess, _ := rt.Acquire(ctx)
sess.Eval(ctx, []byte(`import numpy as np`))
res, _ := sess.Eval(ctx, []byte(`np.fft.fft(np.arange(8)).real.sum()`))
```

Because the interpreter is pre-imported into the golden snapshot, the
multi-hundred-millisecond `import numpy` cost is paid once at `New`, not per
acquire — the same trade the base build makes for interpreter startup.

**What this changes, beyond throughput:**

- **Error boundaries get coarser.** Pure-Python failures are all Python
  exceptions, so `res.Err` is always something an LLM can act on. C
  extensions add paths that terminate as wasm traps instead, and a trapped
  instance cannot be trusted afterwards — its refcounts and C stack are
  mid-operation. sango surfaces these as Go errors (`err != nil`), and the
  correct recovery is to discard the instance and restore from a snapshot,
  not to catch and continue. In practice the common LLM mistakes (shape
  mismatches, dtype errors, API misuse) still arrive as ordinary Python
  exceptions; traps are the rare tail.
- **Forks share their RNG state.** `numpy.random`'s default generator seeds
  itself from OS entropy at import time. A snapshot taken after import
  captures that seed, so every fork produces the *identical* random stream.
  This is deterministic-by-default, which is often what you want for
  reproducible rollouts — but reseed explicitly after `Restore` if you need
  independent draws.
- **The package set is fixed at build time.** Static linking is what makes
  snapshot/fork work at all, and the cost is that no C-extension package can
  be added at runtime. Pure-Python packages can still be dropped into the
  VFS; anything with a compiled component requires rebuilding the wasm. This
  also means security updates to numpy ship as a new wasm binary, and every
  existing snapshot is invalidated by the build-hash header.
- **Interruption granularity.** A long ndarray operation has no Python
  bytecode boundaries, so `context` cancellation stops it by terminating the
  instance rather than raising into the guest. Budget for this the same way
  you budget for traps.
- **Size and snapshot cost.** The numpy build is substantially larger than
  the stdlib-only one in both binary size and per-snapshot bytes (TBD —
  measure before assuming it fits your pooling budget). Keep it as a
  separate import so plain-Python workloads do not pay for it.

pandas: TBD — additionally carries data-file dependencies (tzdata) that must
be mounted into the VFS and go stale on their own schedule.

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
- Statically linked C extensions run inside the same linear memory and gain
  no additional host capability. They do widen the trap surface: memory
  safety bugs in the extension become instance termination, not host
  compromise.

## Scope, honestly

- **Packages are chosen at build time, not by pip.** The stdlib-only build
  covers most agent glue code (math, dates, regex, JSON); the numpy build
  covers the common analysis path. Anything else with a compiled component
  means building your own wasm. If you need arbitrary `pip install` at
  runtime, a remote heavyweight sandbox (E2B, Daytona, …) is the right
  tool — sango is the fast path next to it, not a replacement.
- **Bring your own build.** The wasm binary is an ordinary argument to
  `sango.New`. If you build a custom CPython with extra modules baked in,
  sango will run it; the snapshot header keeps builds from mixing.
- **Sizes.** QuickJS adds ~1 MB to your binary; CPython (interpreter +
  stdlib zip) adds ~30 MB; the numpy build adds TBD. They are separate
  packages — import only what you use.
- **Long-running / adversarial code.** Use `context` deadlines on `Eval`
  (wazero interrupts on cancellation) and memory limits for hostile guests.
  Note that native code sections interrupt at coarser granularity than
  Python bytecode. Hardening options are being expanded — see the issues.

## How it fits together

```
your Go app
└── sango (core: Runtime / Instance / Snapshot — 5 methods)
    ├── adapter/quickjs   QuickJS-ng built for wasm32-wasi, //go:embed'd
    ├── adapter/cpython   CPython 3.13 + stdlib zip, //go:embed'd
    │                     ├── stdlib-only build
    │                     └── numpy 2.1.3 statically linked (incl. numpy.fft)
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
make -C wasm/cpython numpy     # cross-build numpy and link it in
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