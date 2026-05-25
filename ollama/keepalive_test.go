package ollama

import (
	"strings"
	"testing"
	"time"
)

func TestParseKeepAlive(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		in          string
		wantNil     bool
		wantPos     time.Duration
		wantNeg     bool
		wantErrPart string
	}{
		{name: "empty string returns nil", in: "", wantNil: true},
		{name: "whitespace returns nil", in: "  \t ", wantNil: true},
		{name: "minus one sentinel", in: "-1", wantNeg: true},
		{name: "negative duration form", in: "-1s", wantNeg: true},
		{name: "5 minutes", in: "5m", wantPos: 5 * time.Minute},
		{name: "300s", in: "300s", wantPos: 300 * time.Second},
		{name: "compound duration", in: "1h30m", wantPos: 90 * time.Minute},
		{name: "zero with unit is allowed", in: "0s", wantPos: 0},
		{name: "bare zero parses as zero", in: "0", wantPos: 0},
		{name: "garbage rejected", in: "soon", wantErrPart: "invalid keep_alive"},
		{name: "no unit non-zero rejected", in: "5", wantErrPart: "invalid keep_alive"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseKeepAlive(tc.in)
			if tc.wantErrPart != "" {
				if err == nil {
					t.Fatalf("parseKeepAlive(%q) err = nil, want error containing %q", tc.in, tc.wantErrPart)
				}
				if !strings.Contains(err.Error(), tc.wantErrPart) {
					t.Fatalf("parseKeepAlive(%q) err = %q, want substring %q", tc.in, err.Error(), tc.wantErrPart)
				}
				if got != nil {
					t.Errorf("parseKeepAlive(%q) returned non-nil %v on error", tc.in, *got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseKeepAlive(%q) unexpected err: %v", tc.in, err)
			}
			if tc.wantNil {
				if got != nil {
					t.Fatalf("parseKeepAlive(%q) = %v, want nil", tc.in, *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("parseKeepAlive(%q) = nil, want non-nil", tc.in)
			}
			if tc.wantNeg {
				if *got >= 0 {
					t.Errorf("parseKeepAlive(%q) = %v, want negative", tc.in, *got)
				}
				return
			}
			if *got != tc.wantPos {
				t.Errorf("parseKeepAlive(%q) = %v, want %v", tc.in, *got, tc.wantPos)
			}
		})
	}
}
