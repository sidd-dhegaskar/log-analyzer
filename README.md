# Project 1: Log Analyzer

**Level:** Beginner · **Time:** 2–3 hours · **Suggested order:** 1st

## Problem statement

You have a directory containing 1,000 web-server access-log files. Each line looks like:

```
2026-11-03T10:00:01Z GET /api/users 200 123ms
```

For all files together, produce a report with:

- Requests per HTTP status code.
- The top 10 endpoints by request count.
- Average latency.

Print the report as JSON.

Implement it sequentially first, then make it concurrent. Every concurrent version must print exactly the same report as the sequential one.

## Architecture

```
                 Log Files
                     |
          ┌──────────┼──────────┐
          |          |          |
       Worker 1   Worker 2   Worker 3
          |          |          |
          └──────────┼──────────┘
                     |
              Results Channel
                     |
                Aggregator
                     |
                JSON Report
```

## Concepts you will learn

| Concept | How you'll use it |
| --- | --- |
| Goroutines | Parse files concurrently |
| WaitGroup | Wait for all workers |
| Mutex | Protect shared counters |
| Channels | Send per-file results |
| Buffered channels | Buffer completed results |
| Directional channels | Restrict who can send and who can receive |
| Worker pool | Bound the number of concurrent files |
| Select | Handle results and cancellation |
| GOMAXPROCS | Experiment with CPU parallelism |
| Confinement | Keep worker-local statistics isolated |

**Backend theme (about 30 min):** parse text lines, produce JSON output with `encoding/json`.

## Implementation milestones

- [ ] 1. Write a log generator that creates 1,000 files.
- [ ] 2. Write a sequential analyzer and save its output as the baseline.
- [ ] 3. Create a goroutine for every file.
- [ ] 4. Use `sync.WaitGroup` to wait for completion.
- [ ] 5. Introduce shared maps for the counters.
- [ ] 6. Use `sync.Mutex` to protect the shared maps.
- [ ] 7. Refactor to send per-file results through a channel instead of sharing the maps.
- [ ] 8. Replace goroutine-per-file with a fixed worker pool.
- [ ] 9. Introduce cancellation (Ctrl+C) using a done channel or `context`.
- [ ] 10. Print the report as JSON.

## Experiments (break things on purpose)

- [ ] Remove the mutex. Run with `go run -race .` and observe the race conditions.
- [ ] Remove `wg.Wait()`. What happens to the final report?
- [ ] Make the result channel unbuffered. Observe blocking.
- [ ] Close the results channel before all workers finish. What happens?
- [ ] Vary `GOMAXPROCS` (1, 2, N cores) and compare timings.

## 5-step learning cycle

1. **Sequential version** – solve the problem without concurrency.
2. **Introduce concurrency** – find the independent operations.
3. **Break things** – deadlocks, races, blocked channels, leaks.
4. **Make it production-ready** – cancellation, error handling, graceful shutdown.
5. **Benchmark** – sequential vs concurrent: throughput, latency, CPU, memory.

## Tools

- `go run -race .` / `go test -race ./...`
- `go test -bench=.`
- `runtime.NumGoroutine()`

## What you should understand after this project

When to use goroutines, channels, mutexes and WaitGroups, and the difference between shared-memory concurrency and communication-based concurrency.
