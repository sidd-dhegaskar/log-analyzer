# Phase 8 (build-spec format): bound concurrency with a worker pool

Written for: you, mid-project (Phases 0–7 done, `Channel` works), who learns best from small concept lessons and wants to write the code yourself from a contract instead of copying it.

**Time box: about 75 minutes.** Read concepts 20 · warm-up 10 · build B1–B5 35 · predict, verify and answer questions 10. If a build step takes more than 15 minutes, open the next hint level. That is what the hints are for.

**What changed from the main guide:** the explanation is kept (Sections 1–3). The "type this code" part is replaced by contracts, a test file that defines "done", predictions, and a hint ladder. The reference solution is at the very end, closed. `result`, `aggregate`, `AnalyzeFile` and `Sequential` already exist, so you reuse them and write none of the parsing or stats again.

| How to use this page | |
| --- | --- |
| Contract | Name, signature, behaviour. Never the body. |
| Why | Who calls it and what breaks without it. |
| Done when | A command and the result you should see. |
| Hint ladder | L1 nudge toward the concept · L2 shape of the approach · L3 skeleton. Open one level at a time, after you have really tried. L4 is the full solution, last. |

---

## 1. Why this phase: see the problem first (5 min)

`Channel` starts one goroutine per file. With 1,000 files that is 1,000 goroutines and up to 1,000 open files at once.

```bash
ulimit -n        # your per-process open-file limit
```

**Predict (write it down):** if the input were 5 million files instead of 1,000, which resource runs out first: goroutine memory, open-file limit, or CPU? Which of those does adding more goroutines actually help?

Goroutines are cheap. The **resources they hold** (file descriptors, memory, connections) are not. A worker pool puts a hard cap on how many jobs are in flight.

---

## 2. Concepts (read before building)

Already taught, so only reminders here: channels, close/range, directional types, the closer goroutine and `WaitGroup` (main guide §7.1–7.4). What is new in this phase: the **pool pattern**, the **cascade of closes**, **draining on error**, and **choosing N**.

### 2.1 Pattern: worker pool, taught independently (New · Essential)

**Problem:** process many independent jobs with **at most N running at once**.

**Architecture and data flow:**

```
               jobs (unbuffered)                 results (buffered)
producer ──▶ ─────────────────── ──▶ worker 1 ──┐
  (sole sender,                  ──▶ worker 2 ──┼──▶ ─────────── ──▶ aggregator
   closes jobs)                  ──▶ worker N ──┘          ▲
                                                           │ close after wg.Wait()
                                                     closer goroutine
```

**Analogy:** a bank with N tellers and one queue. Customers (jobs) join the queue, any free teller takes the next one, and the bank never has more than N customers being served however long the queue is.

**Primitives:** goroutines (producer, N workers, closer), two channels, one WaitGroup.

**Lifecycle: shutdown is a cascade of closes [Convention]:**

| # | Event | Consequence |
| --- | --- | --- |
| 1 | Producer sends every path, then `close(jobs)` | Each worker's `range jobs` ends once the remaining jobs are taken |
| 2 | Each worker returns | WaitGroup counter reaches 0 |
| 3 | Closer: `wg.Wait()` returns, then `close(results)` | Aggregator's `range results` ends |
| 4 | Aggregator returns the total | Every goroutine has exited: no leaks |

Each stage finishes when its **input** is closed, then closes its own **output**. No stage needs to know how many items there were.

**Normal, partial and total failure:**

| Scenario | What the pool does | Acceptable? |
| --- | --- | --- |
| Normal | Cascade completes, report = baseline | yes |
| One file slow | Its worker is busy longer; the other N−1 keep draining `jobs` | yes |
| One file fails | Error travels as a `result`; the worker **keeps going**; `aggregate` records the first error and keeps draining | yes, but the other files are still processed for nothing (fixed in Phase 10) |
| All files fail | Every error flows through; the first is returned | yes, wasteful |
| Caller wants to stop (Ctrl+C) | **Nothing can stop it** | no, that is Phase 10 |
| A worker hangs forever | `wg.Wait` never returns, so `Pool` hangs | needs cancellation plus a context-aware `AnalyzeFile` (Phase 10) |

**Common incorrect implementations (you will meet these in the tests):**

| Bug | Why it fails |
| --- | --- |
| A worker closes `results` when it finishes | The first worker to finish closes it; the others then **panic: send on closed channel** |
| Producer never closes `jobs` | `range jobs` never ends, so `wg.Wait` never returns, so `results` is never closed, so the aggregator hangs |
| `wg.Wait()` called in the same goroutine that later ranges over `results` | Workers block sending once `results` is full while you block in `Wait`: a cycle (deadlock) |
| Worker returns on the first error | It stops draining `jobs`; if all workers do, the producer blocks forever (leak) |
| Starting a goroutine per file and "limiting" with a semaphore inside it | Still creates every goroutine: it bounds work, not goroutine count |
| Workers < 1 | 0: no one reads `jobs`. Negative: `make(chan, n)` panics |

**Buffer decisions:**

| Channel | Choice | Reasoning to be able to give |
| --- | --- | --- |
| `jobs` | unbuffered | The producer hands out one path only when a worker is free. A buffer would just hold paths in memory for no gain. |
| `results` | small (`workers`) | Lets each worker drop one result and carry on while the aggregator is busy merging, and bounds memory. A buffer is never a fix for a missing receiver; it only delays the deadlock. |

**Choosing N [Convention]:** CPU-bound work (parsing): about the number of cores. Resource-bound work: the resource's limit (connection pool size, file descriptor budget, an API's rate limit). For error handling plus cancellation together, see `errgroup` (Track B4 in the main guide).

### 2.2 `runtime.GOMAXPROCS(0)` and `runtime.NumGoroutine()` (Familiar package, new calls)

| Syntax | Input | Output | Why you use it |
| --- | --- | --- | --- |
| `runtime.GOMAXPROCS(n int)` | n < 1 means "don't change, just report" | `int`: the **previous** setting | `GOMAXPROCS(0)` reads how many OS threads may run Go code at once (default: logical CPU count) **[API]**; use it as the default worker count |
| `runtime.NumGoroutine()` | nothing | `int`: goroutines that currently exist | A **snapshot**, not an exact figure: **[Runtime]** it can be briefly wrong while goroutines are being created and exited quickly. I hit this while writing the tests (see Section 5, Q7). |

---

## 3. Warm-up (10 min): the pool in isolation

File: `~/Desktop/personal/warmups/pool.go`. **Contract, no code given:**

| Requirement | Detail |
| --- | --- |
| Jobs | The integers 1 to 9 |
| Workers | Exactly 3 goroutines, each squaring what it receives |
| Output | The sum of all squares, printed once. It must be **285** every run. |
| Rules | Exactly one goroutine closes each channel; no `time.Sleep`; use directional channel types in the helper signatures |

**Predict, then try each and revert:**

| Change | Your prediction | Observe |
| --- | --- | --- |
| Remove the `close` of the jobs channel | | |
| Let each worker `close(results)` when it finishes | | |
| Call `wg.Wait()` before the loop that reads results, results unbuffered | | |

---

## 4. Build spec

Reuse (do not rewrite): `result`, `aggregate` (in `channel.go`), `accesslog.AnalyzeFile`, `Sequential`.

### B1. The entry point, top-down (10 min)

**Why:** build from the caller. `main` wants `-mode=pool -workers=N`. Until `Pool` exists in some form, nothing compiles, and the test file in Section 4.1 won't build either.

**Contract:**

| Item | Spec |
| --- | --- |
| `type Options struct` | One field, `Workers int`. A struct, not a bare parameter, so Phase 10 can add a field without touching every caller (engineering guide §3.2). |
| `Pool(files []string, opts Options) (*accesslog.Stats, error)` | In `internal/analyzer/pool.go`. For now it may return an error saying "not implemented". |
| `main.go` | New flag `-workers` (default `runtime.GOMAXPROCS(0)`); thread it into `run`; new `case "pool"`. Update the `-mode` usage string. |

**Done when:** `go build ./... && go vet ./...` pass, and `go run ./cmd/analyze -mode=pool` prints your "not implemented" error.

### B2. `produce`: feed the jobs (5 min)

**Why:** workers need something to take from. If `Pool` itself pushed paths into an unbuffered channel before any worker existed, it would block forever. So the feeder is its own goroutine. (Q1 asks you to explain this precisely.)

**Contract:** `produce(files []string, jobs chan<- string)`. Sends every path on `jobs`, one at a time, then closes `jobs`. It is the **only** sender. It never reads.

**Done when:** you can say, without looking, who closes `jobs`, when, and why that goroutine is allowed to. (Machine-checked in B4.)

### B3. `work`: one worker's life (5 min)

**Why:** one function = one job. The directional types make the ownership table compiler-checked.

**Contract:** `work(jobs <-chan string, results chan<- result)`. Takes paths until `jobs` is closed and drained. For each path, analyzes it and sends a `result` containing path, stats and error. **It never stops early because of an error** (see the failure table in 2.1). It does not close anything.

**Done when:** it compiles with exactly those parameter types.

### B4. Assemble `Pool` (10 min)

**Why:** this is where the lifecycle is decided: who starts what, who waits, who closes.

**Contract:**

| Step | Requirement |
| --- | --- |
| 1 | Workers below 1 are treated as 1 |
| 2 | Create `jobs` and `results`. **Decide their capacities** and put a one-line reason in a comment. |
| 3 | Start the producer |
| 4 | Start **exactly** `workers` workers |
| 5 | Start something that closes `results` at the right moment |
| 6 | Return what `aggregate(results)` returns |
| Constraint | `Pool` starts at most `workers + 2` goroutines of its own (producer, closer, workers) |

**Fill this in before looking at any hint (the ownership table):**

| Resource | Created by | Sends | Receives | Closes | Ends when |
| --- | --- | --- | --- | --- | --- |
| `jobs` | | | | | |
| `results` | | | | | |
| worker goroutines | | | | | |
| closer goroutine | | | | | |
| total `Stats` | | | | | |

**Done when:** `go test -race -count=1 ./internal/analyzer/` is green (Section 4.1).

### B5. Verify against your baseline (5 min)

```bash
go test -race -count=20 ./internal/analyzer/
go run ./cmd/analyze -mode=pool | cmp - baseline.json && echo SAME
go run -race ./cmd/analyze -mode=pool | cmp - baseline.json && echo SAME
for w in 1 2 4 8 16; do go run ./cmd/analyze -mode=pool -workers=$w > /dev/null; done
```

Record `elapsed` for each `w`:

| w | 1 | 2 | 4 | 8 | 16 |
| --- | --- | --- | --- | --- | --- |
| elapsed | | | | | |

For comparison, on this 8-CPU Mac with your 1,000 files and baseline (warm cache): sequential ≈ 200 ms, `channel` ≈ 47 ms, pool w=1 ≈ 105, w=2 ≈ 57, w=4 ≈ 37, w=8 ≈ 36, w=16 ≈ 41. Yours will differ; the **shape** is what matters: speed-up flattens near the core count, and more workers than cores adds no speed.

---

### 4.1 The test file: your definition of "done"

Save as `internal/analyzer/pool_test.go` (paste it; it is a measuring instrument, not the lesson). **Do not edit a test to make it pass.** If you believe a test is wrong, write down why first.

| Test | What it catches |
| --- | --- |
| `TestPoolMatchesSequential` | Wrong or missing results, for 1, 2, 4, 8 and 100 workers (more workers than files) |
| `TestPoolZeroOrNegativeWorkersStillFinishes` | Missing clamp |
| `TestPoolNoFiles` | Hang or nil result on empty input |
| `TestPoolReportsErrorAndDoesNotHang` | Worker stopping on error; error not wrapped with `%w`; leaked or deadlocked shutdown. Fails after 5 s instead of hanging forever. |
| `TestPoolLeavesNoGoroutinesBehind` | Goroutines still alive after both success and error runs |
| `TestPoolBoundsGoroutines` | Still one goroutine per file. Probabilistic by nature (see Q7). |

```go
package analyzer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/sidd-dhegaskar/log-analyzer/internal/accesslog"
)

// writeLogs creates nFiles log files with deterministic content (a few
// malformed lines included) and returns their paths.
func writeLogs(t *testing.T, nFiles, linesPerFile int) []string {
	t.Helper()
	dir := t.TempDir()
	endpoints := []string{"GET /api/users", "POST /api/orders", "GET /health", "GET /api/items"}
	statuses := []int{200, 200, 200, 404, 500}
	var paths []string
	for f := range nFiles {
		var b []byte
		for l := range linesPerFile {
			n := f*linesPerFile + l
			if n%97 == 0 {
				b = append(b, "this is not a log line\n"...)
				continue
			}
			ep := endpoints[n%len(endpoints)]
			b = fmt.Appendf(b, "2026-11-03T10:00:01Z %s %d %dms\n", ep, statuses[n%len(statuses)], 10+n%200)
		}
		p := filepath.Join(dir, fmt.Sprintf("access-%04d.log", f))
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	return paths
}

// within fails the test (instead of hanging the whole suite) if fn does not
// return in time. A hang is the classic symptom of a worker-pool bug.
func within[T any](t *testing.T, d time.Duration, fn func() T) T {
	t.Helper()
	done := make(chan T, 1)
	go func() { done <- fn() }()
	select {
	case v := <-done:
		return v
	case <-time.After(d):
		t.Fatalf("did not finish within %v: probably a deadlock or a channel that is never closed", d)
		panic("unreachable")
	}
}

type poolOut struct {
	stats *accesslog.Stats
	err   error
}

func runPool(t *testing.T, files []string, workers int) poolOut {
	t.Helper()
	return within(t, 5*time.Second, func() poolOut {
		s, err := Pool(files, Options{Workers: workers})
		return poolOut{s, err}
	})
}

func TestPoolMatchesSequential(t *testing.T) {
	files := writeLogs(t, 40, 500)
	want, err := Sequential(files)
	if err != nil {
		t.Fatal(err)
	}
	for _, workers := range []int{1, 2, 4, 8, 100} { // 100 > number of files
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			got := runPool(t, files, workers)
			if got.err != nil {
				t.Fatalf("unexpected error: %v", got.err)
			}
			if !reflect.DeepEqual(got.stats, want) {
				t.Errorf("Pool result differs from Sequential\n got: %+v\nwant: %+v", got.stats, want)
			}
		})
	}
}

func TestPoolZeroOrNegativeWorkersStillFinishes(t *testing.T) {
	files := writeLogs(t, 5, 50)
	want, _ := Sequential(files)
	for _, workers := range []int{0, -3} {
		got := runPool(t, files, workers)
		if got.err != nil || !reflect.DeepEqual(got.stats, want) {
			t.Errorf("workers=%d: err=%v, stats equal=%v", workers, got.err, reflect.DeepEqual(got.stats, want))
		}
	}
}

func TestPoolNoFiles(t *testing.T) {
	got := runPool(t, nil, 4)
	if got.err != nil {
		t.Fatalf("unexpected error: %v", got.err)
	}
	if got.stats == nil || got.stats.Requests != 0 {
		t.Errorf("want empty non-nil stats, got %+v", got.stats)
	}
}

func TestPoolReportsErrorAndDoesNotHang(t *testing.T) {
	files := writeLogs(t, 30, 100)
	files[7] = filepath.Join(filepath.Dir(files[7]), "missing.log")
	got := runPool(t, files, 4)
	if got.err == nil {
		t.Fatal("want an error for the missing file, got nil")
	}
	if !errors.Is(got.err, fs.ErrNotExist) {
		t.Errorf("error should wrap fs.ErrNotExist (use %%w), got: %v", got.err)
	}
	if got.stats != nil {
		t.Errorf("on error the stats should be nil, got %+v", got.stats)
	}
}

// goroutinesSettle waits for the goroutine count to fall back to want.
func goroutinesSettle(want int) (int, bool) {
	var n int
	for range 200 {
		n = runtime.NumGoroutine()
		if n <= want {
			return n, true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return n, false
}

func TestPoolLeavesNoGoroutinesBehind(t *testing.T) {
	files := writeLogs(t, 30, 100)
	bad := append([]string{filepath.Join(t.TempDir(), "missing.log")}, files...)
	before := runtime.NumGoroutine()
	for range 5 {
		runPool(t, files, 4) // success path
		runPool(t, bad, 4)   // error path: workers must still drain and exit
	}
	if n, ok := goroutinesSettle(before); !ok {
		t.Errorf("goroutine leak: %d before, %d after", before, n)
	}
}

// TestPoolBoundsGoroutines samples the goroutine count while Pool runs.
// runtime.NumGoroutine is only a snapshot and can be briefly off when
// goroutines come and go quickly, so one high sample proves nothing. A pool
// that really starts one goroutine per file stays above the limit for most
// of the run, so the test fails only if many samples are above it.
func TestPoolBoundsGoroutines(t *testing.T) {
	const workers = 4
	files := writeLogs(t, 300, 300)
	before := runtime.NumGoroutine()
	// allowed: sampler (1) + the helper goroutine that calls Pool (1) +
	// producer (1) + closer (1) + the workers
	limit := before + 4 + workers

	var over, total, peak int // written only by the sampler goroutine
	stop := make(chan struct{})
	sampled := make(chan struct{})
	go func() {
		defer close(sampled)
		for {
			select {
			case <-stop:
				return
			default:
				n := runtime.NumGoroutine()
				total++
				if n > limit {
					over++
				}
				peak = max(peak, n)
				runtime.Gosched()
			}
		}
	}()
	got := runPool(t, files, workers)
	close(stop)
	<-sampled // also makes the sampler's writes visible to this goroutine
	if got.err != nil {
		t.Fatal(got.err)
	}
	if over*5 > total { // more than 20% of samples above the limit
		t.Errorf("%d of %d samples above %d goroutines (peak %d; before=%d, workers=%d): is it still one goroutine per file?",
			over, total, limit, peak, before, workers)
	}
}
```

Before B4 the package won't compile (no `Pool`): that is the "red" state. After B1 it compiles but fails; after B4 it should go green.

---

## 5. Predict, then run (critical thinking)

Answer in your own notes **before** each run. Answers are in the Answers block at the end.

| # | Question |
| --- | --- |
| Q1 | Why must `produce` run in its own goroutine? What exactly happens if `Pool` calls it directly, before starting the workers? |
| Q2 | If `jobs` had capacity `len(files)`, what would change: correctness, memory, speed? |
| Q3 | `results` capacity 0, `workers`, `len(files)`: which are correct? What does each cost? |
| Q4 | File #3 fails. What does each worker do next? Is it acceptable that the other 997 files are still processed? |
| Q5 | `workers` is 100 and there are 40 files. How many workers do real work? What do the other 60 do, and do they exit? |
| Q6 | List what would have to change for Ctrl+C to stop the pool early. Don't build it (Phase 10). Who needs to learn about the stop, and how? |
| Q7 | Why is `TestPoolBoundsGoroutines` probabilistic? Why does it count samples instead of checking the peak? |
| Q8 | Optional: in my timings w=1 (≈105 ms) beat sequential (≈200 ms). With one worker, what still runs in parallel with it? (My hypothesis; I have not proven it. How would you test it?) |

---

## 6. Break it on purpose (after the tests are green)

One change at a time: predict which test fails and how, run `go test -count=1 ./internal/analyzer/`, then revert.

| # | Change | Verified behaviour on go1.27.1 |
| --- | --- | --- |
| E1 | Remove the `close` of `jobs` | `TestPoolMatchesSequential` times out: "did not finish within 5s" |
| E2 | Call the producer without `go` | Times out with a deadlock (`TestPoolReportsErrorAndDoesNotHang` reports it) |
| E3 | Replace the closer goroutine with a plain `wg.Wait(); close(results)` before `aggregate` | Same kind of timeout. Compare with the Phase 9 E3c experiment in the main guide. |
| E4 | Clamp to 0 instead of 1 (`max(w, 0)`) | `Pool` returns empty stats and **no error**. The tests fail on a wrong result, not a hang. A silent wrong answer is worse than a crash. |
| E5 | Remove the clamp entirely | `panic: makechan: size out of range` for a negative worker count |
| E6 | Make `work` return when it sees an error | `TestPoolReportsErrorAndDoesNotHang` fails: the workers stop draining |
| E7 | Start `len(files)` workers instead of `workers` | `TestPoolBoundsGoroutines` fails (about 300 goroutines against a limit near 10) |

---

## 7. Checkpoint 8

- [ ] `go test -race -count=20 ./internal/analyzer/` passes
- [ ] `-mode=pool` output `cmp`s equal to `baseline.json`, with and without `-race`
- [ ] Timing table filled in for w = 1, 2, 4, 8, 16
- [ ] Ownership table filled in from memory
- [ ] You answered Q1–Q7 before reading the answers
- [ ] You tried at least E1, E4 and E6 and can explain each failure

**Tell me what you built** (paste `pool.go` and your answers) and I will review it in review mode.

---

## Hints (open one level at a time)

<details>
<summary>B1 hints</summary>

- **L1:** What two things does a caller of `Pool` have to supply? Look at how the test calls it.
- **L2:** `flag.Int` returns a `*int`. `run` currently takes `(dir, mode string, top int)`; add one parameter, and pass `*workers` from `main`.
- **L3:** The stub body is one line returning `nil` and `errors.New("pool: not implemented")`.

</details>

<details>
<summary>B2 hints</summary>

- **L1:** Only one goroutine sends on `jobs`. Who is therefore the right one to close it, and when does it know it is finished?
- **L2:** A loop over `files` that sends each path. Make the `close` impossible to forget.
- **L3:** First line is `defer close(jobs)`, then a single `for` loop with one send.

</details>

<details>
<summary>B3 hints</summary>

- **L1:** What ends a `for ... range` over a channel?
- **L2:** The loop body is two lines: analyze the path, send a `result` with all three fields. No `return` on error.
- **L3:** `for path := range jobs { ... }`, using `accesslog.AnalyzeFile` and `results <- result{...}`.

</details>

<details>
<summary>B4 hints</summary>

- **L1:** Draw the diagram. For each channel mark its senders and who knows the last send has happened. Reuse the closer pattern from Phase 7; what is new is the second channel.
- **L2:** The closer waits for the workers, so it needs the same WaitGroup the workers are counted in. `aggregate` must run on the calling goroutine.
- **L3 (order of statements):** clamp workers, make `jobs`, make `results`, `go produce(...)`, WaitGroup and a loop starting workers, `go` closer, `return aggregate(results)`.

</details>

<details>
<summary>Answers to Q1–Q8</summary>

- **Q1:** `jobs` is unbuffered, so every send waits for a receiver. If `Pool` called `produce` itself before starting workers, the first send would wait for a worker that does not exist yet, so `Pool` blocks forever. (Starting the workers first would also work, but only until the workers block on a full `results` while nobody aggregates.)
- **Q2:** Correctness is unchanged. Memory: all paths are held in the channel (small here). Speed: no real change; the producer just finishes sooner. It also stops the producer being throttled by worker availability, which matters if producing is expensive.
- **Q3:** All three are correct if `aggregate` drains and the closer exists. Capacity 0: workers wait for the aggregator after every file. `workers`: each can drop one result. `len(files)`: nobody ever waits, at the cost of holding every result in memory at once.
- **Q4:** The failing worker sends the error as a `result` and carries on. The others never notice. The other 997 are processed for nothing; stopping them is exactly what cancellation (Phase 10) is for.
- **Q5:** At most 40 workers get any work (one file each at best; depending on timing, some of those may get none). All the others block on `range jobs` until it closes, then exit. Surplus workers cost a little memory but do not break correctness.
- **Q6:** The producer must stop sending, the workers must stop taking, and `AnalyzeFile` should notice mid-file. Something both can wait on needs to be passed in (a `context.Context`), `select` replaces the plain send/receive so a blocked goroutine can also see "stop", and the closer/aggregator must still shut down cleanly. A worker busy in a single file only notices at its next check.
- **Q7:** `runtime.NumGoroutine` is a snapshot [Runtime]. Under heavy load (I ran 6–8 copies of the test at once) I saw single samples of 34–36 goroutines even for a correct pool that creates 10. A pool that really makes one goroutine per file stays high for most of the run, so the test requires more than 20% of samples to be over the limit. One high sample is noise; a sustained one is a bug.
- **Q8:** Hypothesis: the producer, the aggregator (merging) and the single worker run on different cores, so reading and parsing overlap with merging. Test it by making the merge artificially slow, or by comparing against `GOMAXPROCS=1`. Treat it as a guess until measured.

</details>

<details>
<summary>L4: reference solution (open last)</summary>

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
	jobs := make(chan string)             // unbuffered: hand out one path at a time
	results := make(chan result, workers) // small buffer: a worker can drop its result and move on

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

Ownership table, filled in:

| Resource | Created by | Sends | Receives | Closes | Ends when |
| --- | --- | --- | --- | --- | --- |
| `jobs` | `Pool` | `produce` (`chan<-`) | workers (`<-chan`) | `produce`, after the last path | closed and drained |
| `results` | `Pool` | workers (`chan<-`) | `aggregate` (`<-chan`) | closer goroutine, after `wg.Wait()` | closed and drained |
| worker goroutines | `Pool` via `wg.Go` | – | – | – | when `jobs` is closed and empty |
| closer goroutine | `Pool` | – | – | `results` | right after `close` |
| total `Stats` | `aggregate` | – | – | – | returned to the caller (confined: only the aggregator touches it) |

This is the code from the main guide (§8.3). I compiled it against your repo layout, and it passes the tests above with `-race` and produces your `baseline.json` exactly.

</details>
