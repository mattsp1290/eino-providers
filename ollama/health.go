package ollama

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	einoproviders "github.com/mattsp1290/eino-providers"
)

// ErrBackendUnreachable identifies a failed Ollama health probe.
var ErrBackendUnreachable = einoproviders.ErrBackendUnreachable

const healthProbeMaxTimeout = 5 * time.Second

// PingOllama probes an Ollama server at baseURL with GET /api/tags.
func PingOllama(ctx context.Context, baseURL string, client *http.Client) error {
	if client == nil {
		client = &http.Client{}
	}
	probeURL, displayURL, err := buildProbeURL(baseURL)
	if err != nil {
		return fmt.Errorf("ollama: parse base URL %s: %w", displayURL, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return fmt.Errorf("ollama: build ping request for %s: %w", displayURL, err)
	}
	resp, err := client.Do(req) //nolint:gosec // baseURL is operator configuration, not user input.
	if err != nil {
		return fmt.Errorf("ollama: ping %s: %w", displayURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ollama: ping %s returned status %d", displayURL, resp.StatusCode)
	}
	return nil
}

func pingWithCappedTimeout(parent context.Context, baseURL string, client *http.Client, readTimeout time.Duration) error {
	probeTimeout := healthProbeMaxTimeout
	if readTimeout > 0 && readTimeout < probeTimeout {
		probeTimeout = readTimeout
	}
	ctx, cancel := context.WithTimeout(parent, probeTimeout)
	defer cancel()
	if err := PingOllama(ctx, baseURL, client); err != nil {
		return fmt.Errorf("%w: %w", ErrBackendUnreachable, err)
	}
	return nil
}

func buildProbeURL(baseURL string) (probe string, display string, err error) {
	if baseURL == "" {
		return "", "", fmt.Errorf("base_url is empty")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", "<unparseable>", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		display = redactedURL(u)
		return "", display, fmt.Errorf("scheme must be http or https (got %q)", u.Scheme)
	}
	if u.Host == "" {
		display = redactedURL(u)
		return "", display, fmt.Errorf("host is empty")
	}
	probeU := *u
	probeU.User = nil
	probeU.RawQuery = ""
	probeU.Fragment = ""
	probeU.Path = strings.TrimRight(probeU.Path, "/") + "/api/tags"
	return probeU.String(), redactedURL(u), nil
}

func redactedURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	clone := *u
	clone.User = nil
	return clone.String()
}
