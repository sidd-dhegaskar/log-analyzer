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
