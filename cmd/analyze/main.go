// Command analyze prints a JSON report for a directory of access logs.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"
	"runtime"
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
	case "channel":
		stats, err = analyzer.Channel(files)
	case "locked":
		stats, err = analyzer.Locked(files)
	case "racy":
		stats,err=analyzer.Racy(files)
	case "sequential":
		stats, err = analyzer.Sequential(files)
	default:
		return fmt.Errorf("unknown -mode %q", mode)
	}
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "mode=%s files=%d elapsed=%v goroutines=%d\n", mode, len(files), time.Since(start), runtime.NumGoroutine())
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(stats.Report(top))
}
