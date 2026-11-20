// Package providerhttp is the bounded, non-redirecting Go provider transport.
// Provider bodies and transport errors must never become diagnostics.
package providerhttp

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Do executes one request without retries or redirects. The caller owns its
// context/deadline. HTTP failures return only the status, never the body or URL.
func Do(req *http.Request, timeout time.Duration, maxResponseBytes int64) ([]byte, error) {
	response, err := (&http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}
		return nil, errors.New("provider request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, &StatusError{Code: response.StatusCode}
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}
		return nil, errors.New("provider response could not be read")
	}
	if int64(len(raw)) > maxResponseBytes {
		return nil, errors.New("provider response is too large")
	}
	return raw, nil
}

type StatusError struct{ Code int }

func (e *StatusError) Error() string { return fmt.Sprintf("provider returned HTTP %d", e.Code) }
