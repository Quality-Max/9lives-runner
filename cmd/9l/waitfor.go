package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// waitUsageError is a --wait-for target that cannot be probed at all, such
// as a malformed URL; it is a usage error, not an environment one.
type waitUsageError struct{ error }

// waitForTargets probes each target in turn until all are ready or the
// shared timeout passes. A target is an http(s) URL, ready when it answers
// with a status below 500 other than 429, or a host:port, ready when it
// accepts a TCP connection. The error names the target without credentials
// or query and the last reason it was not ready.
func waitForTargets(ctx context.Context, targets []string, timeout time.Duration) error {
	probes := make([]func(context.Context) error, 0, len(targets))
	names := make([]string, 0, len(targets))
	for _, target := range targets {
		probe, name, err := targetProbe(target)
		if err != nil {
			return waitUsageError{err}
		}
		probes, names = append(probes, probe), append(names, name)
	}
	deadline := time.Now().Add(timeout)
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	for i, probe := range probes {
		var last error
		for {
			if last = probe(ctx); last == nil {
				break
			}
			if ctx.Err() != nil || !time.Now().Add(500*time.Millisecond).Before(deadline) {
				return fmt.Errorf("%s was not ready after %s: %s", names[i], timeout.Round(time.Second), waitReason(last))
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("%s was not ready: %s", names[i], waitReason(last))
			case <-time.After(500 * time.Millisecond):
			}
		}
	}
	return nil
}

// targetProbe returns one readiness check and the target's display name,
// which never carries user info or a query string.
func targetProbe(target string) (func(context.Context) error, string, error) {
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		parsed, err := url.Parse(target)
		if err != nil || parsed.Host == "" {
			return nil, "", errors.New("an http(s) target needs a host")
		}
		display := &url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: parsed.Path}
		client := &http.Client{Timeout: 5 * time.Second}
		return func(ctx context.Context) error {
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
			if err != nil {
				return err
			}
			response, err := client.Do(request)
			if err != nil {
				return err
			}
			response.Body.Close()
			if response.StatusCode >= 500 || response.StatusCode == http.StatusTooManyRequests {
				return fmt.Errorf("HTTP %d", response.StatusCode)
			}
			return nil
		}, display.String(), nil
	}
	host, port, err := net.SplitHostPort(target)
	if number, convErr := strconv.Atoi(port); err != nil || convErr != nil || host == "" || number < 1 || number > 65535 {
		return nil, "", errors.New("a target is an http(s) URL or host:port")
	}
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	return func(ctx context.Context) error {
		conn, err := dialer.DialContext(ctx, "tcp", target)
		if err != nil {
			return err
		}
		return conn.Close()
	}, target, nil
}

// waitReason is the structured part of a probe error: the HTTP status, the
// network error's operation and code, or a timeout. URLs inside the error
// are dropped so a credential in a target never reaches output.
func waitReason(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Err != nil {
		return opErr.Op + ": " + opErr.Err.Error()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}
	text := err.Error()
	if strings.Contains(text, "://") {
		return "request failed"
	}
	return text
}
