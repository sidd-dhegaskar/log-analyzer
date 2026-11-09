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
	Endpoint  string //"method path"
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
