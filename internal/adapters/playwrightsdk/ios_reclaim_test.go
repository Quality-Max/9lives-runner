package playwrightsdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/runner"
)

const (
	reclaimOwner   = "9l-ios-12345678-1234-1234-1234-123456789abc"
	reclaimUDID    = "ABCDEF12-1234-1234-1234-123456789ABC"
	reclaimRuntime = "com.apple.CoreSimulator.SimRuntime.iOS-18-6"
	reclaimType    = "com.apple.CoreSimulator.SimDeviceType.iPhone-16"
)

// fakeSimulators is a synthetic CoreSimulator inventory. It establishes the
// reclaim decisions only; scripts/ios-smoke.cjs qualifies the real commands.
type fakeSimulators struct {
	devices                              map[string][]iosDevice
	calls                                [][]string
	inventoryFails, deleteFails, garbage bool
}

func (fake *fakeSimulators) simctl(ctx context.Context, args ...string) ([]byte, error) {
	fake.calls = append(fake.calls, args)
	if _, bounded := ctx.Deadline(); !bounded {
		return nil, errors.New("unbounded simctl command")
	}
	switch args[0] {
	case "list":
		if fake.inventoryFails {
			return nil, errors.New("synthetic inventory failure")
		}
		if fake.garbage {
			return []byte(`{"devices":`), nil
		}
		return json.Marshal(map[string]any{"devices": fake.devices})
	case "shutdown":
		return nil, errors.New("synthetic shutdown failure")
	case "delete":
		if fake.deleteFails {
			return nil, errors.New("synthetic deletion failure")
		}
		for runtime, devices := range fake.devices {
			var kept []iosDevice
			for _, device := range devices {
				if device.UDID != args[1] {
					kept = append(kept, device)
				}
			}
			fake.devices[runtime] = kept
		}
	}
	return nil, nil
}

func (fake *fakeSimulators) commands() string {
	var names []string
	for _, call := range fake.calls {
		names = append(names, call[0])
	}
	return strings.Join(names, ",")
}

func writeOwnership(t *testing.T, scratch, file, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(scratch, file), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func ownershipJSON(name, runtime, deviceType string) string {
	raw, _ := json.Marshal(iosOwnership{Version: 1, Name: name, Runtime: runtime, DeviceType: deviceType})
	return string(raw)
}

func TestReclaimRemovesOnlyTheRecordedSimulator(t *testing.T) {
	owned := iosDevice{Name: reclaimOwner, UDID: reclaimUDID, DeviceType: reclaimType}
	unrelated := iosDevice{Name: "9l-sentinel-1", UDID: "00000000-0000-0000-0000-000000000001", DeviceType: reclaimType}
	other := "com.apple.CoreSimulator.SimRuntime.iOS-17-5"
	cases := map[string]struct {
		devices             map[string][]iosDevice
		inventoryFails      bool
		deleteFails         bool
		garbage             bool
		wantOutcome         string
		wantCommands        string
		wantUnrelatedStays  bool
		wantOwnedStillThere bool
	}{
		"removed":           {devices: map[string][]iosDevice{reclaimRuntime: {unrelated, owned}}, wantOutcome: "removed", wantCommands: "list,shutdown,delete,list"},
		"already absent":    {devices: map[string][]iosDevice{reclaimRuntime: {unrelated}}, wantOutcome: "absent", wantCommands: "list"},
		"inventory fails":   {devices: map[string][]iosDevice{reclaimRuntime: {owned}}, inventoryFails: true, wantOutcome: "unconfirmed", wantCommands: "list", wantOwnedStillThere: true},
		"inventory garbage": {devices: map[string][]iosDevice{reclaimRuntime: {owned}}, garbage: true, wantOutcome: "unconfirmed", wantCommands: "list", wantOwnedStillThere: true},
		"delete fails":      {devices: map[string][]iosDevice{reclaimRuntime: {unrelated, owned}}, deleteFails: true, wantOutcome: "unconfirmed", wantCommands: "list,shutdown,delete,list", wantOwnedStillThere: true},
		// The name alone never selects a device: runtime, type and count must match.
		"other runtime": {devices: map[string][]iosDevice{other: {owned}}, wantOutcome: "unconfirmed", wantCommands: "list", wantOwnedStillThere: true},
		"other type": {devices: map[string][]iosDevice{reclaimRuntime: {{Name: reclaimOwner, UDID: reclaimUDID, DeviceType: "com.apple.CoreSimulator.SimDeviceType.iPad-Air"}}},
			wantOutcome: "unconfirmed", wantCommands: "list", wantOwnedStillThere: true},
		"two devices": {devices: map[string][]iosDevice{reclaimRuntime: {owned, {Name: reclaimOwner, UDID: "00000000-0000-0000-0000-000000000002", DeviceType: reclaimType}}},
			wantOutcome: "unconfirmed", wantCommands: "list", wantOwnedStillThere: true},
		"two runtimes":   {devices: map[string][]iosDevice{reclaimRuntime: {owned}, other: {owned}}, wantOutcome: "unconfirmed", wantCommands: "list", wantOwnedStillThere: true},
		"malformed udid": {devices: map[string][]iosDevice{reclaimRuntime: {{Name: reclaimOwner, UDID: "all", DeviceType: reclaimType}}}, wantOutcome: "unconfirmed", wantCommands: "list"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			scratch := t.TempDir()
			writeOwnership(t, scratch, reclaimOwner+".owned.json", ownershipJSON(reclaimOwner, reclaimRuntime, reclaimType))
			fake := &fakeSimulators{devices: test.devices, inventoryFails: test.inventoryFails, deleteFails: test.deleteFails, garbage: test.garbage}
			got := reclaimIOSSimulators(scratch, fake.simctl)
			want := []runner.TargetCleanup{{Kind: "ios-simulator", Outcome: test.wantOutcome}}
			if !reflect.DeepEqual(got, want) || fake.commands() != test.wantCommands {
				t.Fatalf("got %+v after %s, want %+v after %s", got, fake.commands(), want, test.wantCommands)
			}
			for _, call := range fake.calls {
				if (call[0] == "shutdown" || call[0] == "delete") && (len(call) != 2 || call[1] != reclaimUDID) {
					t.Fatalf("command reached a device that is not the recorded one: %v", call)
				}
			}
			ownedLeft, unrelatedLeft := false, false
			for _, devices := range fake.devices {
				for _, device := range devices {
					ownedLeft = ownedLeft || device.UDID == reclaimUDID
					unrelatedLeft = unrelatedLeft || device.Name == unrelated.Name
				}
			}
			if ownedLeft != test.wantOwnedStillThere {
				t.Fatalf("owned device present=%v, want %v", ownedLeft, test.wantOwnedStillThere)
			}
			if name == "removed" && !unrelatedLeft {
				t.Fatal("an unrelated simulator was removed")
			}
		})
	}
}

func TestReclaimTrustsOnlyClosedOwnershipRecords(t *testing.T) {
	valid := ownershipJSON(reclaimOwner, reclaimRuntime, reclaimType)
	rejected := map[string]string{
		"unknown field":   `{"version":1,"name":"` + reclaimOwner + `","runtime":"` + reclaimRuntime + `","deviceType":"` + reclaimType + `","udid":"` + reclaimUDID + `"}`,
		"other version":   strings.Replace(valid, `"version":1`, `"version":2`, 1),
		"name mismatch":   ownershipJSON("9l-ios-00000000-0000-0000-0000-000000000000", reclaimRuntime, reclaimType),
		"runtime pattern": ownershipJSON(reclaimOwner, "com.apple.CoreSimulator.SimRuntime.iOS-18-6; rm", reclaimType),
		"type pattern":    ownershipJSON(reclaimOwner, reclaimRuntime, "iPhone 16"),
		"trailing data":   valid + valid,
		// A streaming decoder leaves a stray closing bracket unread.
		"trailing brace":   valid + "}",
		"trailing bracket": valid + "]",
		// Decoding alone keeps the last of two members with the same name.
		"duplicate member": `{"version":2,"version":1,"name":"` + reclaimOwner + `","runtime":"` + reclaimRuntime + `","deviceType":"` + reclaimType + `"}`,
		"not json":         "9l",
		"oversized":        valid + strings.Repeat(" ", 2000),
	}
	for name, content := range rejected {
		t.Run(name, func(t *testing.T) {
			scratch := t.TempDir()
			writeOwnership(t, scratch, reclaimOwner+".owned.json", content)
			fake := &fakeSimulators{devices: map[string][]iosDevice{reclaimRuntime: {{Name: reclaimOwner, UDID: reclaimUDID, DeviceType: reclaimType}}}}
			got := reclaimIOSSimulators(scratch, fake.simctl)
			if !reflect.DeepEqual(got, []runner.TargetCleanup{{Kind: "ios-simulator", Outcome: "unconfirmed"}}) || len(fake.calls) != 0 {
				t.Fatalf("an untrusted record ran %v and reported %+v", fake.calls, got)
			}
		})
	}
	t.Run("symlink", func(t *testing.T) {
		scratch := t.TempDir()
		target := filepath.Join(t.TempDir(), "record")
		if err := os.WriteFile(target, []byte(valid), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(scratch, reclaimOwner+".owned.json")); err != nil {
			t.Skip("symbolic links are unavailable")
		}
		fake := &fakeSimulators{}
		if got := reclaimIOSSimulators(scratch, fake.simctl); len(got) != 1 || got[0].Outcome != "unconfirmed" || len(fake.calls) != 0 {
			t.Fatalf("a linked record was followed: %+v %v", got, fake.calls)
		}
	})
	t.Run("unreadable scratch is unconfirmed", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("reading a file as a directory reports a missing path on Windows")
		}
		// A path that exists but cannot be listed may hide a record.
		scratch := filepath.Join(t.TempDir(), "not-a-directory")
		writeOwnership(t, filepath.Dir(scratch), filepath.Base(scratch), valid)
		fake := &fakeSimulators{}
		got := reclaimIOSSimulators(scratch, fake.simctl)
		if !reflect.DeepEqual(got, []runner.TargetCleanup{{Kind: "ios-simulator", Outcome: "unconfirmed"}}) || len(fake.calls) != 0 {
			t.Fatalf("an unreadable scratch reported %+v after %v", got, fake.calls)
		}
	})
	t.Run("only an iOS attempt is reclaimed", func(t *testing.T) {
		// The real adapter entry point: an Android attempt never reaches
		// simctl, and an iOS attempt without a record runs no command.
		scratch := t.TempDir()
		writeOwnership(t, scratch, reclaimOwner+".owned.json", valid)
		if got := New().ReclaimTarget(scratch, map[string]string{"NINELIVES_TARGET": "android:finance"}); got != nil {
			t.Fatalf("an Android attempt reclaimed %+v", got)
		}
		if got := New().ReclaimTarget(t.TempDir(), map[string]string{"NINELIVES_TARGET": "ios:finance"}); got != nil {
			t.Fatalf("an iOS attempt without a record reclaimed %+v", got)
		}
	})
	t.Run("other files are not records", func(t *testing.T) {
		scratch := t.TempDir()
		for _, file := range []string{"worker-artifact", "9l-ios-nope.owned.json", reclaimOwner + ".owned.json.bak", "9L-IOS-12345678-1234-1234-1234-123456789ABC.owned.json"} {
			writeOwnership(t, scratch, file, valid)
		}
		fake := &fakeSimulators{}
		if got := reclaimIOSSimulators(scratch, fake.simctl); got != nil || len(fake.calls) != 0 {
			t.Fatalf("unrelated files were treated as ownership: %+v %v", got, fake.calls)
		}
		if got := reclaimIOSSimulators(filepath.Join(scratch, "missing"), fake.simctl); got != nil {
			t.Fatalf("a missing scratch reported cleanup: %+v", got)
		}
	})
}

// A simctl that never returns, even after its bound (a process exec.Cmd.Wait
// would wait for indefinitely), cannot hold the attempt: reclaim gives up
// shortly after the bound and reports the device unconfirmed (#291).
func TestReclaimStopsWaitingForASimctlThatIgnoresItsBound(t *testing.T) {
	budget, grace := iosReclaimBudget, iosSimctlExitGrace
	iosReclaimBudget, iosSimctlExitGrace = 300*time.Millisecond, 50*time.Millisecond
	defer func() { iosReclaimBudget, iosSimctlExitGrace = budget, grace }()
	release := make(chan struct{})
	defer close(release)
	stuck := func(context.Context, ...string) ([]byte, error) { <-release; return nil, nil }
	scratch := t.TempDir()
	writeOwnership(t, scratch, reclaimOwner+".owned.json", ownershipJSON(reclaimOwner, reclaimRuntime, reclaimType))
	started := time.Now()
	got := reclaimIOSSimulators(scratch, stuck)
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("reclaim waited %v for a simctl that never returns", elapsed)
	}
	if want := []runner.TargetCleanup{{Kind: "ios-simulator", Outcome: "unconfirmed"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// Per-command bounds add up across records; the overall budget does not.
// Every record past the budget is reported unconfirmed without a command.
func TestReclaimBoundsAllRecordsTogether(t *testing.T) {
	budget, grace := iosReclaimBudget, iosSimctlExitGrace
	iosReclaimBudget, iosSimctlExitGrace = 300*time.Millisecond, 50*time.Millisecond
	defer func() { iosReclaimBudget, iosSimctlExitGrace = budget, grace }()
	var calls atomic.Int32
	slow := func(ctx context.Context, _ ...string) ([]byte, error) {
		calls.Add(1)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	scratch := t.TempDir()
	for i := 0; i < maxIOSOwnershipFiles; i++ {
		name := fmt.Sprintf("9l-ios-12345678-1234-1234-1234-1234567890%02x", i)
		writeOwnership(t, scratch, name+".owned.json", ownershipJSON(name, reclaimRuntime, reclaimType))
	}
	started := time.Now()
	got := reclaimIOSSimulators(scratch, slow)
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("reclaim of %d records took %v past its budget", maxIOSOwnershipFiles, elapsed)
	}
	if len(got) != maxIOSOwnershipFiles || calls.Load() != 1 {
		t.Fatalf("got %d outcomes after %d commands, want %d after 1", len(got), calls.Load(), maxIOSOwnershipFiles)
	}
	for _, cleanup := range got {
		if cleanup.Outcome != "unconfirmed" {
			t.Fatalf("outcome %q past the budget", cleanup.Outcome)
		}
	}
}
