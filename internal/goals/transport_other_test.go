//go:build !windows

package goals

import (
	"context"
	"net"
)

func dialGoalTransport(ctx context.Context, socket string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "unix", socket)
}
