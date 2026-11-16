package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type doctorReport struct {
	Ready      bool
	Checks     []doctorCheck
	Unverified []string
}

// fakeAndroidHost builds a fake JDK, SDK and pinned tooling directory, points
// the Android variables at them, and serves the adb host protocol on a
// loopback port with device serials that must never be reported.
func fakeAndroidHost(t *testing.T, appium string) (tools string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake SDK uses shell scripts")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node not installed")
	}
	root := t.TempDir()
	sdk, jdk := filepath.Join(root, "sdk"), filepath.Join(root, "jdk")
	tools = filepath.Join(root, "fixture-tools")
	write := func(name, content string, mode os.FileMode) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(jdk, "bin", "java"), "#!/bin/sh\necho 'openjdk version \"21.0.5\" 2024-10-15' >&2\n", 0o755)
	write(filepath.Join(sdk, "platform-tools", "adb"), "#!/bin/sh\necho 'Android Debug Bridge version 1.0.41'\necho 'Version 36.0.0-fixture'\necho \"Installed as $0\"\n", 0o755)
	write(filepath.Join(sdk, "emulator", "emulator"), "#!/bin/sh\n[ \"$1\" = -list-avds ] && printf 'INFO    | fixture-noise\\nFixture_API_35\\n'\n", 0o755)
	for _, name := range []string{"aapt2", "apksigner"} {
		write(filepath.Join(sdk, "build-tools", "35.0.0", name), "#!/bin/sh\n", 0o755)
	}
	for name, version := range map[string]string{"appium": appium, "appium-uiautomator2-driver": "8.7.0"} {
		manifest, _ := json.Marshal(map[string]string{"name": name, "version": version})
		write(filepath.Join(tools, "node_modules", name, "package.json"), string(manifest), 0o644)
	}
	write(filepath.Join(tools, "node_modules", "appium", "index.js"), "", 0o644)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			request := make([]byte, 16)
			if _, err := conn.Read(request); err == nil && string(request) == "000chost:devices" {
				body := "emulator-5554\tdevice\nfixture-serial-A\tdevice\nfixture-serial-B\tunauthorized\n"
				fmt.Fprintf(conn, "OKAY%04x%s", len(body), body)
			}
			conn.Close()
		}
	}()
	t.Setenv("JAVA_HOME", jdk)
	t.Setenv("ANDROID_HOME", sdk)
	t.Setenv("ANDROID_SDK_ROOT", "")
	t.Setenv("NINELIVES_ANDROID_TOOLS", tools)
	t.Setenv("ANDROID_ADB_SERVER_PORT", fmt.Sprint(listener.Addr().(*net.TCPAddr).Port))
	return tools
}

func runDoctor(t *testing.T, args ...string) (int, string, doctorReport) {
	t.Helper()
	var out, stderr bytes.Buffer
	code := run(append([]string{"doctor", "--format", "json"}, args...), &out, &stderr)
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("exit %d: %s %s", code, out.String(), stderr.String())
	}
	return code, out.String(), report
}

func checksByName(report doctorReport) map[string]doctorCheck {
	checks := map[string]doctorCheck{}
	for _, check := range report.Checks {
		checks[check.Name] = check
	}
	return checks
}

// The Android checks run only under --android, read the variables the CLI
// hands the helper for it, and report counts and versions without serials
// or paths.
func TestDoctorAndroidChecksAFakeSDKWithoutDisclosingSerialsOrPaths(t *testing.T) {
	fakeAndroidHost(t, "3.8.0")
	t.Chdir(t.TempDir())
	_, _, plain := runDoctor(t)
	for _, check := range plain.Checks {
		if strings.HasPrefix(check.Name, "android/") {
			t.Fatalf("Android checks without --android: %+v", check)
		}
	}
	_, raw, report := runDoctor(t, "--android")
	checks := checksByName(report)
	for _, name := range []string{"android/java", "android/sdk", "android/adb", "android/emulator", "android/build-tools", "android/avds", "android/appium", "android/uiautomator2"} {
		if checks[name].Status != "ok" {
			t.Fatalf("%s: %+v", name, checks[name])
		}
	}
	if devices := checks["android/devices"]; devices.Status != "warning" || devices.Message != "1 emulator and 1 physical device attached and ready; 1 unauthorized. Serials are not shown." {
		t.Fatalf("devices: %+v", devices)
	}
	if avds := checks["android/avds"]; !strings.Contains(avds.Message, "1 AVD: Fixture_API_35.") {
		t.Fatalf("avds: %+v", avds)
	}
	for _, private := range []string{"fixture-serial", "emulator-5554", "fixture-noise", "fixture-tools", "Installed as", os.Getenv("ANDROID_HOME")} {
		if strings.Contains(raw, private) {
			t.Fatalf("doctor disclosed %q: %s", private, raw)
		}
	}
	if !strings.Contains(strings.Join(report.Unverified, "\n"), "Appium server start") {
		t.Fatalf("unverified: %v", report.Unverified)
	}
	_, located, _ := runDoctor(t, "--android", "--paths")
	if !strings.Contains(located, os.Getenv("ANDROID_HOME")) || !strings.Contains(located, os.Getenv("NINELIVES_ANDROID_TOOLS")) {
		t.Fatalf("--paths did not name the SDK and tooling roots: %s", located)
	}
}

// A missing Android prerequisite makes doctor exit 2 with the install
// command, and a complete fake host keeps an otherwise ready project at 0.
func TestDoctorAndroidExitCodesFollowMissingPrerequisites(t *testing.T) {
	fakeAndroidHost(t, "3.8.0")
	root := browserProject(t)
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"private":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	if code, raw, report := runDoctor(t, "--android"); code != 0 || !report.Ready {
		t.Fatalf("complete Android host: exit %d %s", code, raw)
	}
	tools := fakeAndroidHost(t, "3.9.0")
	code, _, report := runDoctor(t, "--android")
	appium := checksByName(report)["android/appium"]
	if code != 2 || report.Ready || appium.Status != "missing" || !strings.Contains(appium.Message, "is 3.9.0, not the pinned 3.8.0") ||
		appium.Fix != `npm install --prefix "$NINELIVES_ANDROID_TOOLS" --save-exact --ignore-scripts appium@3.8.0 appium-uiautomator2-driver@8.7.0` {
		t.Fatalf("unpinned Appium: exit %d %+v", code, appium)
	}
	if err := os.RemoveAll(tools); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANDROID_HOME", filepath.Join(root, "absent-sdk"))
	code, _, report = runDoctor(t, "--android")
	if sdk := checksByName(report)["android/sdk"]; code != 2 || sdk.Status != "missing" {
		t.Fatalf("absent SDK: exit %d %+v", code, sdk)
	}
}

func TestDoctorAndroidUsage(t *testing.T) {
	var out, stderr bytes.Buffer
	if code := run([]string{"doctor", "--android", "spec.ts"}, &out, &stderr); code != 2 || !strings.Contains(stderr.String(), "--android") {
		t.Fatalf("a spec without --sdk: exit %d %s", code, stderr.String())
	}
}
