package ollama

import (
	"fmt"
	"strings"
	"time"
)

// parseKeepAlive parses an Ollama keep_alive string into the pointer shape the
// Eino adapter expects.
func parseKeepAlive(s string) (*time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	if s == "-1" {
		d := time.Duration(-1)
		return &d, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return nil, fmt.Errorf("ollama: invalid keep_alive %q: %w", s, err)
	}
	return &d, nil
}
