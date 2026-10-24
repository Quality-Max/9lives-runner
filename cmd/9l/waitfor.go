package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
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
		if err != nil || parsed.Hostname() == "" {
			return nil, "", errors.New("an http(s) target needs a host")
		}
		if port := parsed.Port(); port != "" {
			if number, err := strconv.Atoi(port); err != nil || number < 1 || number > 65535 {
				return nil, "", errors.New("an http(s) target's port must be between 1 and 65535")
			}
		}
		display := &url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: parsed.Path}
		// Readiness is the target's own answer: a redirect is a response, so
		// it is not followed to a login service or into a loop.
		client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
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
				return httpStatusError(response.StatusCode)
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
// network error's operation and code, or a timeout. Anything else is a fixed
// phrase, so no error text that could quote the target reaches output.
func waitReason(err error) string {
	var status httpStatusError
	if errors.As(err, &status) {
		return status.Error()
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Err != nil {
		return opErr.Op + ": " + netReason(opErr.Err)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return "timed out"
	}
	return "request failed"
}

// netReason keeps the operating system's error code text ("connection
// refused", "no such host") and nothing a target could have supplied.
func netReason(err error) string {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "no such host"
	}
	var sysErr interface{ Errno() uintptr }
	if errors.As(err, &sysErr) || strings.Contains(err.Error(), "connection refused") {
		return strings.TrimSpace(strings.TrimPrefix(err.Error(), "connect: "))
	}
	return "unreachable"
}

// httpStatusError is a probe answer that is not ready yet.
type httpStatusError int

func (status httpStatusError) Error() string { return fmt.Sprintf("HTTP %d", int(status)) }

// waitTargets collects --wait-for values. A rejected value is never
// returned through flag.Value, which would print it verbatim, since a
// target may carry a credential; the caller reports a fixed message.
type waitTargets struct {
	values  []string
	invalid bool
}

func (w *waitTargets) String() string { return strings.Join(w.values, ",") }
func (w *waitTargets) Set(value string) error {
	if strings.TrimSpace(value) == "" || len(value) > 512 || strings.ContainsAny(value, "\r\n\x00") || len(w.values) == 16 {
		w.invalid = true
		return nil
	}
	w.values = append(w.values, value)
	return nil
}
