# Log Analyzer: Step-by-Step Guide

Written for a programmer who already knows Go syntax and wants to understand concurrency deeply, typing every line by hand.

All code in this guide was compiled and run with **go1.27.1 darwin/arm64** (8 logical CPUs). Every output quoted was observed on that machine; timings will differ on yours.

---

## Assumptions (README ambiguities resolved)

| # | Ambiguity | Decision |
| --- | --- | --- |
| A1 | No `go.mod` exists yet | Run `go mod init github.com/sidd-dhegaskar/log-analyzer` (path taken from your git remote). On go1.27.1 this writes `go 1.27.1`, the target version. I checked this. |
| A2 | What is an "endpoint"? | `METHOD PATH`, e.g. `GET /api/users`. `GET` and `POST` on the same path are different endpoints. |
| A3 | Lines per file | 1,000 lines × 1,000 files = 1,000,000 lines (≈47 MB). Configurable with a flag. |
| A4 | Malformed lines | Skipped, counted, and reported as `malformed_lines`. The generator inserts a few on purpose (about 1 in 5,000). |
| A5 | Ties in the top 10 | Higher count first; equal counts sorted by endpoint name. Without this rule the order of tied endpoints could vary between runs. |
| A6 | Average latency | `sum of all latencies / number of requests`, computed from an `int64` sum. Never average the per-file averages (that gives a different, wrong number). |
| A7 | JSON appears in milestone 10, but milestone 2 needs a baseline | JSON output moves to Phase 2. Milestone 10 becomes "finalise the JSON report and CLI contract". See *Order adjustments* below. |
| A8 | "Introduce cancellation" | Ctrl+C (SIGINT) cancels a `context.Context`. A cancelled run prints no report and exits with code 130. A partial report would not match the baseline. |
| A9 | Repo layout | Two binaries (generator and analyzer), so `cmd/loggen` and `cmd/analyze` plus `internal/` packages (engineering guide §1.2 rule 4). |
| A10 | Timestamps | Not parsed or validated. The report does not use them. |
| A11 | Test comparison helper | `github.com/google/go-cmp` (`cmp.Diff`), as the engineering guide §5 recommends. It is the only third-party dependency, and only tests use it. |

---

## What we are building

**In one sentence:** a command-line program that reads 1,000 access-log files and prints one JSON report with request counts per status code, the 10 busiest endpoints and the average latency.

```
 cmd/loggen ──writes──▶  logs/access-0000.log … access-0999.log
                                   │
                                   ▼
                      analyzer.ListFiles(dir)   → []string paths
                                   │
                ┌──────────────────┼──────────────────┐
                ▼                  ▼                  ▼
   accesslog.AnalyzeFile   accesslog.AnalyzeFile  …  (one *Stats per file)
                │                  │                  │
                └──────── merge into one total ───────┘
                                   │
                         total.Report(10)  → Report struct
                                   │
                         encoding/json  → stdout
```

The "merge" box is the part that changes from phase to phase: a plain loop, then goroutines with a mutex, then channels, then a worker pool.

### Files

The repository currently contains only `README.md` and `.git/`. Everything else is created by this guide.

| Path | Status | Package | Role | Goroutines? | Why it is needed |
| --- | --- | --- | --- | --- | --- |
| `README.md` | **existing** | – | Project spec | – | – |
| `go.mod` | new | – | Module path and Go version | – | Every module needs one |
| `cmd/loggen/main.go` | new | `main` | Thin CLI: flags → `loggen.Generate` | no | Milestone 1 |
| `internal/loggen/loggen.go` | new | `loggen` | Writes deterministic log files | no | Tests and benchmarks also generate data, so the logic cannot live in `main` |
| `internal/accesslog/parse.go` | new | `accesslog` | `ParseLine`: one line → `Entry` | no | Parsing is the backend theme; isolated so it is easy to test |
| `internal/accesslog/stats.go` | new | `accesslog` | `Stats`, `Add`, `Merge`, `Report` | no (not concurrency-safe, by design) | One definition of "the counters", shared by all strategies |
| `internal/accesslog/file.go` | new | `accesslog` | `AnalyzeFile`: path → `*Stats` | no | The unit of work every strategy runs |
| `internal/accesslog/parse_test.go` | new | `accesslog` | Table-driven parser tests | no | Tools: `go test` |
| `internal/analyzer/analyzer.go` | new | `analyzer` | Package doc + `ListFiles` | no | Finds the input files |
| `internal/analyzer/sequential.go` | new | `analyzer` | `Sequential`: the baseline | no | Milestone 2 |
| `internal/analyzer/racy.go` | new | `analyzer` | `Racy`: goroutine per file, unsynchronized, **deliberately broken** | yes | Milestones 3–5 and the race experiment |
| `internal/analyzer/locked.go` | new | `analyzer` | `Locked`: goroutine per file + mutex | yes | Milestone 6 |
| `internal/analyzer/channel.go` | new | `analyzer` | `Channel`: goroutine per file + results channel | yes | Milestone 7 |
| `internal/analyzer/pool.go` | new | `analyzer` | `Pool`: fixed workers + cancellation | yes | Milestones 8–9 |
| `internal/analyzer/analyzer_test.go` | new | `analyzer` | Equivalence, cancellation and leak tests | yes | Tools: `-race`, `NumGoroutine` |
| `internal/analyzer/bench_test.go` | new | `analyzer` | Benchmarks | yes | Tools: `go test -bench` |
| `cmd/analyze/main.go` | new | `main` | Thin CLI: flags, Ctrl+C, strategy choice, JSON output | starts signal goroutines (Phase 10) | Milestone 10 |
| `baseline.json` | new (generated) | – | Sequential output every version is checked against | – | Milestone 2 |
| `logs/` | new (generated) | – | 1,000 input files. Add it to `.gitignore` | – | Milestone 1 |

All the strategies stay in the repo as separate functions so you can switch between them with `-mode=...` and benchmark them against each other.

### Packages (`<PACKAGES>`)

| Import | Plain meaning | Where it is used | Source |
| --- | --- | --- | --- |
| `sync` | WaitGroup and Mutex | `analyzer` | README |
| `runtime` | Ask the runtime about goroutines and CPUs | `cmd/analyze`, tests | README |
| `encoding/json` | Turn Go values into JSON text | `cmd/analyze` | README |
| `context` | Carry a cancellation signal across goroutines | `analyzer.Pool`, `cmd/analyze` | README ("done channel or `context`") |
| `testing` | Tests and benchmarks | `*_test.go` | README (tools) |
| `os` | Files, directories, exit codes, stdout and stderr | everywhere | implementation-derived |
| `path/filepath` | Build file paths portably | `loggen`, `analyzer` | implementation-derived |
| `fmt` | Formatted printing, error wrapping | everywhere | implementation-derived |
| `bufio` | Buffered writing (generator) and line-by-line reading (analyzer) | `loggen`, `accesslog` | implementation-derived |
| `math/rand/v2` | Seeded pseudo-random numbers | `loggen` | implementation-derived |
| `time` | Timestamps, durations, timers | `loggen`, `cmd/analyze`, `Pool` | implementation-derived |
| `flag` | Command-line options | `cmd/*` | implementation-derived |
| `strings` | Split and trim text | `accesslog`, `analyzer` | implementation-derived |
| `strconv` | Text → integer | `accesslog` | implementation-derived |
| `errors` | Sentinel errors, `errors.Is`, `errors.Join` | `accesslog`, `analyzer` | implementation-derived |
| `slices` | Sort a slice with a custom order | `accesslog` | implementation-derived |
| `os/signal` | Turn Ctrl+C into a cancelled context | `cmd/analyze` | implementation-derived (README says Ctrl+C) |
| `github.com/google/go-cmp/cmp` | Readable diff of two Go values in tests | `analyzer_test.go` | implementation-derived (engineering guide §5) |

---

## How to read this guide

### Labels

| Label | Meaning |
| --- | --- |
| **[Spec]** | Guaranteed by the Go language specification or the Go memory model |
| **[API]** | Guaranteed by a standard-library package's documentation |
| **[Runtime]** | How the current runtime happens to work. It can change between releases. |
| **[Convention]** | Common engineering practice, not enforced by Go |
| **[Model]** | A teaching simplification. It is useful for reasoning but is not a literal description of the implementation. |

Concept depth: **Familiar** (a brief note), **New** (taught from the foundations), **Advanced** (internals and trade-offs).
Concept scope: **Essential** (needed for this project), **Related** (helps you understand the design), **Advanced** (beyond the project).

### Step 0: concept inventory (from the README only)

| List | Contents |
| --- | --- |
| `<CONCEPTS>` | Goroutines, WaitGroup, Mutex, Channels, Buffered channels, Directional channels, Worker pool, Select, GOMAXPROCS, Confinement. Backend: text parsing, JSON output. |
| `<PATTERNS>` | Fan-out of files to workers → results channel → single aggregator (fan-in); goroutine-per-task; worker pool; done-channel/context cancellation. |
| `<PACKAGES>` | See the table above. |
| `<MILESTONES>` | 1 generator · 2 sequential + baseline · 3 goroutine per file · 4 WaitGroup · 5 shared maps · 6 Mutex · 7 results channel · 8 worker pool · 9 cancellation · 10 JSON |
| `<EXPERIMENTS>` | Remove mutex + `-race` · remove `wg.Wait()` · unbuffered results channel · close results early · vary `GOMAXPROCS` |
| `<TOOLS>` | `go run -race .` / `go test -race ./...` · `go test -bench=.` · `runtime.NumGoroutine()` |

### Dependency map

```
goroutine ──▶ WaitGroup ──▶ data race ──▶ Mutex ──▶ confinement
                  │                                     │
                  └──────────▶ channels ◀───────────────┘
                                  │
                   buffered · directional · close/range
                                  │
                       worker pool (fan-out/fan-in)
                                  │
                     select ──▶ context ──▶ Ctrl+C
                                  │
                     tests (-race, leaks) ──▶ benchmarks ──▶ GOMAXPROCS
```

### Order adjustments (and why)

1. **JSON moves from milestone 10 to Phase 2.** Milestone 2 asks you to save a baseline, and comparing reports needs a fixed serialized form. Milestone 10 keeps the remaining work: the output contract, stderr versus stdout, and exit codes.
2. **Experiments get their own phase (9), between the worker pool and cancellation.** This follows the README's learning cycle (concurrent → break things → production-ready). Two experiments come up naturally earlier and are pointed out where they occur.
3. **`select` is taught in Phase 10, not Phase 7.** It only becomes necessary when a goroutine must wait for two things at once ("send my result" *or* "stop").

### Learning cycle mapping

| README cycle | Phases |
| --- | --- |
| 1 Sequential | 1–2 |
| 2 Introduce concurrency | 3–8 |
| 3 Break things | 5 (race), 9 |
| 4 Production-ready | 10–12 |
| 5 Benchmark | 13 |

---

## Phase 0: Project setup

### 0.1 Create the module

```bash
cd ~/Desktop/personal/log-analyzer
go mod init github.com/sidd-dhegaskar/log-analyzer
cat go.mod
```

Expected:

```
module github.com/sidd-dhegaskar/log-analyzer

go 1.27.1
```

If the `go` line says anything else, run `go mod edit -go=1.27.1`. The `go` line sets the **language version** for the module. It turns on features such as per-iteration loop variables (1.22) and `range` over integers (1.22), which this guide relies on.

### 0.2 Create the folders

```bash
mkdir -p cmd/loggen cmd/analyze internal/loggen internal/accesslog internal/analyzer
printf 'logs/\n/analyze\n/loggen\n*.out\n*.test\n' > .gitignore
```

**Rule applied (engineering guide §1.2):** two binaries mean `cmd/<name>/main.go`, and all logic goes in `internal/` so no other module can import it.

### 0.3 Know your tools

| Command | What it does |
| --- | --- |
| `go run ./cmd/analyze` | Compile and run the analyzer |
| `go build -race` / `go run -race` / `go test -race` | Build with the race detector |
| `go vet ./...` | Static checks (includes copied-mutex and lost-cancel checks) |
| `gofmt -s -w .` | Format |

**Platform note (macOS):** on darwin the race detector does not need cgo. `CGO_ENABLED=0 go build -race` succeeded on this machine. On Linux, `-race` needs cgo and a C toolchain (`CGO_ENABLED=1`, gcc or clang).

**Checkpoint 0:** `go env GOVERSION` prints `go1.27.1`, and `cat go.mod` shows `go 1.27.1`.

---

## Warm-ups (before the project)

The first concurrent milestones (3 and 4) need exactly two ideas: **goroutines** and **WaitGroup**. Practise them in a scratch folder **outside** the project, so the broken programs never end up in your module:

```bash
mkdir -p ~/Desktop/personal/warmups && cd ~/Desktop/personal/warmups
```

A single file outside a module runs with `go run file.go`. No `go.mod` is needed when you import only the standard library.

### W1. Goroutines (New · Essential)

**The problem.** A program normally does one thing at a time. Reading 1,000 files one by one leaves the CPU idle while waiting on the disk, and it leaves 7 of 8 cores unused.

**The core idea.** A goroutine is an independently running function call managed by the Go runtime **[Spec]**. `go f(x)` evaluates `f` and `x` *now*, in the current goroutine, then starts `f(x)` running concurrently and returns immediately, without waiting **[Spec]**.

**Mental model [Model].**

```
main goroutine:  ──go A()──go B()──go C()──── return from main ✗ (program ends)
goroutine A:          ├──────── running ────────┤
goroutine B:                ├──── running ───────────┤  ← killed if main returns first
```

**Analogy.** Handing tasks to coworkers. Saying "please do this" is instant, and you don't wait for them to finish. But if you lock the office (main returns), everyone is sent home mid-task.

**Operations.**

| Operation | Meaning in the model |
| --- | --- |
| `go f(args)` | Evaluate `f` and the args now; start a new goroutine running `f(args)`; continue immediately |
| return from `f` | That goroutine ends. Its return values are discarded. |
| return from `main` | The **whole program exits**. Other goroutines are not waited for **[Spec]**. |

**Lifecycle and ownership.** Creation: whoever runs `go`. Completion: when the function returns. Nothing signals completion automatically. Cancellation: there is **no way to kill a goroutine from outside**. It must notice a signal and return by itself (Phase 10). Results: a goroutine has no return value you can collect. Results must be written somewhere or sent on a channel. Cleanup: its stack is reclaimed when it returns. A goroutine that never returns is a **leak**.

**W1.1 Hit the problem.** Create `w1.go`:

```go
package main

import "fmt"

func main() {
	for i := range 3 {
		go fmt.Println("hello from goroutine", i)
	}
	fmt.Println("main done")
}
```

`range 3` iterates `i = 0, 1, 2` (Go 1.22+ **[Spec]**). `go fmt.Println(...)` evaluates the arguments (`i` included) at the `go` statement.

```bash
go run w1.go
```

**What to observe:** usually only `main done` is printed. Sometimes one or two hellos appear in a random order. `main` returned before the goroutines got to run, and the program exited.

**W1.2 The wrong fix (understand why it is wrong).** Adding `time.Sleep(100 * time.Millisecond)` before `main done` "works" on your laptop. It is **not synchronization**: it guesses how long the work takes. On a loaded machine, or with slower work, it fails silently. It also creates no *happens-before* ordering (Phase 5), so even when the timing works the program is still incorrect under the memory model. We need a tool that **waits for completion**. That tool is W2.

**Failure modes.**

| Mistake | Effect |
| --- | --- |
| `main` returns before goroutines finish | Their work is silently lost |
| A goroutine blocks forever | Leak: memory held until the process exits |
| A panic in any goroutine that is not recovered | The **whole program** crashes, not just that goroutine **[Spec]** |

---

### W2. `sync.WaitGroup` (New · Essential)

**The problem.** `main` must wait until N goroutines have finished, and must not guess with `Sleep`.

**The core idea.** A WaitGroup is **a counter of unfinished tasks plus a waiting room** **[Model]**. Goroutines that call `Wait` sit in the waiting room until the counter reaches zero.

```
counter: 3        waiting room: [main]
  Done → 2
  Done → 1
  Done → 0  ──▶ everyone in the waiting room is released
```

**Analogy.** A tour guide counting heads. "Three people are still in the museum." Each person who comes out is checked off. The bus leaves when the count is zero.

**Operations, defined in terms of the model [API].**

| Operation | Effect on the model |
| --- | --- |
| `var wg sync.WaitGroup` | Counter = 0. The zero value is ready to use. |
| `wg.Add(n)` | counter += n. If it reaches 0, release the waiters. If it goes **below 0, panic**. |
| `wg.Done()` | Same as `wg.Add(-1)` |
| `wg.Wait()` | If counter == 0, return immediately. Otherwise block in the waiting room until it reaches 0. |
| `wg.Go(f)` (Go 1.25+) | Add one task, run `f` in a new goroutine, and mark the task done when `f` returns. `f` must not panic. |

**Syntax | Input | Output | Why**

| Syntax | Input | Output | Why you use it |
| --- | --- | --- | --- |
| `var wg sync.WaitGroup` | – | a WaitGroup with counter 0 | Declare it; no constructor is needed |
| `wg.Add(delta int)` | delta = how many tasks to add (may be negative) | nothing | Announce tasks *before* they start |
| `wg.Done()` | – | nothing | A task reports that it has finished |
| `wg.Wait()` | – | nothing; **blocks** until the counter is 0 | The owner waits for all tasks |
| `wg.Go(f func())` | f = the task to run in a new goroutine | nothing | Add + `go` + `defer Done` in one call, so you can't forget one of them |

**W2.1 Declare it, and the imports.** Create `w2.go`. Imports come first. Go **rejects unused imports**, so only import what the file uses:

```go
package main

import (
	"fmt"  // printing
	"sync" // WaitGroup
)

func main() {
	var wg sync.WaitGroup
```

**W2.2 Add before starting the goroutine.**

```go
	for i := range 3 {
		wg.Add(1)
```

`Add(1)` runs in `main`, **before** `go`. Why this matters is explained in the correctness rule below.

**W2.3 Done when the task ends.**

```go
		go func() {
			defer wg.Done()
			fmt.Println("worker", i, "finished")
		}()
	}
```

`defer wg.Done()` runs even if the function returns early. The closure uses `i`. Since Go 1.22 each iteration has **its own** `i` **[Spec]**, so each goroutine sees a different value. (Under the pre-1.22 rules, all closures in a `for i := 0; i < 3; i++` loop shared **one** `i` and usually printed `3`.)

**W2.4 Wait.**

```go
	wg.Wait()
	fmt.Println("all done")
}
```

**W2.5 Put it together and run.**

```bash
go run w2.go
```

**What to observe:** three `worker N finished` lines in **any order** (nondeterministic), then `all done` **always last** (deterministic, because Wait guarantees it). Run it five times and watch the order change.

**W2.6 The same program with `wg.Go`.** Replace the loop body with:

```go
	for i := range 3 {
		wg.Go(func() {
			fmt.Println("worker", i, "finished")
		})
	}
```

Same behaviour, and there is no `Add`/`Done` to get wrong. The project uses explicit `Add`/`Done` once (Phase 4, to see the mechanics) and `wg.Go` from then on.

**Trace (3 tasks).**

| Step | Event | Counter | Waiting room |
| --- | --- | --- | --- |
| 1 | `Add(1)` ×3 (in main) | 3 | – |
| 2 | main calls `Wait` | 3 | main |
| 3 | worker 1 `Done` | 2 | main |
| 4 | worker 0 `Done` | 1 | main |
| 5 | worker 2 `Done` | 0 | released → main continues |

**Correctness rules.**

1. **When the counter is zero, any `Add` with a positive delta must happen before `Wait`** **[API]**. If the goroutine itself called `Add(1)`, `main` could reach `Wait` while the counter is still 0, return immediately, and miss the task entirely. Calling `Add` in the parent, before `go`, guarantees the ordering. That is why the convention exists (and why `wg.Go` does it for you). It is not literally true that "Add must always happen before Wait": once the counter is positive, a running task may `Add` more work.
2. **Every `Add(n)` is matched by exactly n `Done`s.** Too few means `Wait` blocks forever. Too many panics.
3. **Never copy a WaitGroup after first use** **[API]**. Pass `*sync.WaitGroup`. `go vet` reports copies.

**Failure modes** (observed on go1.27.1; exact wording is representative).

| Mistake | Result |
| --- | --- |
| `Done` without `Add` | `panic: sync: negative WaitGroup counter` |
| `Add(2)`, only one `Done`, nothing else running | `fatal error: all goroutines are asleep - deadlock!` |
| Same, but another goroutine is alive (e.g. a signal handler) | **Hangs forever with no message**. The deadlock detector only fires when *every* goroutine is blocked **[Runtime]**. |
| `Add(1)` inside the goroutine | Wait may return early → missing results. Since Go 1.25, `go vet` warns about this pattern. |
| WaitGroup passed by value | Done hits a copy → deadlock. `go vet` reports it. |

**Checkpoint W:** `w2.go` always prints `all done` last, and you can explain why `w1.go` usually doesn't print its hellos.

---

## Phase 1: Log generator (Milestone 1)

**Why now:** you can't analyze logs you don't have. The generator is also **deterministic** (same seed = byte-identical files), which makes a fixed baseline possible.

### 1.1 Concept first: packages used by the generator (Familiar, briefly)

You already know `os` and `fmt` in general. These are only the calls used here.

| Syntax | Input | Output | Why you use it |
| --- | --- | --- | --- |
| `os.MkdirAll(path string, perm os.FileMode)` | directory path; permission bits such as `0o755` | `error` (nil if it already exists) | Create `logs/` |
| `os.Create(name string)` | file path | `*os.File`, `error` | Create or truncate a file for writing |
| `f.Close()` | – | `error` | Release the file descriptor. **For a written file, this error matters**: data may only reach the disk at Close. |
| `filepath.Join(elem ...string)` | path parts | `string` joined with the OS separator | Build `logs/access-0007.log` portably |
| `fmt.Sprintf(format, args...)` | format such as `"access-%04d.log"`; values | `string` | `%04d` pads with zeros to 4 digits: `7 → 0007` |
| `fmt.Fprintf(w io.Writer, format, args...)` | destination writer; format; values | bytes written `int`, `error` | Write a formatted line to a file or stderr |
| `fmt.Fprintln(w, args...)` | destination; values | `int`, `error` | Write values plus a newline |
| `fmt.Errorf(format, args...)` with `%w` | format; values; one wrapped error | `error` | Add context while keeping the cause inspectable with `errors.Is` |

**`bufio.Writer` (New, small).** *Problem:* each `Fprintf` straight to a file is a system call, and 1M syscalls are slow. *Model:* a bucket in front of the file. Writes fill the bucket (4096 bytes by default), and only a full bucket is poured into the file. **`Flush` pours whatever is left.** If you forget `Flush`, the last part of the file is silently missing.

| Syntax | Input | Output | Why you use it |
| --- | --- | --- | --- |
| `bufio.NewWriter(w io.Writer)` | the file | `*bufio.Writer` | Batch many small writes into a few syscalls |
| `w.Flush()` | – | `error` | Write out the buffered remainder. Required before Close. |

**`math/rand/v2` (New, small).** *Problem:* we want realistic-looking but **repeatable** data. *Model:* a seeded generator is a fixed sequence of numbers. The same seed gives the same sequence. `rand.NewPCG(seed1, seed2)` is a source with two 64-bit seeds. `rand.New` wraps it with helper methods.

| Syntax | Input | Output | Why you use it |
| --- | --- | --- | --- |
| `rand.NewPCG(seed1, seed2 uint64)` | two seeds | `*rand.PCG` (a `rand.Source`) | Deterministic source. We use (seed, fileIndex) so each file differs but is repeatable. |
| `rand.New(src rand.Source)` | source | `*rand.Rand` | Gives the methods below |
| `r.IntN(n int)` | upper bound n (must be > 0, or it panics) | `int` in `[0, n)` | Pick an endpoint, status or latency |

A `*rand.Rand` is **not safe for concurrent use** **[API]**. Each file gets its own, so this doesn't matter here. If you upgrade Go, regenerate both the logs and the baseline together.

**`time` (Familiar, the calls used here).**

| Syntax | Input | Output | Why you use it |
| --- | --- | --- | --- |
| `time.Date(y, mon, d, h, m, s, ns, loc)` | calendar fields; `time.UTC` | `time.Time` | A fixed start timestamp |
| `t.Add(d time.Duration)` | duration | `time.Time` | Line n gets start + n seconds |
| `time.Duration(n) * time.Second` | an int converted to Duration | `time.Duration` | Durations are typed `int64` nanoseconds |
| `t.Format(time.RFC3339)` | layout constant | `string` such as `2026-11-03T10:00:01Z` | Matches the README's format |

**Builtin `min(a, b)`** (Go 1.21 **[Spec]**). `min(r.IntN(n), r.IntN(n))` takes the smaller of two random picks. That skews choices toward the start of the list, so some endpoints are clearly busier than others and the top 10 is meaningful.

### 1.2 Write `internal/loggen/loggen.go`, part 1: package, imports, data

**What this code does:** it defines the vocabulary of fake traffic (15 endpoints, weighted status codes). Later steps use it to write `files` log files of `lines` lines each. It has no concurrency. The analyzer's tests and benchmarks call it too, which is why it lives in `internal/` and not in `main`.

```go
// Package loggen writes deterministic synthetic access-log files for testing
// and benchmarking the analyzer.
package loggen

import (
	"bufio"        // buffered writes
	"fmt"          // Sprintf/Fprintf/Errorf
	"math/rand/v2" // seeded random numbers
	"os"           // MkdirAll, Create
	"path/filepath"
	"time" // timestamps
)

var endpoints = []string{
	"GET /api/users", "GET /api/orders", "POST /api/orders", "GET /api/products",
	"GET /api/cart", "POST /api/cart", "GET /health", "POST /api/login",
	"GET /api/search", "DELETE /api/cart", "PUT /api/users", "GET /api/reviews",
	"POST /api/payments", "GET /api/inventory", "GET /static/app.js",
}

// 200 appears 6 times and 404 twice, so they are more likely to be picked.
var statuses = []int{200, 200, 200, 200, 200, 200, 201, 204, 301, 400, 401, 404, 404, 500, 503}
```

These package-level vars are **read-only after init**, so they are "logically constant", which engineering guide §3.5 allows.

### 1.3 `Generate`: one file per index

```go
// Generate writes files log files of lines lines each into dir. The same
// seed always produces byte-identical files.
func Generate(dir string, files, lines int, seed uint64) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for i := range files {
		path := filepath.Join(dir, fmt.Sprintf("access-%04d.log", i))
		r := rand.New(rand.NewPCG(seed, uint64(i)))
		if err := writeFile(path, lines, r); err != nil {
			return fmt.Errorf("generate %s: %w", path, err)
		}
	}
	return nil
}
```

- Each file has its own generator seeded with `(seed, i)`, so file 7 always has the same content, whatever order files are written in.
- **Error rule (engineering guide §3.1):** `MkdirAll`'s error already contains the path, so it is returned as is. The wrap in the loop adds *new* information (which file), and `%w` sits at the end.

### 1.4 `writeFile`: buffered writing and correct Close handling

```go
func writeFile(path string, lines int, r *rand.Rand) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	start := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	for n := range lines {
		if r.IntN(5000) == 0 {
			fmt.Fprintln(w, "corrupted line")
			continue
		}
		ts := start.Add(time.Duration(n) * time.Second).Format(time.RFC3339)
		ep := endpoints[min(r.IntN(len(endpoints)), r.IntN(len(endpoints)))]
		status := statuses[r.IntN(len(statuses))]
		latency := 1 + r.IntN(500)
		fmt.Fprintf(w, "%s %s %d %dms\n", ts, ep, status, latency)
	}
	if err := w.Flush(); err != nil {
		f.Close() // the write error is the one worth reporting
		return err
	}
	return f.Close()
}
```

| Line | Why |
| --- | --- |
| `r.IntN(5000) == 0` | About 1 line in 5,000 is corrupted, to exercise the malformed-line path |
| `Fprintf(w, ...)` errors ignored | A `bufio.Writer` remembers the first write error and returns it from `Flush`, so checking `Flush` is enough **[API]** |
| `return f.Close()` | Not `defer f.Close()`: for a *written* file the Close error can mean lost data, so it must be returned |
| `f.Close() // ...` | Discarding an error needs a comment saying why (engineering guide §3.1) |

### 1.5 Write `cmd/loggen/main.go`

**What this code does:** it reads four command-line options, calls `loggen.Generate`, and reports success or failure. It is "thin": no logic, just wiring (engineering guide §1.2 rule 4).

**`flag` (Familiar, the calls used here).** *Model:* each declaration returns a pointer to a box holding the default. `flag.Parse()` fills the boxes from what the user typed.

| Syntax | Input | Output | Why you use it |
| --- | --- | --- | --- |
| `flag.String(name, default, usage string)` | flag name; default; help text | `*string` | `-dir` |
| `flag.Int(name string, default int, usage string)` | ″ | `*int` | `-files`, `-lines` |
| `flag.Uint64(name string, default uint64, usage string)` | ″ | `*uint64` | `-seed` |
| `flag.Parse()` | – (reads `os.Args[1:]`) | nothing; exits with a usage message on bad input | Fill the boxes |
| `os.Exit(code int)` | exit status | does not return; **deferred calls do not run** | Non-zero status = failure |

```go
// Command loggen generates synthetic access-log files.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sidd-dhegaskar/log-analyzer/internal/loggen"
)

func main() {
	dir := flag.String("dir", "logs", "output directory")
	files := flag.Int("files", 1000, "number of files")
	lines := flag.Int("lines", 1000, "lines per file")
	seed := flag.Uint64("seed", 1, "random seed (same seed = same files)")
	flag.Parse()

	if err := loggen.Generate(*dir, *files, *lines, *seed); err != nil {
		fmt.Fprintln(os.Stderr, "loggen:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "wrote %d files x %d lines to %s\n", *files, *lines, *dir)
}
```

| You type | `*dir` | `*files` | `*lines` | `*seed` |
| --- | --- | --- | --- | --- |
| `go run ./cmd/loggen` | `logs` | 1000 | 1000 | 1 |
| `go run ./cmd/loggen -files 10 -lines 5` | `logs` | 10 | 5 | 1 |
| `go run ./cmd/loggen -dir /tmp/l -seed 7` | `/tmp/l` | 1000 | 1000 | 7 |

Imports are grouped stdlib first, then a blank line, then project packages (engineering guide §2.3).

### 1.6 Run it

```bash
go run ./cmd/loggen
ls logs | wc -l                  # 1000
head -3 logs/access-0000.log
du -sh logs                      # ≈ 47M
```

Expected first lines (seed 1):

```
2026-11-03T10:00:00Z GET /api/orders 200 351ms
2026-11-03T10:00:01Z GET /api/search 200 66ms
2026-11-03T10:00:02Z GET /api/orders 200 442ms
```

**What could go wrong:** forget `w.Flush()` and the files end mid-line (check with `tail -c 100 logs/access-0000.log`).

**Checkpoint 1:** run the generator twice and compare: `shasum logs/access-0042.log` prints the same hash both times. `grep -h corrupted logs/*.log | wc -l` prints **217**.

---

## Phase 2: Sequential analyzer, JSON report and baseline (Milestone 2, plus JSON from Milestone 10)

**Why now:** before adding concurrency you need a version that is **obviously correct**, and its exact output saved. Every later version must match it byte for byte.

### 2.1 Concept first: text-parsing APIs (Familiar packages, new calls)

| Syntax | Input | Output | Why you use it |
| --- | --- | --- | --- |
| `strings.Fields(s string)` | a line | `[]string` split on **runs** of whitespace; no empty items | Split the 5 columns even with doubled spaces |
| `strconv.Atoi(s string)` | decimal text | `int`, `error` (`*strconv.NumError`) | `"200"` → 200 |
| `strings.CutSuffix(s, suffix string)` (Go 1.20) | text; suffix | `before string`, `found bool` | `"123ms"` → `"123", true`; `"123"` → `"123", false` |
| `strings.HasSuffix(s, suffix string)` | text; suffix | `bool` | Filter `*.log` names |
| `strings.Compare(a, b string)` | two strings | `int`: −1, 0 or +1 | Tie-break in the sort |
| `errors.New(text string)` | message | `error` | Define the sentinel `ErrMalformed` |
| `errors.Is(err, target error)` | error; sentinel | `bool`: is `target` anywhere in the `%w` chain? | Tests check *which* error happened without matching strings |

| Line | `strings.Fields` result | `ParseLine` result |
| --- | --- | --- |
| `2026-11-03T10:00:01Z GET /api/users 200 123ms` | 5 fields | `Entry{"GET /api/users", 200, 123}` |
| `2026-11-03T10:00:01Z  POST /api/cart 201   7ms` | 5 fields (runs of spaces collapse) | `Entry{"POST /api/cart", 201, 7}` |
| `corrupted line` | 2 fields | `ErrMalformed: want 5 fields, got 2` |
| `… GET /x 200 5` | 5 fields | `ErrMalformed: latency "5" has no ms suffix` |

### 2.2 `internal/accesslog/parse.go`

**What this code does:** it turns one text line into an `Entry` (endpoint, status, latency), or returns an error wrapping `ErrMalformed`. It is a pure function: no I/O and no shared state, so it is trivially safe to call from many goroutines at once.

```go
// Package accesslog parses web-server access logs and accumulates request
// statistics. Stats values are not safe for concurrent use.
package accesslog

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrMalformed reports a line that does not match the expected format.
var ErrMalformed = errors.New("malformed line")

// Entry is one parsed access-log line.
type Entry struct {
	Endpoint  string // "METHOD PATH", e.g. "GET /api/users"
	Status    int
	LatencyMS int
}

// ParseLine parses "TIMESTAMP METHOD PATH STATUS LATENCYms".
func ParseLine(line string) (Entry, error) {
	fields := strings.Fields(line)
	if len(fields) != 5 {
		return Entry{}, fmt.Errorf("%w: want 5 fields, got %d", ErrMalformed, len(fields))
	}
	status, err := strconv.Atoi(fields[3])
	if err != nil {
		return Entry{}, fmt.Errorf("%w: status %q", ErrMalformed, fields[3])
	}
	ms, ok := strings.CutSuffix(fields[4], "ms")
	if !ok {
		return Entry{}, fmt.Errorf("%w: latency %q has no ms suffix", ErrMalformed, fields[4])
	}
	latency, err := strconv.Atoi(ms)
	if err != nil {
		return Entry{}, fmt.Errorf("%w: latency %q", ErrMalformed, fields[4])
	}
	return Entry{Endpoint: fields[1] + " " + fields[2], Status: status, LatencyMS: latency}, nil
}
```

Rules applied: the package doc states that `Stats` is **not concurrency-safe** (engineering guide §3.4: document concurrency safety). **`%w` goes at the start for a sentinel category** (§3.1). Errors are handled first with early returns, and there is no `else`.

### 2.3 Test the parser first: `internal/accesslog/parse_test.go`

**`testing` (New, small).** `go test` finds `*_test.go` files and runs every `func TestXxx(t *testing.T)`.

| Syntax | Input | Output | Why you use it |
| --- | --- | --- | --- |
| `t.Run(name string, f func(t *testing.T))` | subtest name; body | `bool` (passed) | Named subtests: `-run TestParseLine/bad_status` |
| `t.Errorf(format, args...)` | message | nothing; marks the test failed and **continues** | Report every failure |
| `t.Fatalf(format, args...)` | message | does not return; stops this (sub)test | Continuing makes no sense |

```go
package accesslog

import (
	"errors"
	"testing"
)

func TestParseLine(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		want    Entry
		wantErr error
	}{
		{name: "valid", line: "2026-11-03T10:00:01Z GET /api/users 200 123ms", want: Entry{Endpoint: "GET /api/users", Status: 200, LatencyMS: 123}},
		{name: "extra spaces", line: "2026-11-03T10:00:01Z  POST /api/cart 201   7ms", want: Entry{Endpoint: "POST /api/cart", Status: 201, LatencyMS: 7}},
		{name: "too few fields", line: "corrupted line", wantErr: ErrMalformed},
		{name: "bad status", line: "2026-11-03T10:00:01Z GET /x OK 5ms", wantErr: ErrMalformed},
		{name: "missing ms", line: "2026-11-03T10:00:01Z GET /x 200 5", wantErr: ErrMalformed},
		{name: "empty", line: "", wantErr: ErrMalformed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseLine(tt.line)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ParseLine(%q) error = %v, want %v", tt.line, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseLine(%q) = %+v, want %+v", tt.line, got, tt.want)
			}
		})
	}
}
```

`errors.Is(nil, nil)` is `true`, so the valid rows pass the error check. `got != tt.want` works because `Entry` contains only comparable fields. Messages use the `Func(in) = got, want want` form (engineering guide §5).

```bash
go test ./internal/accesslog/ -v -run TestParseLine
```

Expected: 6 subtests `PASS`.

### 2.4 Concept: `slices.SortFunc` (Familiar idea, new API)

| Syntax | Input | Output | Why you use it |
| --- | --- | --- | --- |
| `slices.SortFunc(s []E, cmp func(a, b E) int)` (Go 1.21) | slice; comparator returning <0 if a sorts first, 0 if equal, >0 if b first | nothing; sorts **in place**; **not stable** | Order endpoints by count, then by name |

Because the comparator never returns 0 for two different endpoints (names are unique), stability does not matter and the order is fully determined (assumption A5).

### 2.5 `internal/accesslog/stats.go`

**What this code does:** `Stats` holds the running counters for any set of lines: one file, or all files. `Add` records one entry, `Merge` adds one Stats into another, and `Report` turns the counters into the final JSON-ready struct. Every strategy in this project reuses these exact functions. Only *how they are called* changes.

```go
package accesslog

import (
	"slices"
	"strings"
)

// Stats accumulates counters for a set of log lines.
type Stats struct {
	Requests     int
	Malformed    int
	LatencySumMS int64
	Status       map[int]int
	Endpoints    map[string]int
}

// NewStats returns empty Stats with initialised maps.
func NewStats() *Stats {
	return &Stats{Status: make(map[int]int), Endpoints: make(map[string]int)}
}

// Add records one parsed entry.
func (s *Stats) Add(e Entry) {
	s.Requests++
	s.LatencySumMS += int64(e.LatencyMS)
	s.Status[e.Status]++
	s.Endpoints[e.Endpoint]++
}

// Merge adds every counter of o into s. o is not modified.
func (s *Stats) Merge(o *Stats) {
	s.Requests += o.Requests
	s.Malformed += o.Malformed
	s.LatencySumMS += o.LatencySumMS
	for code, n := range o.Status {
		s.Status[code] += n
	}
	for ep, n := range o.Endpoints {
		s.Endpoints[ep] += n
	}
}
```

- `NewStats` is required because writing to a nil map panics. A `Stats{}` literal would crash on the first `Add`.
- **`Merge` is the key operation of the project.** It is associative and commutative: merging files in any order gives the same total. **That property is what allows concurrency at all.** It is why every concurrent version can match the sequential one even though files finish in random order.
- Pointer receivers: these methods mutate, and maps are inside (engineering guide §3.2).

### 2.6 Concept: `encoding/json` (New · Essential, backend theme)

**Problem:** programs exchange data as text, and JSON is the lingua franca of backends. **Model:** `json.Marshal` walks a Go value with reflection and writes the JSON for it. **Struct tags** (`json:"name"`) choose the key names.

Rules that make our output deterministic (all **[API]**, from the `encoding/json` docs):

| Go value | JSON | Guarantee |
| --- | --- | --- |
| struct | object, **fields in declaration order** | yes |
| `map[int]int` | object with keys converted to strings, **keys sorted** | yes |
| `[]T(nil)` | `null` | yes, so we use an empty non-nil slice to get `[]` |
| `float64` | shortest representation that round-trips | yes |

| Syntax | Input | Output | Why you use it |
| --- | --- | --- | --- |
| `json.NewEncoder(w io.Writer)` | destination (stdout) | `*json.Encoder` | Stream JSON to a writer |
| `enc.SetIndent(prefix, indent string)` | `""`, `"  "` | nothing | Human-readable output |
| `enc.Encode(v any)` | the value | `error`; writes JSON **plus a newline** | Print the report |

Go 1.25 shipped an experimental `encoding/json/v2` behind `GOEXPERIMENT=jsonv2`. I am not certain of its status in 1.27.1, so this guide uses the stable `encoding/json` that the README names.

### 2.7 The report types and `Report` (append to `stats.go`)

```go
// EndpointCount is one row of the top-endpoints table.
type EndpointCount struct {
	Endpoint string `json:"endpoint"`
	Count    int    `json:"count"`
}

// Report is the JSON document the analyzer prints.
type Report struct {
	TotalRequests  int             `json:"total_requests"`
	MalformedLines int             `json:"malformed_lines"`
	StatusCounts   map[int]int     `json:"status_counts"`
	TopEndpoints   []EndpointCount `json:"top_endpoints"`
	AvgLatencyMS   float64         `json:"avg_latency_ms"`
}

// Report builds the final report with the n busiest endpoints. Ties are
// broken by endpoint name so the output is deterministic.
func (s *Stats) Report(n int) Report {
	top := make([]EndpointCount, 0, len(s.Endpoints)) // non-nil: encodes as [] not null
	for ep, c := range s.Endpoints {
		top = append(top, EndpointCount{Endpoint: ep, Count: c})
	}
	slices.SortFunc(top, func(a, b EndpointCount) int {
		if a.Count != b.Count {
			return b.Count - a.Count // larger count first
		}
		return strings.Compare(a.Endpoint, b.Endpoint)
	})
	if len(top) > n {
		top = top[:n]
	}
	var avg float64
	if s.Requests > 0 {
		avg = float64(s.LatencySumMS) / float64(s.Requests)
	}
	return Report{
		TotalRequests:  s.Requests,
		MalformedLines: s.Malformed,
		StatusCounts:   s.Status,
		TopEndpoints:   top,
		AvgLatencyMS:   avg,
	}
}
```

- **Map iteration order is randomized** **[Spec: unspecified; Runtime: deliberately randomized]**. The `for ep, c := range s.Endpoints` loop collects items in a random order, and the sort then fixes the order.
- `s.Requests > 0` avoids dividing by zero (NaN cannot even be encoded as JSON).
- Naming: the method is `Report`, not `GetReport` (engineering guide §2.2).

### 2.8 `internal/accesslog/file.go`: analyze one file

**`bufio.Scanner` (New, small).** *Problem:* reading a file line by line without loading all of it into memory. *Model:* a cursor. `Scan()` advances to the next line and reports whether there was one. `Text()` returns that line without the `\n`. When `Scan()` returns false, `Err()` tells you whether that was the normal end of the file (nil) or a real failure.

| Syntax | Input | Output | Why you use it |
| --- | --- | --- | --- |
| `os.Open(name string)` | path | `*os.File` (read-only), `error` | Open a log file |
| `bufio.NewScanner(r io.Reader)` | the file | `*bufio.Scanner` | Line iterator |
| `sc.Scan()` | – | `bool`: true if a line is available | Loop condition |
| `sc.Text()` | – | `string`: the current line (newly allocated) | Pass it to `ParseLine` |
| `sc.Err()` | – | `error`: nil at normal EOF | Detect read errors **after** the loop |

**Watch for:** lines longer than 64 KiB (`bufio.MaxScanTokenSize`) stop the scanner with `bufio.ErrTooLong` **[API]**. That is fine for access logs. Use `sc.Buffer` if you ever need more.

**What this code does:** it opens one file, parses every line, counts malformed lines, and returns that file's private `*Stats`. It is the unit of work every strategy runs, and each call builds **its own** Stats, so concurrent calls share nothing.

```go
package accesslog

import (
	"bufio"
	"os"
)

// AnalyzeFile reads one log file and returns its statistics. Malformed lines
// are counted, not treated as errors; only I/O problems return an error.
func AnalyzeFile(path string) (*Stats, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() // read-only file: a Close error cannot lose data

	s := NewStats()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		e, err := ParseLine(sc.Text())
		if err != nil {
			s.Malformed++
			continue
		}
		s.Add(e)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return s, nil
}
```

Here `defer f.Close()` is fine, in contrast with Phase 1, because nothing is lost if closing a read-only file fails. The comment explains the ignored error. Errors from `os.Open` already include the path, so they are not wrapped.

### 2.9 `internal/analyzer/analyzer.go`: find the files

| Syntax | Input | Output | Why you use it |
| --- | --- | --- | --- |
| `os.ReadDir(name string)` | directory | `[]os.DirEntry` **sorted by filename** **[API]**, `error` | List `logs/` |
| `e.Name()` | – | `string`: base name | Filter by suffix |
| `e.Type().IsRegular()` | – | `bool` | Skip directories and symlinks |

**What this code does:** it returns the full paths of all `*.log` files in a directory, in a stable order. It also carries the package doc comment, which states the contract every strategy must meet.

```go
// Package analyzer combines per-file access-log statistics using several
// strategies, from sequential to a cancellable worker pool. Every strategy
// must return exactly the same Stats for the same files.
package analyzer

import (
	"os"
	"path/filepath"
	"strings"
)

// ListFiles returns the paths of all *.log files in dir, sorted by name.
func ListFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".log") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	return files, nil
}
```

### 2.10 `internal/analyzer/sequential.go`: the baseline

**What this code does:** it analyzes files one after another and merges each into a running total. There is one goroutine and nothing is shared. This is the reference implementation.

```go
package analyzer

import (
	"fmt"

	"github.com/sidd-dhegaskar/log-analyzer/internal/accesslog"
)

// Sequential analyzes files one after another. It is the baseline every
// concurrent strategy is checked against.
func Sequential(files []string) (*accesslog.Stats, error) {
	total := accesslog.NewStats()
	for _, path := range files {
		s, err := accesslog.AnalyzeFile(path)
		if err != nil {
			return nil, fmt.Errorf("analyze: %w", err)
		}
		total.Merge(s)
	}
	return total, nil
}
```

**Lifecycle:** there is no concurrency. `total` is created, owned and returned by this one function call.

### 2.11 `cmd/analyze/main.go`, version 1

**What this code does:** it parses flags, lists the files, runs the chosen strategy, prints timing to **stderr** and the JSON report to **stdout**. Keeping them separate means `> baseline.json` captures only the report. Each later phase adds one `case`.

| Syntax | Input | Output | Why you use it |
| --- | --- | --- | --- |
| `time.Now()` | – | `time.Time` | Start the stopwatch |
| `time.Since(t time.Time)` | start time | `time.Duration` (prints as `335.1ms`) | Elapsed time |

```go
// Command analyze prints a JSON report for a directory of access logs.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/sidd-dhegaskar/log-analyzer/internal/accesslog"
	"github.com/sidd-dhegaskar/log-analyzer/internal/analyzer"
)

func main() {
	dir := flag.String("dir", "logs", "directory of *.log files")
	mode := flag.String("mode", "sequential", "sequential (more modes added later)")
	top := flag.Int("top", 10, "number of endpoints in the report")
	flag.Parse()

	if err := run(*dir, *mode, *top); err != nil {
		fmt.Fprintln(os.Stderr, "analyze:", err)
		os.Exit(1)
	}
}

func run(dir, mode string, top int) error {
	files, err := analyzer.ListFiles(dir)
	if err != nil {
		return err
	}
	start := time.Now()

	var stats *accesslog.Stats
	switch mode {
	case "sequential":
		stats, err = analyzer.Sequential(files)
	default:
		return fmt.Errorf("unknown -mode %q", mode)
	}
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "mode=%s files=%d elapsed=%v\n", mode, len(files), time.Since(start))

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(stats.Report(top))
}
```

`main` → `run` → error is a common pattern **[Convention]**. `run` returns errors normally, and only `main` decides the exit code. This keeps `os.Exit` (which skips defers) in one place.

### 2.12 Run and save the baseline

```bash
go run ./cmd/analyze -mode=sequential > baseline.json
cat baseline.json
```

stderr shows something like `mode=sequential files=1000 elapsed=335ms`. `baseline.json` must be exactly:

```json
{
  "total_requests": 999783,
  "malformed_lines": 217,
  "status_counts": {
    "200": 399198,
    "201": 67048,
    "204": 66944,
    "301": 66830,
    "400": 66742,
    "401": 66660,
    "404": 133669,
    "500": 66180,
    "503": 66512
  },
  "top_endpoints": [
    { "endpoint": "GET /api/users", "count": 129802 },
    { "endpoint": "GET /api/orders", "count": 119545 },
    { "endpoint": "POST /api/orders", "count": 110952 },
    { "endpoint": "GET /api/products", "count": 101973 },
    { "endpoint": "GET /api/cart", "count": 93381 },
    { "endpoint": "POST /api/cart", "count": 84493 },
    { "endpoint": "GET /health", "count": 75689 },
    { "endpoint": "POST /api/login", "count": 66639 },
    { "endpoint": "GET /api/search", "count": 57846 },
    { "endpoint": "DELETE /api/cart", "count": 48498 }
  ],
  "avg_latency_ms": 250.52635721951663
}
```

(Your file shows each top-endpoint object over four lines. It is compressed here only to save space.)

**Invariants:** `total_requests + malformed_lines = 1,000,000`, and the status counts add up to `total_requests`.

### The baseline rule (used in every later phase)

```bash
go run ./cmd/analyze -mode=<mode> | cmp - baseline.json && echo SAME
```

`cmp` compares bytes. Byte equality is a valid test here because the output is **canonical**: struct fields come in a fixed order, map keys are sorted by `encoding/json`, ties are broken by name, and the average is computed from an integer sum, so the floating-point result does not depend on merge order. What is **nondeterministic** in the concurrent versions is only *which file finishes first*. That must never show up in the report.

**Checkpoint 2:** `go test ./...` passes, and `baseline.json` matches the numbers above.

---

## Phase 3: A goroutine for every file (Milestone 3)

**Why now:** the files are **independent**. Analyzing file 7 never needs file 8, so they can run at the same time. That makes them the "independent operations" of learning-cycle step 2.

### 3.1 Concept recap: goroutines (see W1)

Reminder: `go f()` starts `f` concurrently and returns immediately, and nothing waits for it. **What is new here:** the goroutine is a **closure** that uses the loop variable `path`. Since Go 1.22 (enabled by `go 1.27.1` in `go.mod`), every iteration has a fresh `path` **[Spec]**, so each goroutine analyzes its own file.

**`runtime.NumGoroutine` (New, small, README tool).**

| Syntax | Input | Output | Why you use it |
| --- | --- | --- | --- |
| `runtime.NumGoroutine()` | – | `int`: goroutines that exist right now | Spot goroutines that outlive the work (leaks) |

### 3.2 Create `internal/analyzer/racy.go`, first version

**What this code does:** it starts one goroutine per file. Each goroutine analyzes its file and prints a line to stderr. For now the results are **not** combined, and the function returns **without waiting**. This is deliberately incomplete, so you can see what happens.

```go
package analyzer

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sidd-dhegaskar/log-analyzer/internal/accesslog"
)

// Racy starts one goroutine per file. (Work in progress: see Phases 4–5.)
func Racy(files []string) (*accesslog.Stats, error) {
	total := accesslog.NewStats()
	for _, path := range files {
		go func() {
			s, err := accesslog.AnalyzeFile(path)
			if err != nil {
				return // errors are ignored for now; fixed in Phase 5
			}
			fmt.Fprintf(os.Stderr, "%s: %d requests\n", filepath.Base(path), s.Requests)
		}()
	}
	return total, nil
}
```

`filepath.Base(path)` returns the last element of a path (`access-0007.log`).

### 3.3 Wire it into `main.go`

Add `"runtime"` to the imports, add the case, and print the goroutine count:

```go
	case "racy":
		stats, err = analyzer.Racy(files)
```

```go
	fmt.Fprintf(os.Stderr, "mode=%s files=%d elapsed=%v goroutines=%d\n",
		mode, len(files), time.Since(start), runtime.NumGoroutine())
```

### 3.4 Run it and predict first

**Predict:** how many `requests` lines will print? What will `total_requests` be?

```bash
go run ./cmd/analyze -mode=racy
```

Observed (yours will differ in the details):

```
access-0003.log: 1000 requests
access-0036.log: 1000 requests
...                          ← only ~40 of 1000 lines
mode=racy files=1000 elapsed=1.56975ms goroutines=997
{ "total_requests": 0, ... }
```

**What to observe and why:**

| Observation | Cause |
| --- | --- |
| `elapsed≈1.5ms` | `Racy` only *started* 1000 goroutines and returned at once |
| `goroutines=997` | Almost all of them are still running when main prints |
| Only some lines print, in random order | Main encodes the JSON and **exits**, killing the rest (W1) |
| `total_requests: 0` | Nothing merges results yet |

This is also the README experiment **"remove `wg.Wait()`"**, seen before `wg.Wait()` even exists.

**Checkpoint 3:** you can explain why the line count changes from run to run.

---

## Phase 4: Wait for completion with `sync.WaitGroup` (Milestone 4)

### 4.1 Concept recap: WaitGroup (see W2)

Reminder: it is a counter of unfinished tasks plus a waiting room. **What is new here:** the counter goes up to 1000, and `Racy` becomes the **owner** of all the goroutines it starts. It does not return until they have all finished, which is the guide's "goroutine lifetimes must be obvious" rule (engineering guide Part 4).

### 4.2 Add the WaitGroup (explicit Add/Done this one time)

Change `Racy` to:

```go
func Racy(files []string) (*accesslog.Stats, error) {
	total := accesslog.NewStats()
	var wg sync.WaitGroup
	for _, path := range files {
		wg.Add(1) // before `go`: the counter is positive before Wait can run
		go func() {
			defer wg.Done()
			s, err := accesslog.AnalyzeFile(path)
			if err != nil {
				return
			}
			fmt.Fprintf(os.Stderr, "%s: %d requests\n", filepath.Base(path), s.Requests)
		}()
	}
	wg.Wait()
	return total, nil
}
```

Add `"sync"` to the imports.

### 4.3 Run it

```bash
go run ./cmd/analyze -mode=racy 2>&1 >/dev/null | grep -c requests   # 1000
go run ./cmd/analyze -mode=racy 2>&1 >/dev/null | head -3           # random order
```

**What to observe:** always 1,000 lines (deterministic), in a different order each run (nondeterministic). The report still shows `goroutines=1`, because they have all exited. `total_requests` is still 0.

**Lifecycle so far:**

| Question | Answer |
| --- | --- |
| Who creates the goroutines? | `Racy` |
| Who waits for them? | `Racy`, via `wg.Wait()` |
| How is completion detected? | Counter reaches 0 |
| Cancellation? | None yet (Phase 10) |
| Results? | Not collected yet (next phase) |

**Checkpoint 4:** `grep -c requests` prints 1000 every time.

---

## Phase 5: Shared maps, and the data race (Milestone 5)

**Why now:** we need the per-file results in one total. The most obvious idea is to let every goroutine update one shared `total`. This phase shows why that is wrong.

### 5.1 Write it the obvious way

Replace `racy.go` with the final version below. The stderr printing goes away, so remove the `fmt`, `os` and `path/filepath` imports, or Go will refuse to compile. Errors are now collected too.

```go
package analyzer

import (
	"errors"
	"sync"

	"github.com/sidd-dhegaskar/log-analyzer/internal/accesslog"
)

// Racy starts one goroutine per file and merges into shared Stats WITHOUT
// synchronization. It is deliberately broken: it exists for the race
// detector experiment. Never use it for real work.
func Racy(files []string) (*accesslog.Stats, error) {
	total := accesslog.NewStats()
	errs := make([]error, len(files))
	var wg sync.WaitGroup
	for i, path := range files {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := accesslog.AnalyzeFile(path)
			if err != nil {
				errs[i] = err
				return
			}
			total.Merge(s) // DATA RACE: many goroutines write the same maps
		}()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return total, nil
}
```

**`errors.Join` (small).**

| Syntax | Input | Output | Why you use it |
| --- | --- | --- | --- |
| `errors.Join(errs ...error)` (Go 1.20) | any number of errors (nils are skipped) | `error`: nil if all inputs are nil; otherwise one error wrapping all of them | Report every failed file at once |

**Why `errs[i] = err` is *not* a race, while `total.Merge` is:** each goroutine writes a **different slice element**, and distinct elements are distinct memory locations **[Spec / memory model]**. The main goroutine reads `errs` only after `wg.Wait()`, and `Wait` returning *happens after* every `Done` **[API]**. Writing different variables needs no lock. Writing the **same** variable does. Giving each goroutine its own slot is a small example of **confinement** (Phase 6).

### 5.2 Run it and predict first

**Predict:** will the report match the baseline?

```bash
go run ./cmd/analyze -mode=racy
```

Observed on go1.27.1:

```
fatal error: concurrent map writes

goroutine 38 [running]:
internal/runtime/maps.fatal(...)
github.com/sidd-dhegaskar/log-analyzer/internal/accesslog.(*Stats).Merge(...)
	.../internal/accesslog/stats.go:39
...
exit status 2
```

On other runs it may finish with **wrong numbers**, or occasionally with correct ones. That last case is the most dangerous.

### 5.3 Concept first: data races (New · Essential; deeper material in Track B1)

**The problem.** `s.Status[code] += n` looks like one step, but it is three: *read* the count, *add*, *write* the count back. A map write is worse still: it can move entries between internal buckets.

**The core idea [Spec, Go memory model].** A **data race** is two accesses to the same memory location, by different goroutines, where at least one is a write, and **nothing orders them** (no *happens-before* relation). A program with a data race has **no defined meaning** for those accesses. It is not merely "sometimes off by one": for maps and other multi-word values the result can be corrupted memory.

**Trace of a lost update [Model]:**

| Time | Goroutine A | Goroutine B | `Requests` in memory |
| --- | --- | --- | --- |
| 1 | read `Requests` → 5000 | | 5000 |
| 2 | | read `Requests` → 5000 | 5000 |
| 3 | write 5000+1000 | | 6000 |
| 4 | | write 5000+1000 | **6000** (should be 7000) |

**Analogy.** Two cashiers updating the same paper ledger from memory. Each reads the total, does the arithmetic in their head, and writes their own result, overwriting the other's.

**Happens-before, minimally [Spec].** Inside one goroutine, statements happen in program order. *Across* goroutines there is **no** order unless you create one with a synchronization operation: `go` statement start, channel send/receive/close, `Mutex.Unlock`→`Lock`, `WaitGroup.Done`→`Wait` return, atomics. If you can't point to such an edge between a write and a read in different goroutines, you have a race.

**Two detectors:**

| Detector | How it works | Guarantee |
| --- | --- | --- |
| `fatal error: concurrent map writes` | The map implementation sets a "writing" flag and checks it | **Best-effort [Runtime]**: it often misses races. It is a `fatal error`, not a panic, so `recover` cannot catch it. |
| Race detector (`-race`) | Instruments every memory access at compile time and tracks happens-before at run time (ThreadSanitizer) | Reports races that **actually occur during that run**, with no false positives. It can't find races in code paths that didn't run. It costs roughly 2–20× CPU and 5–10× memory **[API: tool docs]**. |

### 5.4 Run the race detector (README tool)

```bash
go run -race ./cmd/analyze -mode=racy > /dev/null
```

Observed (trimmed):

```
==================
WARNING: DATA RACE
Read at 0x00c0000de060 by goroutine 14:
  .../accesslog.(*Stats).Merge()      stats.go:32
  .../analyzer.Racy.func1()           racy.go:26

Previous write at 0x00c0000de060 by goroutine 39:
  .../accesslog.(*Stats).Merge()      stats.go:32
  .../analyzer.Racy.func1()           racy.go:26

Goroutine 14 (running) created at:
  .../analyzer.Racy()                 racy.go:19
==================
```

How to read it: **what** (a read and a write of the same address), **where** (both inside `Merge`, called from the closure in `Racy`), **who** (goroutines 14 and 39, both created at `racy.go:19`). The process ends with `Found N data race(s)` and exit status **66**.

**Checkpoint 5:** you saw both `concurrent map writes` and a `-race` report, and you can explain the lost-update trace. `Racy` stays in the repo, broken on purpose, for Phase 9.

---

## Phase 6: Protect the shared total with `sync.Mutex` (Milestone 6)

### 6.1 Concept first: `sync.Mutex` (New · Essential)

**The problem.** In Phase 5 several goroutines run `Merge` on the same `total` at once. We need "only one at a time" for that code.

**The core idea.** A mutex (*mut*ual *ex*clusion lock) lets **only one goroutine at a time hold the lock** that protects some shared state **[API]**. Others that try to take it block until it is free. Which waiter gets it next, and how fairly, depends on the implementation **[Runtime]**. The code between `Lock` and `Unlock` is the **critical section**.

```
            ┌─────────── critical section ───────────┐
G1:  Lock ──┤ total.Merge(s1)                         ├── Unlock
G2:  Lock ·········(blocked)······························ ├ total.Merge(s2) ┤ Unlock
G3:  Lock ··················(blocked)······························· …
```

**Analogy.** A single-occupancy restroom with one key. You take the key, use the room, and return the key. Nobody else can enter meanwhile. The key is useless if someone climbs in through the window, i.e. touches the state **without** taking the lock.

**Operations, defined in terms of the model [API].**

| Operation | Effect |
| --- | --- |
| `var mu sync.Mutex` | Unlocked. The zero value is ready to use. |
| `mu.Lock()` | If unlocked, take it and continue. Otherwise **block** until it is released, then try again. |
| `mu.Unlock()` | Release it, allowing one waiter to proceed. Unlocking an unlocked mutex is a **fatal error**. |
| `mu.TryLock()` | Take it if free and return true, else false. It exists, but its docs warn that correct uses are rare. Not used here. |

**Syntax | Input | Output | Why**

| Syntax | Input | Output | Why you use it |
| --- | --- | --- | --- |
| `var mu sync.Mutex` | – | an unlocked mutex | Declare it next to the state it guards |
| `mu.Lock()` | – | nothing; **blocks** while another goroutine holds it | Enter the critical section |
| `mu.Unlock()` | – | nothing | Leave it |

**Trace (3 goroutines).**

| Step | Event | Holder | Waiting |
| --- | --- | --- | --- |
| 1 | G1 `Lock` | G1 | – |
| 2 | G2 `Lock` (blocks) | G1 | G2 |
| 3 | G3 `Lock` (blocks) | G1 | G2, G3 |
| 4 | G1 `Unlock` | G2 *or* G3 (not guaranteed which) | the other one |
| 5 | holder `Unlock` | the last one | – |

**What it guarantees for memory [Spec, memory model].** For a mutex, the n-th `Unlock` happens before the (n+1)-th `Lock` returns. So everything G1 wrote inside its critical section is **visible** to G2 inside its own. A mutex gives you ordering and visibility, not only exclusion.

**Correctness rules.**

1. **Every access to the protected state, reads included, must hold the lock.** One unlocked read is still a race.
2. **Keep critical sections small.** Do slow work (file I/O) *outside* the lock, and lock only to touch the shared state. Never call unknown code while holding a lock (engineering guide Part 4).
3. **Never copy a mutex after first use** **[API]**. `go vet` reports it.
4. **Go mutexes are not reentrant.** `Lock` twice in the same goroutine deadlocks.

**Failure modes** (observed on go1.27.1).

| Mistake | Result |
| --- | --- |
| `Unlock` without `Lock` | `fatal error: sync: unlock of unlocked mutex` (cannot be recovered) |
| `Lock` twice in one goroutine | `fatal error: all goroutines are asleep - deadlock!` (or a silent hang if other goroutines are alive) |
| Forget `Unlock` on an early return | Every later `Lock` blocks forever. Use `defer mu.Unlock()` when a function has several return paths. |
| Lock held during I/O | Correct, but serialized: no faster than sequential |
| A struct containing a mutex passed by value | Each copy has its own lock, so there is no protection. `go vet`: `copies lock value` |

### 6.2 Concept: confinement (New · Essential)

**Problem:** locks cost time, and every shared variable is a chance to make a mistake. **Idea:** make most data **unshared**. Data that only one goroutine can reach needs **no synchronization at all**.

**Model:** each goroutine owns some data privately, and ownership is only ever *handed over*, never shared at the same time. **In this project:** `AnalyzeFile` builds a brand-new `*Stats` that only its goroutine can see (confined). Only the final `total.Merge(s)` touches shared state. So the lock is taken **once per file** (1,000 times), not once per line (1,000,000 times).

**Analogy:** each cashier counts their own drawer privately, and only walks to the shared safe (lock) once at the end of the shift.

| Data | Owned by | Needs a lock? |
| --- | --- | --- |
| `s` (per-file Stats) | one worker goroutine | no (confined) |
| `errs[i]` | goroutine i, until `Wait` | no (confined by index) |
| `total` | shared by all goroutines | **yes**, `mu` |

### 6.3 Create `internal/analyzer/locked.go`

**What this code does:** like `Racy`, it starts one goroutine per file. Each goroutine analyzes its file privately, then takes the lock just long enough to merge into the shared total. It uses `wg.Go`, so Add/Done can't be misplaced.

```go
package analyzer

import (
	"errors"
	"sync"

	"github.com/sidd-dhegaskar/log-analyzer/internal/accesslog"
)

// Locked starts one goroutine per file. Each goroutine builds private Stats
// and merges them into the shared total while holding mu.
func Locked(files []string) (*accesslog.Stats, error) {
	var (
		mu    sync.Mutex
		total = accesslog.NewStats() // guarded by mu
	)
	errs := make([]error, len(files))
	var wg sync.WaitGroup
	for i, path := range files {
		wg.Go(func() {
			s, err := accesslog.AnalyzeFile(path) // slow part: no lock held
			if err != nil {
				errs[i] = err
				return
			}
			mu.Lock()
			total.Merge(s)
			mu.Unlock()
		})
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return total, nil
}
```

| Line | Why |
| --- | --- |
| `var ( mu …; total … // guarded by mu )` | Declare the mutex **directly next to** the state it protects, and say so in a comment **[Convention]** |
| `AnalyzeFile` before `Lock` | Rule 2: the slow I/O happens with no lock held |
| `mu.Lock(); total.Merge(s); mu.Unlock()` | Smallest possible critical section. No `defer`, because there is one return path. |
| `return total` after `wg.Wait()` without locking | Safe: every `Unlock` happened before its `Done`, and `Wait` returns after every `Done`. That chain of happens-before edges makes all merges visible. |

Add the case in `main.go`:

```go
	case "locked":
		stats, err = analyzer.Locked(files)
```

### 6.4 Run and compare against the baseline

```bash
go run ./cmd/analyze -mode=locked | cmp - baseline.json && echo SAME
go run -race ./cmd/analyze -mode=locked | cmp - baseline.json && echo SAME
```

**What to observe:** `SAME` both times, no race report, and `elapsed` around 120 ms against about 335 ms sequential (on this machine). The deterministic part is the report. The nondeterministic part is the order in which goroutines win the lock, which doesn't matter because `Merge` is commutative.

**Lifecycle and ownership of the mutex:** `Locked` creates it, it protects only `total`, every worker locks and unlocks around one `Merge`, and it is discarded when `Locked` returns.

**Checkpoint 6:** both commands print `SAME`, and `-race` is silent.

---

## Phase 7: Send per-file results through a channel (Milestone 7)

**Why now:** the mutex version works, but correctness depends on *every* goroutine remembering to lock. The alternative is to stop sharing `total`: workers **send** their private results to the one goroutine that owns `total`. This is Go's motto, *"Don't communicate by sharing memory; share memory by communicating."*

### 7.1 Concept first: channels (New · Essential)

**The problem.** Goroutines need to pass values to each other safely, and to know *when* a value is ready, without each piece of shared data needing its own lock.

**The core idea [Spec].** A channel is a **typed conduit between goroutines**. One goroutine sends a value and another receives it. The channel provides both the **transfer** and the **synchronization**: the receive cannot complete before the send. An **unbuffered** channel makes sender and receiver meet. A **buffered** channel has a FIFO queue of fixed capacity in between.

```
unbuffered (cap 0):   sender ──[ handoff ]──▶ receiver     both wait for each other
buffered (cap 3):     sender ──▶ [ v1 | v2 |    ] ──▶ receiver     sender waits only when full,
                                                                   receiver only when empty
```

**Analogy.** Unbuffered: handing a parcel to someone **in person**. You both stand there until the handoff happens. Buffered: a **mailbox with 3 slots**. You drop the parcel and leave, unless the box is full.

**Operations, defined in terms of the model [Spec].**

| Operation | Model |
| --- | --- |
| `ch := make(chan T)` | Create an unbuffered channel. The zero value of a channel variable is **nil**. |
| `ch := make(chan T, n)` | Create a buffered channel with capacity n |
| `ch <- v` (send) | Unbuffered: block until a receiver takes `v`. Buffered: if there is room, put `v` at the back; otherwise block. |
| `v := <-ch` (receive) | Take the front value. If none is available, block until a sender provides one. |
| `v, ok := <-ch` | `ok == false` means the channel is **closed and drained**, and `v` is the zero value |
| `close(ch)` | Mark it as "no more values will come". Receivers drain what is left, then get `ok == false` immediately, forever. |
| `for v := range ch` | Receive until the channel is closed and drained |
| `len(ch)`, `cap(ch)` | Items currently buffered; capacity. They are only snapshots and useless for synchronization. |

**Syntax | Input | Output | Why**

| Syntax | Input | Output | Why you use it |
| --- | --- | --- | --- |
| `make(chan T, n)` | element type T; capacity n (omit or 0 = unbuffered) | `chan T` | Create the conduit |
| `ch <- v` | a value of type T | nothing; **blocks** while no receiver is ready (unbuffered) or the buffer is full | Hand off a result |
| `v, ok := <-ch` | – | value; `ok` = false if closed and empty. **Blocks** while empty and open. | Take a result |
| `close(ch)` | an open channel | nothing | Tell receivers that the stream is finished |
| `for v := range ch {…}` | a channel | each value in turn; the loop ends after close | Consume a whole stream |

**State-transition table (buffered, cap 2).**

| Step | Operation | Buffer | Closed? | Who is blocked |
| --- | --- | --- | --- | --- |
| 1 | `ch <- 1` | [1] | no | – |
| 2 | `ch <- 2` | [1 2] | no | – |
| 3 | `ch <- 3` | [1 2] | no | **sender** (full) |
| 4 | `<-ch` → 1 | [2 3] | no | sender released, its 3 enters the queue |
| 5 | `close(ch)` | [2 3] | yes | – |
| 6 | `<-ch` → 2, ok | [3] | yes | – |
| 7 | `<-ch` → 3, ok | [] | yes | – |
| 8 | `<-ch` → 0, **!ok** | [] | yes | – (it never blocks again) |
| 9 | `ch <- 4` | – | yes | **panic: send on closed channel** |

**Complete behaviour table [Spec].**

| | nil channel | open, empty | open, has data / room | closed |
| --- | --- | --- | --- | --- |
| send | blocks forever | blocks (unbuffered, or full) | proceeds | **panic** |
| receive | blocks forever | blocks | proceeds | zero value, `ok=false`, at once |
| close | **panic** | ok | ok | **panic** |

**Memory guarantees [Spec, memory model].** A send happens before the corresponding receive completes, so everything the sender wrote *before* sending is visible to the receiver *after* receiving. Passing a `*Stats` over a channel therefore safely **transfers ownership** of the data it points to, as long as the sender stops touching it afterwards. Closing happens before a receive that returns because the channel is closed.

**Correctness rules.**

1. **The closer must know no more sends can happen.** Usually that is the sole sender, or a coordinator that closes after *all* senders have finished. **Receivers never close** (engineering guide Part 4).
2. **Every blocked send needs a receiver that will eventually come, and every blocked receive needs a sender or a close.** Otherwise the goroutine is leaked or deadlocked.

**Failure modes** (observed on go1.27.1).

| Mistake | Result |
| --- | --- |
| Send on an unbuffered channel with no receiver, in main | `fatal error: all goroutines are asleep - deadlock!` with `goroutine 1 [chan send]` |
| `range` over a channel nobody closes | Deadlock after the last value (same message), or a leaked goroutine |
| Send after close | `panic: send on closed channel` |
| Close twice | `panic: close of closed channel` |
| Close a nil channel | `panic: close of nil channel` |
| Worker blocked on a send nobody will receive | **Goroutine leak**: no error at all, just memory that grows |

**W-style mini program (run it in `~/Desktop/personal/warmups/ch.go`):**

```go
package main

import "fmt"

func main() {
	ch := make(chan int, 2)
	go func() {
		for i := range 5 {
			ch <- i // blocks whenever 2 values are waiting
		}
		close(ch) // sole sender closes
	}()
	for v := range ch {
		fmt.Println("got", v)
	}
	fmt.Println("channel closed, loop ended")
}
```

**What to observe:** `got 0` … `got 4`, always in order (channels are FIFO **[Spec]**), then the final line. Delete `close(ch)` and you get `fatal error: all goroutines are asleep - deadlock!` after `got 4`: `range` waits for a value that will never come.

### 7.2 Concept: directional channel types (New · Essential)

**Problem:** nothing stops a receiver function from accidentally sending, or closing. **Idea:** narrow the channel type in a function's signature, so the compiler enforces each role.

| Type | Allowed | Compile error |
| --- | --- | --- |
| `chan T` | send, receive, close | – |
| `chan<- T` (send-only) | send, close | receive |
| `<-chan T` (receive-only) | receive | send, **close** |

A `chan T` converts implicitly to either direction when passed as an argument **[Spec]**. Engineering guide §3.2: *channel parameters should specify direction.*

### 7.3 Concept: buffered results, and how big a buffer should be (Essential)

A buffer **decouples** the timing of sender and receiver: a worker can drop off its result and exit even if the aggregator is busy merging. Rules of thumb **[Convention]**:

- A buffer is **not** a fix for a missing receiver. It only delays the deadlock until the buffer fills.
- With N senders that each send exactly one value, `cap = N` guarantees that no sender ever blocks, at the cost of holding up to N results in memory.
- A small buffer bounds memory and provides **backpressure**: fast producers are slowed down to the consumer's pace.

### 7.4 Pattern: the closer goroutine (fan-in) (Essential)

**Problem:** N workers send on one channel. Who closes it? No single worker knows it is the last one.

**Construction:** a dedicated goroutine waits for all the senders on a WaitGroup, then closes. The aggregator ranges over the channel until it is closed.

```
worker 1 ──┐
worker 2 ──┼──▶ results ──▶ aggregator (range)
worker N ──┘        ▲
                    │ close() after wg.Wait()
              closer goroutine
```

**Why the closer must be a separate goroutine.** If the function that ranges over the channel calls `wg.Wait()` *itself* before ranging, with an unbuffered (or too small) channel, the workers block on send (nobody is receiving yet) while the function blocks in `Wait` (workers can't finish). That is a deadlock. You will trigger it in Phase 9.

### 7.5 Create `internal/analyzer/channel.go`

**What this code does:** it starts one goroutine per file. Each analyzes its file and **sends** a `result` (stats or error) on a buffered channel. A closer goroutine closes the channel after all workers finish. The calling goroutine is the only one that touches `total` (confinement), so there is no mutex.

**Step 7.5a, the result type.** It carries either a value or an error, so errors flow through the same channel as data.

```go
package analyzer

import (
	"fmt"
	"sync"

	"github.com/sidd-dhegaskar/log-analyzer/internal/accesslog"
)

// result is what a worker sends for one file: its stats or its error.
type result struct {
	path  string
	stats *accesslog.Stats
	err   error
}
```

**Step 7.5b, the sender (send-only parameter).**

```go
// analyzeTo analyzes one file and sends the outcome. It may only send.
func analyzeTo(path string, out chan<- result) {
	s, err := accesslog.AnalyzeFile(path)
	out <- result{path: path, stats: s, err: err}
}
```

After the send, this goroutine never touches `s` again. Ownership has moved to the receiver.

**Step 7.5c, the aggregator (receive-only parameter).**

```go
// aggregate merges results until the channel is closed. It may only receive.
func aggregate(in <-chan result) (*accesslog.Stats, error) {
	total := accesslog.NewStats()
	var firstErr error
	for r := range in {
		if r.err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("analyze %s: %w", r.path, r.err)
			}
			continue // keep draining so no sender blocks forever
		}
		total.Merge(r.stats)
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return total, nil
}
```

The `continue` is important. If we `return` on the first error, remaining workers might block forever on `out <- …`. With `cap = len(files)` they wouldn't, but relying on that is fragile. **Draining** keeps the shutdown correct whatever the buffer size.

**Step 7.5d, wire it together.**

```go
// Channel starts one goroutine per file. Each sends its private Stats on a
// results channel; only the calling goroutine (the aggregator) touches the
// total, so no mutex is needed.
//
// Channel ownership: Channel creates results; workers only send; a closer
// goroutine closes results after every worker has finished.
func Channel(files []string) (*accesslog.Stats, error) {
	results := make(chan result, len(files))

	var wg sync.WaitGroup
	for _, path := range files {
		wg.Go(func() { analyzeTo(path, results) })
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	return aggregate(results)
}
```

**Ownership table (the doc comment, expanded):**

| Resource | Created by | Sends | Receives | Closes | Ends when |
| --- | --- | --- | --- | --- | --- |
| `results` | `Channel` | workers (`chan<-`) | `aggregate` (`<-chan`) | closer goroutine, after `wg.Wait()` | closed and drained |
| worker goroutines | `Channel` via `wg.Go` | – | – | – | after their one send |
| closer goroutine | `Channel` | – | – | `results` | right after `close` |
| `total` | `aggregate` | – | – | – | returned to the caller (confined) |

Add the case in `main.go`:

```go
	case "channel":
		stats, err = analyzer.Channel(files)
```

### 7.6 Run, compare, race-check

```bash
go run ./cmd/analyze -mode=channel | cmp - baseline.json && echo SAME
go run -race ./cmd/analyze -mode=channel | cmp - baseline.json && echo SAME
```

**What to observe:** `SAME` twice, no race report. `elapsed` is similar to `locked`.

### 7.7 Shared memory vs communication (the README's final question)

| | `Locked` (shared memory) | `Channel` (communication) |
| --- | --- | --- |
| Who touches `total` | every worker, one at a time | only the aggregator |
| Safety relies on | every access remembering `mu` | the type system plus channel ownership |
| Error flow | `errs[i]` slots + `errors.Join` | errors travel inside `result` |
| Good when | small, hot shared state (a counter, a cache) | handing off data or ownership, building pipelines |

Neither is "better". Engineering guide Part 4: *channels to pass data or ownership, mutexes to guard state.*

**Checkpoint 7:** `SAME` with and without `-race`, and you can fill in the ownership table from memory.

---

## Phase 8: Bound concurrency with a worker pool (Milestone 8)

**Why now:** `Channel` starts **1,000 goroutines and opens up to 1,000 files at once**. With 100,000 files you could hit the OS limit on open files (check yours with `ulimit -n`; it varies by shell and system settings): `too many open files`. Goroutines are cheap. The **resources they hold** (file descriptors, memory, connections) are not.

### 8.1 Pattern: worker pool (fan-out / fan-in) (Essential), taught independently

**Problem it solves:** process many independent jobs with **at most N running at once**.

**Architecture and data flow.**

```
               jobs (unbuffered)                 results (buffered N)
producer ──▶ ─────────────────── ──▶ worker 1 ──┐
  (sole sender,                  ──▶ worker 2 ──┼──▶ ─────────── ──▶ aggregator
   closes jobs)                  ──▶ worker N ──┘          ▲
                                                           │ close after wg.Wait()
                                                     closer goroutine
```

**Primitives used:** goroutines (producer, N workers, closer), two channels, one WaitGroup.

**Lifecycle and termination (normal run):**

| # | Event | Consequence |
| --- | --- | --- |
| 1 | Producer sends every job, then `close(jobs)` | Workers' `range jobs` loops end once the remaining jobs are taken |
| 2 | Each worker returns | `wg` counter → 0 |
| 3 | Closer: `wg.Wait()` returns → `close(results)` | Aggregator's `range results` ends |
| 4 | Aggregator returns the total | Every goroutine has exited: no leaks |

The shutdown is a **cascade of closes**: each stage's input is closed, so it finishes and closes its own output.

**Small standalone implementation** (`~/Desktop/personal/warmups/pool.go`):

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	jobs := make(chan int)
	results := make(chan int, 3)

	go func() { // producer: sole sender on jobs
		defer close(jobs)
		for i := 1; i <= 9; i++ {
			jobs <- i
		}
	}()

	var wg sync.WaitGroup
	for range 3 { // exactly 3 workers, whatever the number of jobs
		wg.Go(func() {
			for n := range jobs {
				results <- n * n
			}
		})
	}
	go func() { wg.Wait(); close(results) }() // closer

	sum := 0
	for sq := range results {
		sum += sq
	}
	fmt.Println("sum of squares:", sum) // always 285
}
```

**What to observe:** always `285`. The order in which squares arrive varies, but the sum doesn't (addition is commutative, like `Merge`).

**Common incorrect implementations:**

| Bug | Why it fails |
| --- | --- |
| Workers `close(results)` when they finish | The first worker to finish closes it, and the others then `panic: send on closed channel` |
| Producer never closes `jobs` | Workers' `range jobs` never ends → `wg.Wait` never returns → results never closed → aggregator hangs |
| `wg.Wait()` then range over results, in the same goroutine | Deadlock once results fill: workers wait for space, Wait waits for workers |
| Aggregator returns early on an error without draining | Workers block forever on `results <-` (leak) unless they also watch a cancellation signal (Phase 10) |
| One goroutine per job "plus a semaphore" but acquired *inside* the goroutine | Still creates all the goroutines, so it bounds work but not goroutine count |

**When to use it in production:** CPU-bound work (N ≈ GOMAXPROCS), resource-bound work (N = connection-pool size, file-descriptor budget, an external API's concurrency limit). For error handling plus cancellation in one, see `errgroup` (Track B4).

### 8.2 Choosing N: `runtime.GOMAXPROCS(0)` (small; full treatment in Phase 13)

| Syntax | Input | Output | Why you use it |
| --- | --- | --- | --- |
| `runtime.GOMAXPROCS(n int)` | n < 1 means "don't change, just report" | `int`: the **previous** setting | `GOMAXPROCS(0)` reads how many OS threads may run Go code at once (default: the number of logical CPUs) |

### 8.3 Create `internal/analyzer/pool.go`, version 1 (no cancellation yet)

**What this code does:** a producer goroutine feeds file paths into `jobs`. Exactly `Workers` goroutines take paths, analyze them and send `result`s. A closer goroutine closes `results` when the workers are done, and `Pool`'s own goroutine merges. At most `Workers` files are open at any moment. It reuses `result` and `aggregate` from `channel.go` (same package).

```go
package analyzer

import (
	"sync"

	"github.com/sidd-dhegaskar/log-analyzer/internal/accesslog"
)

// Options configures Pool.
type Options struct {
	Workers int // number of worker goroutines; values < 1 mean 1
}

// Pool analyzes files with a fixed number of workers.
//
// Channel ownership: the producer is the only sender on jobs and closes it;
// workers send on results; a closer goroutine closes results once all
// workers have returned; Pool's own goroutine is the only receiver.
func Pool(files []string, opts Options) (*accesslog.Stats, error) {
	workers := max(opts.Workers, 1)
	jobs := make(chan string)
	results := make(chan result, workers)

	go produce(files, jobs)

	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() { work(jobs, results) })
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	return aggregate(results)
}

// produce sends every path on jobs, then closes it. It is the only sender.
func produce(files []string, jobs chan<- string) {
	defer close(jobs)
	for _, path := range files {
		jobs <- path
	}
}

// work analyzes paths from jobs until jobs is closed.
func work(jobs <-chan string, results chan<- result) {
	for path := range jobs {
		s, err := accesslog.AnalyzeFile(path)
		results <- result{path: path, stats: s, err: err}
	}
}
```

| Decision | Why |
| --- | --- |
| `Options` struct | Engineering guide §3.2: an option struct for configuration. Phase 10 adds a field without changing every caller. |
| `max(opts.Workers, 1)` | Builtin `max` (Go 1.21). Zero workers would mean no one ever reads `jobs`: a deadlock. |
| `jobs` unbuffered | The producer hands out one path at a time, only when a worker is free. Nothing needs to be buffered. |
| `results` cap = `workers` | Each worker can drop one result without waiting for the aggregator: a small, bounded buffer |
| `produce(files, jobs)` gets `chan<-`, `work` gets `<-chan` and `chan<-` | Directional types make the ownership table compiler-checked |

Add the flag and the case in `main.go`:

```go
	workers := flag.Int("workers", runtime.GOMAXPROCS(0), "worker count for -mode=pool")
```

Pass `*workers` into `run` (add a `workers int` parameter) and add:

```go
	case "pool":
		stats, err = analyzer.Pool(files, analyzer.Options{Workers: workers})
```

Change the `-mode` usage string to `"sequential | racy | locked | channel | pool"` and its default to `"pool"`.

### 8.4 Run, compare, vary N

```bash
go run ./cmd/analyze -mode=pool | cmp - baseline.json && echo SAME
go run -race ./cmd/analyze -mode=pool | cmp - baseline.json && echo SAME
for w in 1 2 4 8 16; do go run ./cmd/analyze -mode=pool -workers=$w > /dev/null; done
```

Observed `elapsed` on this 8-CPU machine: w=1 ≈ 325 ms (≈ sequential), w=2 ≈ 160 ms, w=4 ≈ 105 ms, w=8 ≈ 95 ms, w=16 ≈ 110 ms. **What it teaches:** speed-up flattens once workers ≥ cores. More goroutines than cores only adds scheduling overhead for CPU-bound parsing.

### 8.5 Failure scenarios (normal → partial → total)

| Scenario | What happens in Pool v1 | Acceptable? |
| --- | --- | --- |
| Normal | Cascade of closes, report = baseline | yes |
| One file is slow | Its worker is busy longer; the other N−1 keep draining `jobs` | yes |
| One file fails (unreadable) | Its error flows as a `result`; `aggregate` records it, keeps draining, returns it | yes, but the other 999 files are still processed **for nothing** |
| All files fail | 1,000 errors flow; the first is returned | yes, but wasteful |
| Caller wants to stop (Ctrl+C) | **No way to stop**: the run continues until all files are done | **no**, fixed in Phase 10 |
| A worker never finishes (hung disk read) | `wg.Wait` never returns, Pool hangs | needs cancellation and timeouts that `AnalyzeFile` respects. See the Phase 10 note on granularity. |

**Checkpoint 8:** `SAME` with and without `-race`, and you have a timing table for w = 1, 2, 4, 8, 16.

---

## Phase 9: Break things on purpose (README experiments)

Learning-cycle step 3. For each experiment: **write down your prediction first**, make the change, run it, then **revert** (`git stash` / `git checkout -- <file>`, or undo by hand). Run `cmp - baseline.json` after each revert.

| # | Action | Predict, then observe | What it teaches |
| --- | --- | --- | --- |
| E1 | In `locked.go`, comment out `mu.Lock()` and `mu.Unlock()`. Run `go run -race ./cmd/analyze -mode=locked > /dev/null` | `WARNING: DATA RACE` inside `Merge`, `Found N data race(s)`, exit status 66. Without `-race`: sometimes `fatal error: concurrent map writes`, sometimes wrong totals, sometimes correct (!) | A race can't be found by testing outputs. You need `-race`. Same lesson as Phase 5, now in code you thought was correct. |
| E2 | In `locked.go`, delete `wg.Wait()`. Run `go run ./cmd/analyze -mode=locked`, then the same with `-race` | `elapsed≈1.7ms`, `goroutines=998`, a partial report (I saw `total_requests: 6000`). Under `-race` I saw **no race report** in 3 runs: the program printed and exited before the workers' writes happened | Without the happens-before edge from `Wait`, main reads `total` while workers still write it: missing results **and** a data race. `-race` reports only races that actually happen during that run. A race it misses is still a race. (Phase 3 showed the same thing without merging.) |
| E3a | In `channel.go`, change `make(chan result, len(files))` to `make(chan result)`. Run `-mode=channel \| cmp - baseline.json` | Still `SAME`, with similar timing | Unbuffered is still *correct* here because the closer goroutine lets the aggregator receive while workers send |
| E3b | Keep E3a. To **see** the blocking, temporarily add to the `aggregate` loop: `time.Sleep(200 * time.Microsecond)` and, every 250 results, print `runtime.NumGoroutine()` (needs imports `time` and `runtime`) | Buffered: `received 0 → 994 goroutines, 250 → 410, 500 → 1` (workers dropped their results and exited). Unbuffered: `0 → 1001, 250 → 751, 500 → 501, 750 → 251` (each worker is **parked on its send** until the slow aggregator takes its value) | A buffer decouples sender from receiver. Without one, a fast sender waits at the slowest receiver's pace. Parked goroutines hold their memory, including their `*Stats`. |
| E3c | Unbuffered, **and** replace the closer goroutine with `wg.Wait(); close(results)` written directly in `Channel` before `return aggregate(results)` | `fatal error: all goroutines are asleep - deadlock!`, with `goroutine 1 [sync.WaitGroup.Wait]` and workers in `[chan send]` | Wait (in main) waits for workers. Workers wait for a receiver. The receiver runs only after Wait. That is a cycle. With `cap = len(files)` this "works" only because nobody needs to block, which is fragile. |
| E4 | In `channel.go`, replace the closer goroutine with a plain `close(results)` right after the loop (before workers finish) | A report with zeros or partial counts, `elapsed ≈ 1ms`, `goroutines ≈ 999`, and `panic: send on closed channel` (the order of the report and the panic varies) | Violates the closing rule: the closer did *not* know that no more sends could happen. A send on a closed channel always panics **[Spec]**. |
| E5 | Vary `GOMAXPROCS`: done in Phase 13 with `go test -bench -cpu=1,2,4,8` | See Phase 13 | Parallelism versus concurrency |

**Why E3c behaves differently after Phase 10:** once `main` uses `signal.NotifyContext`, the `os/signal` package keeps an extra goroutine alive waiting for signals. The runtime deadlock detector fires only when **every** goroutine is blocked **[Runtime]**, so the same bug then **hangs silently forever**, with no message. I observed exactly this on go1.27.1. In real servers, deadlocks look like hangs. Don't rely on that fatal error message.

**Checkpoint 9:** all five experiments done, predictions written next to the actual results, the code reverted, and `-mode=channel | cmp - baseline.json` prints `SAME` again.

---

## Phase 10: Cancellation with `select`, `context` and Ctrl+C (Milestone 9)

**Why now:** the Phase 8 failure table showed that Pool can't stop. A user pressing Ctrl+C, a failing file, or a deadline must make every goroutine **notice and return**. A worker can't simply "check a flag" while it is blocked on a channel operation. It needs to wait for **"my channel operation" or "stop"**, whichever happens first.

### 10.1 Concept first: `select` (New · Essential)

**The problem.** A goroutine blocked on `results <- r` is stuck until someone receives. If the receiver has given up, it is stuck forever (a leak). We need a way to block on **several** channel operations and proceed with whichever can go first.

**The core idea [Spec].** `select` is like a `switch` whose cases are channel operations. It blocks until **at least one** case can proceed, then runs exactly one of them. If **several** are ready, it picks one **uniformly at random**. There is no priority by order of the cases. With a `default` case it never blocks.

```
select {
case results <- r:   ◀── ready when a receiver is waiting or the buffer has room
case <-done:         ◀── ready when done is closed
}                        blocks until one is ready; if both are ready, a random one wins
```

**Analogy.** Waiting at a crossroads for whichever bus comes first: the "results" bus or the "go home" bus.

**Operations.**

| Form | Behaviour |
| --- | --- |
| `select { case ch <- v: … case <-other: … }` | Block until one case is ready, then run it |
| a `case` on a **nil** channel | Never ready. This is a way to switch a case off dynamically. |
| `default:` | Runs immediately if no case is ready (non-blocking attempt) |
| `select {}` | Blocks forever |

**Syntax | Input | Output | Why**

| Syntax | Input | Output | Why you use it |
| --- | --- | --- | --- |
| `select { case …: … }` | send/receive cases | runs one ready case; **blocks** until one is ready (unless there is `default`) | Wait for work *or* cancellation |
| `case v, ok := <-ch:` | – | the received value; `ok` false if closed | Receive inside a select |
| `case ch <- v:` | value | – | Send inside a select |

**The done-channel idiom (the README's "done channel") [Convention built on a Spec guarantee].** A receive from a **closed** channel succeeds immediately, for **every** receiver, forever. So `close(done)` is a **broadcast** that any number of goroutines can observe, which a send can't do (a send wakes only one receiver).

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	done := make(chan struct{}) // struct{}: carries no data, only the close matters
	ticks := make(chan int)

	go func() {
		defer fmt.Println("worker: exiting")
		for i := 0; ; i++ {
			select {
			case ticks <- i:
			case <-done:
				return
			}
		}
	}()

	for range 3 {
		fmt.Println("got", <-ticks)
	}
	close(done) // broadcast "stop"
	time.Sleep(10 * time.Millisecond) // only so the demo can print the exit line
}
```

**What to observe:** `got 0`, `got 1`, `got 2`, `worker: exiting`. Remove the `case <-done` branch and the worker blocks forever on `ticks <- 3`: a **leak**. In this tiny program you'd never notice. In a server it grows with every request. (The `Sleep` is only there so the demo can print. Real code waits with a WaitGroup.)

**Trace of the select (after main has received 3 values):**

| Step | `ticks <- 3` ready? | `<-done` ready? | Chosen |
| --- | --- | --- | --- |
| worker enters select | no (main is no longer receiving) | no | blocks |
| main runs `close(done)` | no | **yes** | `<-done` → return |

**Failure modes.**

| Mistake | Result |
| --- | --- |
| A blocking select with no `done`/`ctx.Done()` case | Leak once the other side gives up |
| Assuming the first case has priority | It doesn't. A ready result may still be sent after cancellation (handle that, see 10.5). |
| `default` inside a `for` loop with nothing else blocking | Busy loop burning 100% CPU |
| Sending on `done` instead of closing it | Only one goroutine wakes, and the sender blocks if nobody is listening |

### 10.2 Concept first: `context.Context` (New · Essential)

**The problem.** A done channel works, but real programs need more: cancellation that **flows down a call tree** (cancelling a request cancels everything it started), **deadlines**, and a **reason** for the stop. Every library would invent its own done channel. `context` standardises it.

**The core idea [API].** A `Context` is a **read-only cancellation signal plus a reason**, passed down as the first argument. Its `Done()` method returns a channel that is **closed** when the context is cancelled: it is the done-channel idiom, standardised. Contexts form a **tree**: cancelling a parent cancels all children, while cancelling a child never affects its parent.

```
context.Background()                     ← root: never cancelled (made in main or tests only)
   └── signal.NotifyContext   (Ctrl+C)   ← cancelled by SIGINT
         └── context.WithCancel (Pool)    ← cancelled by Pool on first error, or by its parent
               ├── producer   watches ctx.Done()
               ├── worker 1   watches ctx.Done()
               └── worker N   watches ctx.Done()
```

**Analogy.** A fire alarm wired through a building. Pulling the alarm on one floor sounds it on every floor below, never above. People must *hear it and walk out themselves*. The alarm doesn't teleport them.

**Cancellation is cooperative [API].** Cancelling only **closes `Done()` and sets `Err()`**. It does **not** stop any goroutine. Code must check `ctx.Done()` or `ctx.Err()` and return by itself. A goroutine stuck in a long computation or a blocking syscall that never checks the context keeps running.

**Operations.**

| Syntax | Input | Output | Why you use it |
| --- | --- | --- | --- |
| `context.Background()` | – | an empty root `Context`, never cancelled | The root, in `main` and in tests |
| `context.WithCancel(parent Context)` | parent | `ctx Context`, `cancel CancelFunc` | A child you can cancel yourself. **You must call `cancel`** (usually `defer cancel()`) to release it, even on success. `go vet` reports a lost cancel. |
| `context.WithTimeout(parent, d time.Duration)` | parent; duration | `ctx`, `cancel` | Cancelled automatically after d (`Err` = `DeadlineExceeded`). Used in tests. |
| `ctx.Done()` | – | `<-chan struct{}`, closed on cancellation | Use it as a `select` case |
| `ctx.Err()` | – | `nil` while active; then `context.Canceled` or `context.DeadlineExceeded` | Check after the fact |
| `context.Cause(ctx)` (Go 1.20) | ctx | `error`: the specific reason, if one was given; else the same as `Err()` | Better error messages |
| `cancel()` | – | nothing; idempotent (safe to call many times) | Trigger cancellation and release resources |

**State table.**

| Step | Event | `ctx.Done()` | `ctx.Err()` | Children |
| --- | --- | --- | --- | --- |
| 1 | `ctx, cancel := WithCancel(parent)` | open | nil | – |
| 2 | workers start, `select` on `Done` | open | nil | – |
| 3 | `cancel()` (or the parent is cancelled) | **closed** | `Canceled` | all cancelled too |
| 4 | `cancel()` again | closed | `Canceled` | no effect |

**Lifecycle and ownership rules.**

| Question | Answer |
| --- | --- |
| Who creates the root? | Only `main`, `init` or tests (engineering guide Part 4) |
| Who owns `cancel`? | The function that called `WithCancel`. It alone decides when to cancel, and it **must** call it before returning. |
| How is it passed? | As the **first parameter**, named `ctx`. **Never stored in a struct** (engineering guide Part 4). Never put in `Options`. |
| Who observes it? | Every goroutine that can block |
| What happens to work in progress? | It finishes its current step, then returns at its next check (cooperative) |

**Failure modes.**

| Mistake | Result |
| --- | --- |
| Not calling `cancel` | The child stays registered with its parent until the parent ends: a leak. `go vet`: "the cancel function … should be called" |
| Checking `ctx` only at the start | Cancellation is ignored for the rest of the run |
| Storing `ctx` in a struct | Its lifetime becomes unclear, and the wrong request's context gets used |
| Treating cancellation as "stop now" | Goroutines keep running until they reach a check: wait for them (`wg`) |

### 10.3 Concept: Ctrl+C as a context, `signal.NotifyContext` (small)

| Syntax | Input | Output | Why you use it |
| --- | --- | --- | --- |
| `signal.NotifyContext(parent Context, sigs ...os.Signal)` | parent; signals such as `os.Interrupt` (Ctrl+C) | `ctx`, `stop func()` | `ctx` is cancelled when a listed signal arrives. `stop()` unregisters the handler and releases resources. Call it (deferred). |

While registered, Ctrl+C **no longer kills the process**. It only cancels `ctx`, so your code must react. Observed on go1.27.1: `context.Cause(ctx)` is an error whose text is `interrupt signal received`, and `errors.Is(cause, context.Canceled)` is still true. **Watch for:** a second Ctrl+C is also swallowed until `stop()` runs. A common production refinement is to call `stop()` as soon as `ctx.Done()` fires, so a second Ctrl+C kills the process immediately.

**`time.After` (small; used only by the experiment delay).**

| Syntax | Input | Output | Why you use it |
| --- | --- | --- | --- |
| `time.After(d time.Duration)` | duration | `<-chan time.Time` that receives once after d | A timeout or delay you can put in a `select` next to `ctx.Done()` |

Since Go 1.23 (with `go ≥ 1.23` in `go.mod`), an unused `time.After` timer is garbage-collected even if it never fires **[API]**. Before that, a `time.After` inside a hot loop leaked memory until it fired.

### 10.4 Update `internal/analyzer/pool.go` to the final version

**What changes:** `Pool` takes `ctx` first, derives its own cancellable child, cancels it on the first file error (fail fast), and the producer and workers `select` on `ctx.Done()` wherever they could block. The aggregation loop moves inline because it now needs to call `cancel`. `Pool` still returns only after **every** goroutine it started has exited.

```go
package analyzer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sidd-dhegaskar/log-analyzer/internal/accesslog"
)

// Options configures Pool.
type Options struct {
	Workers int // number of worker goroutines; values < 1 mean 1

	// FileDelay adds an artificial pause per file so cancellation can be
	// observed by hand. Leave it zero for real runs.
	FileDelay time.Duration
}
```

`FileDelay` exists only so a human can press Ctrl+C before the 0.1 s run finishes. Without it the run ends before you can react.

**Producer: stop sending if cancelled.**

```go
// produce sends every path on jobs, then closes it. It stops early if ctx is
// cancelled. It is the only sender on jobs.
func produce(ctx context.Context, files []string, jobs chan<- string) {
	defer close(jobs)
	for _, path := range files {
		select {
		case jobs <- path:
		case <-ctx.Done():
			return
		}
	}
}
```

`defer close(jobs)` runs on **both** exits, so workers' `range jobs` always ends.

**Worker: two blocking points, two selects.**

```go
// work analyzes paths from jobs until jobs is closed or ctx is cancelled.
func work(ctx context.Context, jobs <-chan string, results chan<- result, delay time.Duration) {
	for path := range jobs {
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return
			}
		}
		s, err := accesslog.AnalyzeFile(path)
		select {
		case results <- result{path: path, stats: s, err: err}:
		case <-ctx.Done():
			return
		}
	}
}
```

**Cancellation granularity:** `AnalyzeFile` does not take a `ctx`, so a worker notices cancellation *between* files. That takes about 0.3 ms here, which is fine. For files that take seconds, pass `ctx` into the file reader and check `ctx.Err()` every few thousand lines.

**Pool itself.**

```go
// Pool analyzes files with a fixed number of workers. It stops early, and
// returns the cause, when ctx is cancelled or a file cannot be read. Pool
// returns only after every goroutine it started has exited.
//
// Channel ownership: the producer is the only sender on jobs and closes it;
// workers send on results; a closer goroutine closes results once all
// workers have returned; Pool's own goroutine is the only receiver.
func Pool(ctx context.Context, files []string, opts Options) (*accesslog.Stats, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	workers := max(opts.Workers, 1)
	jobs := make(chan string)
	results := make(chan result, workers)

	go produce(ctx, files, jobs)

	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() { work(ctx, jobs, results, opts.FileDelay) })
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	total := accesslog.NewStats()
	var firstErr error
	for r := range results {
		if r.err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("analyze %s: %w", r.path, r.err)
				cancel() // tell producer and workers to stop early
			}
			continue
		}
		total.Merge(r.stats)
	}
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, context.Cause(ctx)
	}
	return total, nil
}
```

| Line | Why |
| --- | --- |
| `ctx, cancel := context.WithCancel(ctx)` | Pool needs its **own** cancel for fail-fast, without cancelling the caller's context. Shadowing `ctx` on purpose stops anything from using the parent by mistake. |
| `defer cancel()` | Always release the child context, even on success |
| `for r := range results` (no `select` on `ctx.Done()`) | The aggregator **keeps draining until `results` is closed**. Because workers return promptly on cancellation, the close comes soon, and when Pool returns every goroutine has exited (structured shutdown, no leaks). |
| `cancel()` on the first error | Fail fast: the producer stops handing out jobs and workers stop at their next check |
| Merging results that arrive after cancel | Harmless: `select` may still pick the send case if both are ready **[Spec: random choice]**. The total is discarded anyway. |
| `return nil, context.Cause(ctx)` | **A partial report is never returned.** It would silently differ from the baseline (assumption A8). |

The `aggregate` helper from `channel.go` stays in use by `Channel`. Pool doesn't reuse it because it must call `cancel`.

**Ownership after this change:**

| Resource | Created by | Cancelled or closed by | Observed by |
| --- | --- | --- | --- |
| parent ctx | `main` (NotifyContext) | SIGINT, or `stop()` | Pool (inherited) |
| child ctx + `cancel` | `Pool` | Pool (first error, `defer`), or the parent | producer, workers |
| `jobs` | Pool | producer (`defer close`) | workers (`range`) |
| `results` | Pool | closer goroutine | Pool (`range`) |

### 10.5 Failure scenarios for the final Pool

| Scenario | Execution | Termination |
| --- | --- | --- |
| Normal | Cascade of closes | Report = baseline |
| One file unreadable | Its error is a result → `cancel()` → producer returns and closes `jobs` → workers exit at their next select → closer closes `results` | Returns `analyze …: open …: no such file or directory` |
| All files fail | First error cancels, the rest are drained and ignored | Same, quickly |
| Caller cancels (Ctrl+C or timeout) | Every blocked point has a `ctx.Done()` case | Returns `Cause`, which matches `context.Canceled` or `DeadlineExceeded` with `errors.Is` |
| Cancelled **before** Pool starts | Producer's first select may send one job or return immediately (random). Workers return at once. | Returns the context error, no leak |
| A result is ready at the moment of cancel | Either select case may win, and both are handled | No leak, partial total discarded |
| A worker never finishes (stuck in a syscall) | ctx can't interrupt it. Pool waits on the closer, which waits on `wg`. | **Hangs.** This is the limit of cooperative cancellation. The fix belongs in the blocking call (I/O with deadlines). |

### 10.6 Update `main.go` for Ctrl+C

Add the imports `context`, `errors` and `os/signal`. Then:

```go
	delay := flag.Duration("file_delay", 0, "artificial per-file delay for -mode=pool (e.g. 5ms)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := run(ctx, *dir, *mode, *workers, *top, *delay); err != nil {
		fmt.Fprintln(os.Stderr, "analyze:", err)
		if errors.Is(err, context.Canceled) {
			os.Exit(130) // conventional exit code for "stopped by Ctrl+C"
		}
		os.Exit(1)
	}
```

`run` takes `ctx context.Context` as its **first** parameter, plus `delay time.Duration`, and the pool case becomes:

```go
	case "pool":
		stats, err = analyzer.Pool(ctx, files, analyzer.Options{Workers: workers, FileDelay: delay})
```

| Syntax | Input | Output | Why you use it |
| --- | --- | --- | --- |
| `flag.Duration(name string, default time.Duration, usage string)` | ″ | `*time.Duration`; parses `5ms`, `1s` | `-file_delay` |

Flag names use snake_case (engineering guide §2.2: `--worker_count` → `workerCount`). `os.Exit` skips defers, so `stop()` doesn't run on that path. That's harmless because the process is ending anyway.

### 10.7 Try it

```bash
go run ./cmd/analyze -mode=pool | cmp - baseline.json && echo SAME
go build -o analyze ./cmd/analyze
./analyze -mode=pool -file_delay=20ms > out.json    # press Ctrl+C after ~1 second
echo "exit=$?"; wc -c out.json
```

Observed:

```
analyze: interrupt signal received
exit=130
       0 out.json
```

**What to observe:** it stops within milliseconds, stdout is empty (no partial report), and the exit code is 130. Without the `select`s in `produce` and `work`, Ctrl+C would cancel the context but goroutines would keep blocking. With `-file_delay=20ms` the run takes about 2.5 s, so Pool would simply finish, ignoring you.

Also notice `goroutines=3` on stderr in normal runs now: main plus 2 goroutines started by `os/signal` and `NotifyContext` **[Runtime: the count is an implementation detail]**. That is the baseline for leak checks.

**Checkpoint 10:** `SAME` for a normal run, and exit 130 with empty stdout for a Ctrl+C run.

---

## Phase 11: The JSON report as a contract (Milestone 10)

**Why now:** the JSON already prints (Phase 2). Milestone 10 is about making the output **dependable for other programs**: scripts, CI and dashboards consume it.

### 11.1 The output contract

| Aspect | Rule | Where it is enforced |
| --- | --- | --- |
| stdout | **Only** the JSON report, followed by a newline | `run`: `enc.Encode` is the only write to stdout |
| stderr | Diagnostics (`mode=… elapsed=… goroutines=…`), errors | `fmt.Fprintf(os.Stderr, …)` |
| Exit code 0 | Report printed and complete | `main` |
| Exit code 1 | Any error, no report | `main` |
| Exit code 130 | Cancelled by Ctrl+C, no report | `main` |
| Determinism | Same input → byte-identical stdout, whatever the `-mode` or `-workers` | Sorted map keys **[API]**, field order **[API]**, tie-break (A5), integer latency sum (A6) |
| Empty input | `top_endpoints: []`, not `null` | `make([]EndpointCount, 0, …)` |
| Encode errors | Returned, not ignored | `return enc.Encode(...)` |

### 11.2 The final `cmd/analyze/main.go` (complete)

This file has changed in Phases 2, 3, 6, 7, 8 and 10. Check yours against it line by line:

```go
// Command analyze prints a JSON report for a directory of access logs.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"time"

	"github.com/sidd-dhegaskar/log-analyzer/internal/accesslog"
	"github.com/sidd-dhegaskar/log-analyzer/internal/analyzer"
)

func main() {
	dir := flag.String("dir", "logs", "directory of *.log files")
	mode := flag.String("mode", "pool", "sequential | racy | locked | channel | pool")
	workers := flag.Int("workers", runtime.GOMAXPROCS(0), "worker count for -mode=pool")
	top := flag.Int("top", 10, "number of endpoints in the report")
	delay := flag.Duration("file_delay", 0, "artificial per-file delay for -mode=pool (e.g. 5ms)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := run(ctx, *dir, *mode, *workers, *top, *delay); err != nil {
		fmt.Fprintln(os.Stderr, "analyze:", err)
		if errors.Is(err, context.Canceled) {
			os.Exit(130) // conventional exit code for "stopped by Ctrl+C"
		}
		os.Exit(1)
	}
}

func run(ctx context.Context, dir, mode string, workers, top int, delay time.Duration) error {
	files, err := analyzer.ListFiles(dir)
	if err != nil {
		return err
	}
	start := time.Now()

	var stats *accesslog.Stats
	switch mode {
	case "sequential":
		stats, err = analyzer.Sequential(files)
	case "racy":
		stats, err = analyzer.Racy(files)
	case "locked":
		stats, err = analyzer.Locked(files)
	case "channel":
		stats, err = analyzer.Channel(files)
	case "pool":
		stats, err = analyzer.Pool(ctx, files, analyzer.Options{Workers: workers, FileDelay: delay})
	default:
		return fmt.Errorf("unknown -mode %q", mode)
	}
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "mode=%s files=%d elapsed=%v goroutines=%d\n",
		mode, len(files), time.Since(start), runtime.NumGoroutine())

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(stats.Report(top))
}
```

`run` has 6 parameters, which is at the edge of the engineering guide's "keep parameter lists short". Bundling `mode`, `workers`, `top` and `delay` into a `config` struct would be the next refactor. It is left as is so each phase's diff stays small.

### 11.3 Verify the contract

```bash
for m in sequential locked channel pool; do
  go run ./cmd/analyze -mode=$m 2>/dev/null | cmp - baseline.json && echo "$m SAME"
done
go run ./cmd/analyze 2>/dev/null | python3 -m json.tool > /dev/null && echo "valid JSON"
go run ./cmd/analyze -mode=nope; echo "exit=$?"                       # analyze: unknown -mode "nope", exit=1
go run ./cmd/analyze -dir /does/not/exist; echo "exit=$?"             # open /does/not/exist: no such file or directory, exit=1
mkdir -p /tmp/empty-logs && go run ./cmd/analyze -dir /tmp/empty-logs 2>/dev/null   # top_endpoints: []
```

Note: with `go run`, the exit code you see is the one `go run` itself returns, which is 1 for any non-zero child status. Build the binary (`go build -o analyze ./cmd/analyze`) to see the real 130.

**Checkpoint 11:** four `SAME` lines, `valid JSON`, and correct messages and exit codes for the error cases.

---

## Phase 12: Tests: equivalence, cancellation, leaks, `-race` (README tools)

**Why now:** `cmp - baseline.json` checks one data set by hand. Tests make the guarantees **automatic**: every strategy matches Sequential, cancellation stops quickly, and no goroutine outlives `Pool`.

### 12.1 Add the test dependency

```bash
go get github.com/google/go-cmp@v0.7.0
go mod tidy
```

`go.mod` now has `require github.com/google/go-cmp v0.7.0`, and a `go.sum` appears (commit both).

| Syntax | Input | Output | Why you use it |
| --- | --- | --- | --- |
| `cmp.Diff(want, got any)` | two values of the same type | `string`: empty if equal, else a readable `-want +got` diff | Compare whole `Report` structs (engineering guide §5: don't compare serialized JSON in tests) |
| `t.TempDir()` | – | `string`: a fresh directory, deleted after the test | Hermetic test data |
| `t.Helper()` | – | nothing | Failures are reported at the caller's line |
| `testing.TB` | – | interface satisfied by both `*testing.T` and `*testing.B` | One helper for tests and benchmarks |

`go-cmp`'s package is also named `cmp`, the same name as the standard library's `cmp` package. This project doesn't import the stdlib one, so there is no clash.

### 12.2 `internal/analyzer/analyzer_test.go`, the helper and the equivalence test

**What this code does:** it generates a small deterministic data set in a temp directory, runs every correct strategy (including Pool with 1 and 8 workers), and checks that each `Report` equals Sequential's. `Racy` is excluded on purpose.

```go
package analyzer

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/sidd-dhegaskar/log-analyzer/internal/accesslog"
	"github.com/sidd-dhegaskar/log-analyzer/internal/loggen"
)

// testFiles generates a small deterministic data set and returns its paths.
func testFiles(t testing.TB, files, lines int) []string {
	t.Helper()
	dir := t.TempDir()
	if err := loggen.Generate(dir, files, lines, 42); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	paths, err := ListFiles(dir)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	return paths
}

func TestStrategiesMatchSequential(t *testing.T) {
	files := testFiles(t, 50, 200)
	base, err := Sequential(files)
	if err != nil {
		t.Fatalf("Sequential: %v", err)
	}
	want := base.Report(10)

	tests := []struct {
		name string
		run  func([]string) (*accesslog.Stats, error)
	}{
		{"locked", Locked},
		{"channel", Channel},
		{"pool-1", func(f []string) (*accesslog.Stats, error) { return Pool(context.Background(), f, Options{Workers: 1}) }},
		{"pool-8", func(f []string) (*accesslog.Stats, error) { return Pool(context.Background(), f, Options{Workers: 8}) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.run(files)
			if err != nil {
				t.Fatalf("%s: %v", tt.name, err)
			}
			if diff := cmp.Diff(want, got.Report(10)); diff != "" {
				t.Errorf("%s report mismatch (-want +got):\n%s", tt.name, diff)
			}
		})
	}
}
```

Table-driven with named subtests, and a function-valued field so that strategies with different signatures fit one table. Tests create their root context with `context.Background()`, which engineering guide Part 4 allows.

### 12.3 Error and cancellation tests

```go
func TestPoolMissingFile(t *testing.T) {
	files := testFiles(t, 20, 10)
	files = append(files, filepath.Join(t.TempDir(), "missing.log"))
	_, err := Pool(context.Background(), files, Options{Workers: 4})
	if err == nil {
		t.Fatal("Pool with a missing file: got nil error, want error")
	}
}

func TestPoolCancelled(t *testing.T) {
	files := testFiles(t, 100, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := Pool(ctx, files, Options{Workers: 4, FileDelay: 10 * time.Millisecond})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Pool after timeout: err = %v, want %v", err, context.DeadlineExceeded)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Pool took %v to stop after cancellation, want < 1s", elapsed)
	}
}
```

Without cancellation, 100 files × 10 ms ÷ 4 workers would take about 250 ms. The test proves that Pool both **returns the right error** and **stops early**. `errors.Is` is used, never string matching (engineering guide §3.1).

### 12.4 Goroutine-leak test (`runtime.NumGoroutine`, README tool)

**Idea:** count goroutines before, run, count after. Anything extra is a goroutine that outlived its owner. **Subtlety:** the closer goroutine calls `close(results)`, which lets Pool return, and only *then* finishes its own last instruction. For a moment it may still be counted, so the test polls briefly instead of checking exactly once.

```go
func TestPoolNoGoroutineLeak(t *testing.T) {
	files := testFiles(t, 100, 10)
	broken := append([]string{filepath.Join(t.TempDir(), "missing.log")}, files...)
	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()                                                       // already cancelled: the hardest case for clean shutdown
	_, _ = Pool(ctx, files, Options{Workers: 8})                   // only the goroutine count matters here
	_, _ = Pool(context.Background(), files, Options{Workers: 8})  // only the goroutine count matters here
	_, _ = Pool(context.Background(), broken, Options{Workers: 8}) // fails on the first file

	// The closer goroutine may still be returning; give it a moment.
	deadline := time.Now().Add(time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Errorf("goroutines: before=%d after=%d, want after <= before", before, after)
	}
}
```

It covers the three ways Pool can end: cancelled before it starts, a normal run, and fail-fast on an error (`broken` puts a missing file first). `_, _ =` with a comment is the engineering guide's rule for deliberately discarded errors (§3.1).

### 12.5 Run everything with the race detector

```bash
go vet ./...
go test -race -count=1 ./...
```

Expected:

```
ok  	github.com/sidd-dhegaskar/log-analyzer/internal/accesslog	1.4s
ok  	github.com/sidd-dhegaskar/log-analyzer/internal/analyzer	1.9s
```

`-count=1` disables the test cache, so the tests really run.

**Prove the leak test works (a broken implementation on purpose).** In `pool.go` make two changes: (1) in Pool's loop, replace `continue` after `cancel()` with `return nil, firstErr` (stop draining on the first error); (2) in `work`, delete the `case <-ctx.Done(): return` branch of the **result** `select`. Then run:

```bash
go test -count=1 -run Leak ./internal/analyzer/
```

Observed:

```
analyzer_test.go:102: goroutines: before=2 after=7, want after <= before
FAIL
```

Why: Pool returned, so nobody receives from `results` any more. Workers fill its buffer, then block forever on a send that has no `ctx.Done()` escape. Each of those goroutines is leaked, and so is the closer waiting for them. Either fix on its own is enough to prevent the leak: drain, or let workers escape through `ctx.Done()`. Pool has both, as defence in depth. Revert both changes.

**Checkpoint 12:** `go test -race -count=1 ./...` passes three times in a row. Flaky concurrency tests are bugs.

---

## Phase 13: Benchmarks and GOMAXPROCS (README tools, learning-cycle step 5)

### 13.1 Concept first: benchmarks (`testing.B`, small)

| Syntax | Input | Output | Why you use it |
| --- | --- | --- | --- |
| `func BenchmarkXxx(b *testing.B)` | – | – | Found and run by `go test -bench` |
| `for b.Loop() { … }` (Go 1.24) | – | `bool`: keep looping? | The framework picks the iteration count. Setup before the loop is excluded from timing automatically. |
| `go test -bench=. -benchmem` | `-bench` regex of names; `-benchmem` adds allocations | `ns/op`, `B/op`, `allocs/op` | Throughput and memory per run |
| `-cpu=1,2,4,8` | list of GOMAXPROCS values | each benchmark repeated per value; the name gets a suffix (`-4`) | The README's GOMAXPROCS experiment |
| `-run='^$'` | regex matching no test | – | Skip the unit tests while benchmarking |

### 13.2 `internal/analyzer/bench_test.go`

**What this code does:** it generates 200 files × 1,000 lines once per benchmark (outside the timed loop), then times one full analysis per iteration for each strategy.

```go
package analyzer

import (
	"context"
	"testing"
)

func BenchmarkSequential(b *testing.B) {
	files := testFiles(b, 200, 1000)
	for b.Loop() {
		if _, err := Sequential(files); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLocked(b *testing.B) {
	files := testFiles(b, 200, 1000)
	for b.Loop() {
		if _, err := Locked(files); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkChannel(b *testing.B) {
	files := testFiles(b, 200, 1000)
	for b.Loop() {
		if _, err := Channel(files); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPool(b *testing.B) {
	files := testFiles(b, 200, 1000)
	for b.Loop() {
		if _, err := Pool(context.Background(), files, Options{Workers: 8}); err != nil {
			b.Fatal(err)
		}
	}
}
```

### 13.3 Concept: GOMAXPROCS, concurrency vs parallelism (New · Essential; internals in Part C)

**The problem.** "Concurrent" and "faster" are not the same thing. You need to know what actually limits how much runs at once.

**The core idea.** *Concurrency* is how a program is **structured**: independent tasks that may overlap. *Parallelism* is **execution**: tasks literally running at the same instant on different cores. `GOMAXPROCS` is the maximum number of OS threads that may **execute Go code** at the same time **[API]**. Threads blocked in system calls don't count against it **[API: runtime docs]**. The default is the number of logical CPUs. Since Go 1.25, on Linux it also respects cgroup CPU limits (containers) and is re-checked periodically **[API/Runtime]**. On macOS that doesn't apply.

**Analogy.** GOMAXPROCS is the number of checkout counters open. Goroutines are customers. A customer who walks away to fetch something (a syscall) frees the counter for the next one.

| Way to set it | Effect |
| --- | --- |
| `GOMAXPROCS=2 go run …` (env var) | Set at program start |
| `runtime.GOMAXPROCS(2)` | Change at run time (returns the old value). Rarely right in production code. |
| `go test -cpu=1,2,4` | Benchmarks and tests repeated per value |

### 13.4 Run the benchmarks

```bash
go test -run='^$' -bench=. -benchmem -cpu=1,2,4,8 ./internal/analyzer/
```

One run on this machine (8 logical CPUs, arm64):

| Benchmark | -cpu=1 | -cpu=2 | -cpu=4 | -cpu=8 |
| --- | --- | --- | --- | --- |
| Sequential | 62.7 ms | 62.5 ms | 62.7 ms | 62.7 ms |
| Locked | 31.3 ms | 35.2 ms | 24.5 ms | 38.4 ms |
| Channel | 33.9 ms | 37.8 ms | 25.9 ms | 31.9 ms |
| Pool (8 workers) | 30.3 ms | 35.0 ms | 23.7 ms | 30.2 ms |

Memory: about 30 MB and 603k allocations per op for **every** strategy (≈3 allocations per line).

**Predict first, then read the analysis:**

| Observation | Explanation |
| --- | --- |
| Sequential is unaffected by `-cpu` | One goroutine can use only one thread for Go code |
| **Concurrent versions are about 2× faster even at `-cpu=1`** | Reading files means syscalls. While one goroutine's thread is blocked in `read`, the scheduler hands the P to another thread so other goroutines keep parsing **[Runtime]**. That is concurrency without parallelism: overlapping I/O waits. |
| Gains above 4 are small or negative, and the numbers jump around | The work is short (≈30 ms), so goroutine startup, merging, GC and disk-cache effects dominate. Merging is serialized in the aggregator or mutex (Amdahl's law). Single runs are noisy. |
| Allocations are the same everywhere | Concurrency doesn't change the work done. It changes *when* and *where* it runs. |

**Measure properly:** `-count=10` and compare with `benchstat` (`go install golang.org/x/perf/cmd/benchstat@latest`, optional). Close other apps. The second run benefits from the OS file cache, so the first run is not representative.

**Experiment E5 (README):**

```bash
for p in 1 2 8; do echo "GOMAXPROCS=$p"; GOMAXPROCS=$p go run ./cmd/analyze -mode=pool > /dev/null; done
```

Observed: about 356 ms at 1 against about 100 ms at 8. With the CLI (1,000 files, a cold start) the CPU-bound parsing dominates more than in the benchmark. Note that `-workers` defaults to `runtime.GOMAXPROCS(0)`, so `GOMAXPROCS=1` also means 1 worker.

**Profiling to find out where the time goes:** `go test -run='^$' -bench=Pool -cpuprofile=cpu.out ./internal/analyzer/ && go tool pprof -top cpu.out`. Expect `strings.Fields`, `strconv.Atoi`, map assignment and syscalls near the top.

**Checkpoint 13:** your own benchmark table, plus one sentence explaining why concurrency helps at `-cpu=1` here.

---

# Part B: Track B, advanced Go knowledge

A bounded set of topics. Each builds on something you have already used. The core explanations above are not repeated.

| # | Topic | Scope | Why it is here |
| --- | --- | --- | --- |
| B1 | Memory model and happens-before | Related | Explains *why* WaitGroup, Mutex and channels make the project correct |
| B2 | `sync/atomic` | Related | The lighter-weight alternative to a mutex for single counters |
| B3 | `sync.RWMutex` and lock contention | Related | When a plain Mutex is the wrong tool |
| B4 | `errgroup` | Related | The production-grade form of Pool's error and cancellation handling |
| B5 | Goroutine-leak detection and `testing/synctest` | Advanced | Testing concurrent code without flakiness |

### B1. The Go memory model (Advanced · Related) [Spec]

**Problem:** CPUs and compilers **reorder** memory operations for speed. Without rules, goroutine B could see goroutine A's writes late, partly, or in a different order.

**Core rule:** if write W *happens before* read R, then R observes W (or something later). Happens-before is built from **program order within one goroutine** plus **synchronization edges** between goroutines:

| Edge (A happens before B) | Used in this project |
| --- | --- |
| `go f()` statement → start of `f` | every goroutine sees `path` and `files` |
| send on ch → the corresponding receive completes | worker's `*Stats` writes → aggregator reads them |
| `close(ch)` → a receive that returns because ch is closed | closer → aggregator's `range` ends |
| receive on unbuffered ch → that send completes | – |
| k-th receive on a channel of capacity C → (k+C)-th send completes | this is why a buffered channel works as a **semaphore** |
| n-th `Unlock` → (n+1)-th `Lock` returns | `Locked`'s merges |
| `wg.Done()` → the `wg.Wait()` it unblocks returns | `errs[i]` and `total` read after `Wait` |
| atomic op observed by another atomic op | B2 |

Edges are transitive. In `Locked`: worker's merge → `Unlock` → … → `Done` → `Wait` returns → main reads `total`. **Any shared variable access that you can't connect with such a chain is a race.**

**What races do in Go:** unlike C/C++, Go limits the damage for single-word values (you read *some* value that was written). Races on multi-word values (strings, slices, interfaces, maps) **can corrupt memory and crash** **[Spec: memory model, "implementation restrictions for programs containing data races"]**. "Benign races" don't exist in practice.

**Read next:** https://go.dev/ref/mem (short, and now within your reach).

### B2. `sync/atomic` (Related)

**Problem:** a mutex for one `int` counter is heavyweight. **Idea:** hardware instructions that read-modify-write one word indivisibly. Typed wrappers (Go 1.19+) prevent mixing atomic and non-atomic access to the same variable.

```go
var processed atomic.Int64 // zero value ready; never copy after first use
// in each worker:
processed.Add(1)
// anywhere, any time (e.g. a progress printer):
fmt.Println(processed.Load())
```

| Syntax | Input | Output | Why you use it |
| --- | --- | --- | --- |
| `var x atomic.Int64` | – | counter = 0 | Lock-free shared counter |
| `x.Add(delta int64)` | delta | `int64`: the new value | Increment from many goroutines |
| `x.Load()` | – | `int64` | Read safely while others write |
| `x.CompareAndSwap(old, new int64)` | expected, replacement | `bool` swapped | Lock-free state machines |

**Limits:** atomics protect **one** word. Our `Stats` (several maps plus counters that must stay consistent with each other) needs a mutex or confinement. A progress counter ("files done: 412/1000") is the right job for an atomic. Try adding one to Pool as an exercise and run `-race`.

### B3. `sync.RWMutex` and contention (Related)

`RWMutex` allows **many readers or one writer**. It helps only when reads vastly outnumber writes *and* critical sections are non-trivial (for example a config cache read on every request). Our merges are all writes, so it would be slower here. Contention (many goroutines waiting on one lock) shows up in `go test -bench -mutexprofile=mutex.out` and `go tool pprof`. The usual fixes are the ones you already applied: shorter critical sections, or confinement plus merging (fewer lock acquisitions).

### B4. `golang.org/x/sync/errgroup` (Related)

Pool hand-builds what `errgroup.WithContext` provides: run goroutines, **cancel the shared context on the first error**, `Wait` returns that error. `g.SetLimit(n)` turns it into a bounded pool.

```go
g, ctx := errgroup.WithContext(ctx)
g.SetLimit(workers)
for _, path := range files {
	g.Go(func() error {
		s, err := accesslog.AnalyzeFile(path)
		if err != nil {
			return err // cancels ctx for everyone
		}
		select {
		case results <- s:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
}
```

It is an extension module (`go get golang.org/x/sync`), not the standard library, so it was not used in the main track. With `SetLimit`, `g.Go` **blocks** the loop when the limit is reached. That is a semaphore, not a jobs channel. You would still need a closer goroutine (`g.Wait(); close(results)`) and an aggregator. Rewriting `Pool` with errgroup is a good exercise. Your tests from Phase 12 tell you whether you got it right.

### B5. Leak detection and deterministic concurrency tests (Advanced)

- **`go.uber.org/goleak`** (third-party): `defer goleak.VerifyNone(t)` fails a test if unexpected goroutines remain, and prints their stacks. It is more precise than counting.
- **`testing/synctest`** (standard library since Go 1.25): runs a test in a "bubble" with a **fake clock**. Time advances instantly once every goroutine in the bubble is blocked. `TestPoolCancelled` could use it to test a 1-hour timeout in microseconds, deterministically. `synctest.Wait()` waits until all bubble goroutines are blocked, which also turns "is anything still running?" into a precise question.
- A goroutine-leak *profile* (`GOEXPERIMENT=goroutineleakprofile`) was added as an experiment around Go 1.26. I am not certain of its status in 1.27.1, so check `go doc runtime/pprof` before relying on it.

### Worth learning later (not expanded here)

`sync.Once` / `sync.OnceValue` · `sync.Pool` (reduce those 603k allocations) · `sync.Cond` · semaphores (`golang.org/x/sync/semaphore`) · pipelines with several stages (Project 3) · rate limiting (Project 5) · `context.AfterFunc` / `WithoutCancel` · pprof and execution traces (`go tool trace`) · `bufio.Scanner.Bytes()` for zero-copy parsing · graceful HTTP server shutdown (Project 2).

---

# Part C: How it works under the hood

Everything here is **[Runtime]** unless marked otherwise: accurate for current Go releases, not promised forever.

### C1. Goroutines vs OS threads

| | OS thread | Goroutine |
| --- | --- | --- |
| Created by | the kernel (expensive syscall) | the Go runtime (a function call plus a small allocation) |
| Stack | fixed, often 1–8 MB | starts small (≈2 KB currently) and **grows by copying** when needed |
| Switch cost | kernel context switch (≈µs) | user-space switch of a few registers (≈100 ns) |
| Scheduling | kernel, preemptive | Go scheduler, cooperative at function calls **plus** async preemption |

That is why the 1,000 goroutines of Phase 3 cost about nothing, while 1,000 threads would be noticeable. **[Spec]** guarantees only that goroutines are independent concurrent executions in the same address space. Sizes and costs are implementation facts.

### C2. The G/M/P scheduler

```
   G = goroutine (stack + state)     M = OS thread     P = processor slot (GOMAXPROCS of them)

   ┌──── P0 ─────┐   ┌──── P1 ─────┐        global run queue: [G G G]
   │ local queue │   │ local queue │
   │ [G G G]     │   │ [G]         │        netpoller: Gs waiting on network I/O
   └──────┬──────┘   └──────┬──────┘
          M0 runs G         M1 runs G         M2 blocked in syscall (holds no P)
```

| Mechanism | What happens | Where you saw it |
| --- | --- | --- |
| Run queues | `go f()` puts the new G on the current P's **local queue** (256 slots), overflowing to the global queue | Phase 3: 1,000 Gs queued instantly |
| Work stealing | An idle P steals half of another P's local queue | Balanced load across workers |
| Blocking on a channel or mutex | The G is parked (state `waiting`) **off** any queue; the M picks another G. No OS thread blocks. | E3b: 1,000 parked senders cost only memory |
| Syscall handoff | When a G enters a blocking syscall (a file `read`), its M keeps the G, and the P is handed to another M (the `sysmon` thread retakes Ps stuck in syscalls) | Phase 13: 2× speed-up at `-cpu=1` |
| Preemption | Since Go 1.14, `sysmon` signals a G that has run for about 10 ms, so a tight loop can't hog a P | Fairness between workers |
| GOMAXPROCS | = number of Ps = maximum Ms running Go code at once | Phase 13 |

### C3. Channel internals

A channel is a pointer to a runtime struct (`hchan`): a **lock**, a **ring buffer** (`buf`, `sendx`, `recvx`, `qcount`, `dataqsiz` = capacity), a `closed` flag, and two **wait queues** (`recvq`, `sendq`) of parked goroutines.

| Operation | Fast path | Slow path |
| --- | --- | --- |
| send | If a receiver is waiting in `recvq`: copy the value **directly onto its stack** and wake it (no buffer involved). Else, if there is buffer room: copy into `buf[sendx]`. | Park the sender in `sendq` (Phase 9 E3b) |
| receive | If a sender is waiting: take its value (from the buffer front, then refill from that sender) and wake it. Else, if the buffer is non-empty: copy from `buf[recvx]`. | Park in `recvq` |
| close | Set `closed`, wake **every** waiting receiver (zero values) and every waiting sender (they panic) | – |

That is why `close` can broadcast but a send cannot: close walks the whole `recvq`. Channels are mutex-protected queues with direct handoff. They are not lock-free and not magic, which is why a mutex can beat a channel for simple shared state.

### C4. Mutex internals

`sync.Mutex` is two `int32`s (a state word and a semaphore). `Lock` first tries one atomic compare-and-swap (the uncontended fast path, a few ns), then spins briefly on multicore machines, then parks the goroutine on a runtime semaphore. **Normal mode** lets a newly arriving goroutine barge ahead of woken waiters (better throughput). If a waiter has waited more than 1 ms, the mutex switches to **starvation mode**, where ownership is handed directly to the waiter at the front. None of this ordering is part of the API. The documentation only promises mutual exclusion and the happens-before edge.

### C5. WaitGroup and timers, briefly

A WaitGroup packs its counter and waiter count into one 64-bit atomic word. `Wait` parks on a semaphore, and the final `Done` releases every waiter. Timers (`time.After`) live in per-P heaps inside the runtime. Since Go 1.23 they are unbuffered and collectable when unreferenced **[API]**.

---

# Appendix: Reference dictionary

### Concurrency tools

| Syntax | Input | Output | Why you use it | Watch for |
| --- | --- | --- | --- | --- |
| `go f(x)` | function call | nothing; returns immediately | Run concurrently | Args evaluated now; main exiting kills it; an unrecovered panic kills the program |
| `var wg sync.WaitGroup` | – | counter 0 | Wait for N tasks | Never copy |
| `wg.Add(n)` | delta | nothing | Count tasks before starting them | Positive Add at counter 0 must happen before Wait; negative → panic |
| `wg.Done()` | – | nothing | A task finished | Always `defer` it |
| `wg.Go(f)` | `func()` | nothing | Add + go + Done in one call (1.25+) | `f` must not panic |
| `wg.Wait()` | – | nothing; blocks until 0 | The owner waits | Hangs if a Done is missing |
| `var mu sync.Mutex` | – | unlocked | Guard shared state | Declare next to the state; never copy |
| `mu.Lock()` / `mu.Unlock()` | – | nothing; Lock blocks | Critical section | Not reentrant; Unlock of an unlocked mutex is fatal; keep it short |
| `make(chan T, n)` | type, capacity | `chan T` | Conduit between goroutines | nil channel blocks forever |
| `ch <- v` | value | nothing; may block | Send / hand off ownership | Panics if closed |
| `v, ok := <-ch` | – | value, ok | Receive | `ok=false` → closed and drained |
| `close(ch)` | channel | nothing | "No more values" broadcast | Only the closer that knows sends are over; twice → panic |
| `for v := range ch` | channel | values | Consume until closed | Never ends if nobody closes |
| `chan<- T` / `<-chan T` | – | – | Compiler-enforced roles | A receive-only channel can't be closed |
| `select {…}` | channel cases | runs one ready case | Wait for several operations | Random among ready cases; `default` makes it non-blocking |
| `context.Background()` | – | root ctx | Root in main or tests | Never in library code paths |
| `context.WithCancel(p)` | parent | ctx, cancel | Cancellable child | Always call cancel |
| `context.WithTimeout(p, d)` | parent, duration | ctx, cancel | Deadline | `Err` = DeadlineExceeded |
| `ctx.Done()` / `ctx.Err()` / `context.Cause(ctx)` | – | chan / error / error | Observe cancellation | Cooperative: you must check |
| `signal.NotifyContext(p, os.Interrupt)` | parent, signals | ctx, stop | Ctrl+C → cancel | Ctrl+C no longer kills the process; call stop |
| `time.After(d)` | duration | `<-chan time.Time` | Timeout or delay in select | GC-safe since 1.23 |
| `runtime.NumGoroutine()` | – | int | Leak checks, diagnostics | Includes runtime/library goroutines (e.g. signal handling) |
| `runtime.GOMAXPROCS(0)` | 0 = query | int | Default worker count | Setting it in code is rarely right |
| `go test -race` / `go run -race` | – | race reports, exit 66 | Find data races | Only races that happen during that run |
| `go test -bench=. -benchmem -cpu=…` | – | ns/op, B/op, allocs/op | Measure | Noisy: use `-count` and benchstat |

### Backend and reliability concepts

| Concept | Meaning in this project |
| --- | --- |
| Baseline | The sequential output; every version must reproduce it byte for byte |
| Canonical output | One valid serialization per result: sorted keys, fixed field order, deterministic ties |
| Output contract | stdout = data only; stderr = diagnostics; exit codes 0 / 1 / 130 |
| Fail fast | Cancel all remaining work on the first error (Pool) |
| Graceful cancellation | Stop on a signal, wait for every goroutine, print nothing partial |
| Bounded concurrency | At most N files open at once (worker pool) |

### Vocabulary

| Term | Definition |
| --- | --- |
| Concurrency | Structuring a program as independent tasks that may overlap in time |
| Parallelism | Tasks executing at the same instant on different cores |
| Data race | Unsynchronized concurrent accesses to one memory location, at least one a write |
| Race condition | A correctness bug that depends on timing (broader than a data race) |
| Happens-before | The ordering relation that makes one goroutine's write visible to another's read |
| Critical section | Code that runs while holding a lock |
| Deadlock | Goroutines each waiting for another, so none proceeds |
| Goroutine leak | A goroutine blocked forever (or never told to stop), holding its memory |
| Confinement | Data reachable by only one goroutine, so no synchronization is needed |
| Ownership transfer | Sending a pointer on a channel and never touching it again on the sending side |
| Fan-out / fan-in | Distribute jobs to many workers / gather their results into one stream |
| Worker pool | A fixed set of goroutines consuming a jobs channel |
| Backpressure | A full buffer slowing a producer down to the consumer's pace |
| Cooperative cancellation | A stop *request* that code must notice and act on itself |
| Contention | Goroutines waiting on the same lock |
| Parked goroutine | A goroutine waiting on a channel or lock, using no CPU and no thread |

---

# Final checklist

**Structure**
- [ ] `go.mod` says `module github.com/sidd-dhegaskar/log-analyzer` and `go 1.27.1`
- [ ] `cmd/loggen`, `cmd/analyze` are thin; logic is in `internal/`
- [ ] `logs/` is git-ignored; `baseline.json`, `go.sum` are committed

**Correctness**
- [ ] `sequential`, `locked`, `channel`, `pool` all `cmp` equal to `baseline.json`
- [ ] `go run -race ./cmd/analyze -mode=racy` reports a race (it is meant to)
- [ ] `go test -race -count=1 ./...` passes three times in a row
- [ ] Ctrl+C with `-file_delay=20ms` → exit 130, empty stdout

**Concurrency rules (engineering guide Part 4)**
- [ ] Every goroutine has an owner that waits for it (`wg.Wait`, closer goroutine)
- [ ] Every channel's closer is its sole sender or a post-`Wait` coordinator; receivers never close
- [ ] Every blocking `select` in Pool has a `ctx.Done()` case
- [ ] `ctx` is the first parameter and is never stored in a struct; `cancel` is always called
- [ ] Channel parameters are directional
- [ ] The mutex sits next to the state it guards; the critical section contains no I/O

**Style**
- [ ] `gofmt -s -l .` prints nothing; `go vet ./...` is clean
- [ ] Every exported name has a doc comment starting with its name
- [ ] Errors wrapped only with new context; discarded errors have a comment

**Understanding: answer without looking**
- [ ] Why does `w1.go` usually print only `main done`?
- [ ] Why must `wg.Add` normally be called before `go`?
- [ ] Why is `errs[i] = err` safe but `total.Merge(s)` not?
- [ ] Why does unbuffered + `wg.Wait()` before `range` deadlock, and why did it *hang* instead after Phase 10?
- [ ] Why are concurrent versions faster even with `GOMAXPROCS=1`?
- [ ] When would you choose a mutex over a channel, and vice versa?

Tell me what you built and I will review it against the checkpoint.
