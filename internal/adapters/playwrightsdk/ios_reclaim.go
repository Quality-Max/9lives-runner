package playwrightsdk

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// An owned iOS Simulator is registered with CoreSimulator, outside the
// worker's process tree, so stopping that tree does not remove it. The driver
// records the exact device it is about to create in the attempt's private
// scratch and deletes the record once it has confirmed the device gone. A
// record that outlives the worker names a device nobody else will remove.
const (
	iosOwnershipVersion   = 1
	maxIOSOwnershipFiles  = 16
	maxIOSOwnershipBytes  = 1024
	maxIOSInventoryBytes  = 4 << 20
	iosReclaimInventory   = 30 * time.Second
	iosReclaimShutdown    = 15 * time.Second
	iosReclaimDelete      = 30 * time.Second
	iosCleanupKind        = "ios-simulator"
	iosCleanupRemoved     = "removed"
	iosCleanupAbsent      = "absent"
	iosCleanupUnconfirmed = "unconfirmed"
)

var (
	iosOwnershipFile = regexp.MustCompile(`^(9l-ios-[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12})\.owned\.json$`)
	iosRuntimeID     = regexp.MustCompile(`^com\.apple\.CoreSimulator\.SimRuntime\.iOS-[0-9]{1,3}(-[0-9]{1,5}){1,2}$`)
	iosDeviceTypeID  = regexp.MustCompile(`^com\.apple\.CoreSimulator\.SimDeviceType\.[A-Za-z0-9-]{1,80}$`)
	iosDeviceUDID    = regexp.MustCompile(`^[A-Fa-f0-9]{8}(-[A-Fa-f0-9]{4}){3}-[A-Fa-f0-9]{12}$`)
)

type iosOwnership struct {
	Version    int    `json:"version"`
	Name       string `json:"name"`
	Runtime    string `json:"runtime"`
	DeviceType string `json:"deviceType"`
}

// simctlFunc runs one bounded simctl command and returns its standard output.
type simctlFunc func(ctx context.Context, args ...string) ([]byte, error)

// ReclaimTarget removes each owned Simulator still recorded in scratch after
// the worker's process tree has stopped. It never selects a device by state:
// only the recorded unguessable name with the recorded runtime and device
// type is removed, and only a fresh inventory decides whether it is gone.
func (Adapter) ReclaimTarget(scratch string, env map[string]string) []runner.TargetCleanup {
	return reclaimIOSSimulators(scratch, xcrunSimctl(env))
}

func reclaimIOSSimulators(scratch string, simctl simctlFunc) []runner.TargetCleanup {
	entries, err := os.ReadDir(scratch)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		if iosOwnershipFile.MatchString(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	var cleanups []runner.TargetCleanup
	for index, name := range names {
		outcome := iosCleanupUnconfirmed
		// More records than one attempt can own are not trusted or acted on.
		if index < maxIOSOwnershipFiles {
			if owned, ok := readIOSOwnership(filepath.Join(scratch, name)); ok {
				outcome = reclaimIOSSimulator(owned, simctl)
			}
		}
		cleanups = append(cleanups, runner.TargetCleanup{Kind: iosCleanupKind, Outcome: outcome})
	}
	return cleanups
}

// readIOSOwnership accepts only a small regular file whose closed content
// repeats the name in its own file name. The worker wrote it; nothing in it
// reaches a command line except identifiers that match these fixed patterns.
func readIOSOwnership(path string) (iosOwnership, bool) {
	var owned iosOwnership
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxIOSOwnershipBytes {
		return owned, false
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) > maxIOSOwnershipBytes {
		return owned, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&owned); err != nil || decoder.More() {
		return owned, false
	}
	match := iosOwnershipFile.FindStringSubmatch(filepath.Base(path))
	if owned.Version != iosOwnershipVersion || match == nil || owned.Name != match[1] ||
		!iosRuntimeID.MatchString(owned.Runtime) || !iosDeviceTypeID.MatchString(owned.DeviceType) {
		return owned, false
	}
	return owned, true
}

type iosDevice struct {
	Name       string `json:"name"`
	UDID       string `json:"udid"`
	DeviceType string `json:"deviceTypeIdentifier"`
}

// iosDevicesNamed lists the devices carrying the owned name, by runtime.
func iosDevicesNamed(name string, simctl simctlFunc) (map[string][]iosDevice, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), iosReclaimInventory)
	defer cancel()
	raw, err := simctl(ctx, "list", "devices", "-j")
	if err != nil {
		return nil, false
	}
	var inventory struct {
		Devices map[string][]iosDevice `json:"devices"`
	}
	if err := json.Unmarshal(raw, &inventory); err != nil || inventory.Devices == nil {
		return nil, false
	}
	named := make(map[string][]iosDevice)
	for runtime, devices := range inventory.Devices {
		for _, device := range devices {
			if device.Name == name {
				named[runtime] = append(named[runtime], device)
			}
		}
	}
	return named, true
}

func reclaimIOSSimulator(owned iosOwnership, simctl simctlFunc) string {
	named, ok := iosDevicesNamed(owned.Name, simctl)
	if !ok {
		return iosCleanupUnconfirmed
	}
	if len(named) == 0 {
		return iosCleanupAbsent
	}
	devices := named[owned.Runtime]
	// One device, in the recorded runtime, of the recorded type. Anything
	// else carrying the name is not provably ours and is left alone.
	if len(named) != 1 || len(devices) != 1 || devices[0].DeviceType != owned.DeviceType || !iosDeviceUDID.MatchString(devices[0].UDID) {
		return iosCleanupUnconfirmed
	}
	// Shutdown may fail or hang on a device that is mid-boot. Deletion keeps
	// its own bound, and neither command's exit status decides the outcome.
	for _, step := range []struct {
		command string
		bound   time.Duration
	}{{"shutdown", iosReclaimShutdown}, {"delete", iosReclaimDelete}} {
		ctx, cancel := context.WithTimeout(context.Background(), step.bound)
		_, _ = simctl(ctx, step.command, devices[0].UDID)
		cancel()
	}
	if after, ok := iosDevicesNamed(owned.Name, simctl); ok && len(after) == 0 {
		return iosCleanupRemoved
	}
	return iosCleanupUnconfirmed
}

// xcrunSimctl runs simctl with a fixed executable and a closed environment.
// DEVELOPER_DIR is the value the engine itself gave the worker, if any.
func xcrunSimctl(env map[string]string) simctlFunc {
	var environment []string
	for _, key := range []string{"PATH", "HOME", "USER", "LOGNAME", "LANG", "LC_ALL", "DEVELOPER_DIR"} {
		value, ok := env[key]
		if !ok || key != "DEVELOPER_DIR" {
			value, ok = os.LookupEnv(key)
		}
		if ok && value != "" {
			environment = append(environment, key+"="+value)
		}
	}
	return func(ctx context.Context, args ...string) ([]byte, error) {
		command := exec.CommandContext(ctx, "/usr/bin/xcrun", append([]string{"simctl"}, args...)...)
		command.Env = environment
		command.WaitDelay = time.Second
		var stdout bytes.Buffer
		command.Stdout = &limitedWriter{buffer: &stdout, remaining: maxIOSInventoryBytes}
		if err := command.Run(); err != nil {
			return nil, err
		}
		return stdout.Bytes(), nil
	}
}

// limitedWriter fails once a command has written more than its bound, so an
// oversized inventory is rejected instead of being parsed in part.
type limitedWriter struct {
	buffer    *bytes.Buffer
	remaining int
}

func (writer *limitedWriter) Write(data []byte) (int, error) {
	if len(data) > writer.remaining {
		return 0, os.ErrInvalid
	}
	writer.remaining -= len(data)
	return writer.buffer.Write(data)
}
