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
