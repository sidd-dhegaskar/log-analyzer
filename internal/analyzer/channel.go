package analyzer

import (
	"fmt"
	"github.com/sidd-dhegaskar/log-analyzer/internal/accesslog"
	"sync"
)

type result struct {
	path  string
	stats *accesslog.Stats
	err   error
}

func analyzeTo(path string, out chan<- result) {
	s, err := accesslog.AnalyzeFile(path)
	out <- result{path: path, stats: s, err: err}
}

func aggregate(in <-chan result) (*accesslog.Stats, error) {
	total := accesslog.NewStats()
	var firstErr error
	for r := range in {
		if r.err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("Analyze %s:%w", r.path, r.err)
			}
			continue
		}
		total.Merge(r.stats)
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return total, nil
}

func Channel(files []string) (*accesslog.Stats, error) {
	results := make(chan result, len(files))

	var wg sync.WaitGroup
	for _, path := range files {
		wg.Go(func() {
			analyzeTo(path, results)
		})
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	return aggregate(results)
}