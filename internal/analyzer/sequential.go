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
