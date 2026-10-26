package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	waitInterval   = 500 * time.Millisecond
	minWaitTimeout = time.Second
	maxWaitTimeout = 30 * time.Minute
)

// waitProbe is one parsed --wait-for target: a readiness check and a display
// name that is only scheme://host[:port] or host:port, never a path, query,
// fragment or user info.
type waitProbe struct {
	name  string
	check func(context.Context) error
}

// waitNotReadyError is a target that did not become ready before the wait
// timeout. Its text is persisted in receipts, so it holds only the display
// name, the timeout and a fixed reason.
type waitNotReadyError struct{ detail string }

func (err waitNotReadyError) Error() string {
	return "--wait-for target not ready (environment): " + err.detail
}

// waitHostName is a host name or an IPv4 address: no user info, path,
// spaces or colons.
var waitHostName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// waitZone is an IPv6 zone identifier, such as en0 in fe80::1%en0.
var waitZone = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// validWaitHost accepts a host name, an IPv4 address or, when the host has a
// colon, only a valid IPv6 literal with an optional zone. So "localhost:80",
// which url.Parse yields as the host of "http://localhost:80:90", is rejected
// rather than probed.
func validWaitHost(host string) bool {
	if !strings.Contains(host, ":") {
		return waitHostName.MatchString(host)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil || !addr.Is6() {
		return false
	}
	return addr.Zone() == "" || waitZone.MatchString(addr.Zone())
}

// validPort accepts only 1 to 5 decimal digits naming 1-65535, so "+80",
// "-1" and " 80" are rejected rather than reinterpreted.
func validPort(port string) bool {
	if port == "" || len(port) > 5 {
		return false
	}
	for _, r := range port {
		if r < '0' || r > '9' {
			return false
		}
	}
	number, err := strconv.Atoi(port)
	return err == nil && number >= 1 && number <= 65535
}

// parseWaitTargets turns --wait-for values into probes. Its errors are fixed
// phrases: a rejected target is never echoed, since it may carry a
// credential.
func parseWaitTargets(targets []string) ([]waitProbe, error) {
	probes := make([]waitProbe, 0, len(targets))
	for _, target := range targets {
		probe, err := targetProbe(target)
		if err != nil {
			return nil, err
		}
		probes = append(probes, probe)
	}
	return probes, nil
}

// waitForTargets probes each target in turn until all are ready or the
// shared timeout passes. A target is an http(s) URL, ready when it answers
// with a status below 500 other than 429, or a host:port, ready when it
// accepts a TCP connection. If ctx ends first (cancellation, or the run's
// own deadline) its error is returned unchanged, so the caller reports an
// interrupted run rather than a target that was not ready.
func waitForTargets(ctx context.Context, probes []waitProbe, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	waitCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	for _, probe := range probes {
		reason := ""
		for {
			err := probe.check(waitCtx)
			if err == nil {
				break
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// A probe cut short by the wait deadline keeps the last real
			// reason, such as "connection refused", rather than "timed out".
			if reason == "" || waitCtx.Err() == nil {
				reason = waitReason(err)
			}
			if waitCtx.Err() != nil {
				return waitNotReadyError{fmt.Sprintf("%s was not ready after %s: %s", probe.name, timeout.Round(time.Second), reason)}
			}
			select {
			case <-waitCtx.Done():
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return waitNotReadyError{fmt.Sprintf("%s was not ready after %s: %s", probe.name, timeout.Round(time.Second), reason)}
			case <-time.After(waitInterval):
			}
		}
	}
	return nil
}

// targetProbe returns one readiness check and its display name.
func targetProbe(target string) (waitProbe, error) {
	lower := strings.ToLower(target)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		parsed, err := url.Parse(target)
		// An IPv6 host must be bracketed, so its colons are never read as a
		// port separator.
		if err != nil || parsed.Hostname() == "" || !validWaitHost(parsed.Hostname()) || strings.Contains(parsed.Hostname(), ":") != strings.HasPrefix(parsed.Host, "[") {
			return waitProbe{}, errors.New("an http(s) target needs a host name or IP address")
		}
		if port := parsed.Port(); port != "" || strings.HasSuffix(parsed.Host, ":") {
			if !validPort(port) {
				return waitProbe{}, errors.New("a target's port must be digits naming 1 to 65535")
			}
		}
		display := (&url.URL{Scheme: strings.ToLower(parsed.Scheme), Host: parsed.Host}).String()
		// Readiness is the target's own answer: a redirect is a response, so
		// it is not followed to a login service or into a loop.
		client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		return waitProbe{name: display, check: func(ctx context.Context) error {
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
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
		}}, nil
	}
	host, port, err := net.SplitHostPort(target)
	if err != nil || host == "" || !validWaitHost(host) || !validPort(port) || strings.Contains(host, ":") != strings.HasPrefix(target, "[") {
		return waitProbe{}, errors.New("a target is an http(s) URL or host:port, with a host name or IP address and a port of 1 to 65535")
	}
	address := net.JoinHostPort(host, port)
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	return waitProbe{name: address, check: func(ctx context.Context) error {
		conn, err := dialer.DialContext(ctx, "tcp", address)
		if err != nil {
			return err
		}
		return conn.Close()
	}}, nil
}

// waitReason is the structured part of a probe error: the HTTP status, a
// TLS failure, the network operation and error code, or a timeout. Anything
// else is a fixed phrase, so no error text that could quote the target
// reaches output or receipts.
func waitReason(err error) string {
	var status httpStatusError
	if errors.As(err, &status) {
		return status.Error()
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	var verification *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	if errors.As(err, &verification) || errors.As(err, &unknownAuthority) || errors.As(err, &hostname) || errors.As(err, &invalid) {
		return "certificate not trusted"
	}
	var record tls.RecordHeaderError
	var alert tls.AlertError
	if errors.As(err, &record) || errors.As(err, &alert) || strings.Contains(err.Error(), "HTTP response to HTTPS client") || strings.Contains(err.Error(), "tls: ") {
		return "tls handshake failed"
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

// netReason names the operating system's error code with a fixed phrase and
// nothing a target could have supplied.
func netReason(err error) string {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "no such host"
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case syscall.ECONNREFUSED:
			return "connection refused"
		case syscall.ECONNRESET:
			return "connection reset"
		case syscall.ECONNABORTED:
			return "connection aborted"
		case syscall.EHOSTUNREACH:
			return "host unreachable"
		case syscall.ENETUNREACH:
			return "network unreachable"
		case syscall.ETIMEDOUT:
			return "timed out"
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return "timed out"
	}
	if strings.Contains(err.Error(), "refused") {
		return "connection refused"
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

// String never lists the targets: flag output must not echo them.
func (w *waitTargets) String() string { return "" }
func (w *waitTargets) Set(value string) error {
	if strings.TrimSpace(value) == "" || len(value) > 512 || strings.ContainsAny(value, "\r\n\x00") || len(w.values) == 16 {
		w.invalid = true
		return nil
	}
	w.values = append(w.values, value)
	return nil
}
