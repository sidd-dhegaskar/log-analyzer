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
