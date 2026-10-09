//go:build windows

package goals

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func dialGoalTransport(ctx context.Context, socket string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, socket)
}

// Check the kernel object's ACL, not just the descriptor supplied at creation.
func TestWindowsGoalPipeRestrictsAccessToEngineUser(t *testing.T) {
	s := setup(t, decision(`{"action":"complete"}`), Defaults())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, err := dialGoalTransport(ctx, s.socket)
	if err != nil {
		t.Fatal("engine user cannot access owned goal pipe")
	}
	defer connection.Close()
	descriptor, ok := connection.(interface{ Fd() uintptr })
	if !ok {
		t.Fatal("goal connection has no inspectable handle")
	}
	security, err := windows.GetSecurityInfo(windows.Handle(descriptor.Fd()), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal("goal pipe ACL unavailable")
	}
	acl, _, err := security.DACL()
	if err != nil || acl == nil || acl.AceCount != 1 {
		t.Fatal("goal pipe must grant exactly one principal")
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal("engine identity unavailable")
	}
	if !strings.Contains(security.String(), ";;;"+user.User.Sid.String()+")") {
		t.Fatal("goal pipe grants another principal")
	}
	control, _, err := security.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("goal pipe inherits access from other principals")
	}
}
