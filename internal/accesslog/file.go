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
