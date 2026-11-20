package playwrightsdk

import (
	"strings"
	"testing"
)

func iosRuntimeFrame() map[string]any {
	return map[string]any{"type": "ios_runtime", "testId": testKey, "retry": 0, "targetId": TargetID("ios:finance"), "iosRuntime": map[string]any{
		"bundleId": "dev.ninelives.fixture", "appSHA256": strings.Repeat("a", 64), "version": "1.0", "build": "1",
		"runtime": "com.apple.CoreSimulator.SimRuntime.iOS-18-5", "deviceType": "com.apple.CoreSimulator.SimDeviceType.iPhone-16", "os": "18.5", "architecture": "arm64", "xcode": "16.4", "appium": "3.8.0", "xcuitest": "10.43.1", "worker": strings.Repeat("b", 64), "viewportWidth": "390", "viewportHeight": "844",
	}}
}
func iosFrames() []map[string]any {
	f := targetFrames()
	for _, frame := range f {
		frame["targetId"] = TargetID("ios:finance")
	}
	return append(f[:3], append([]map[string]any{iosRuntimeFrame()}, f[3:]...)...)
}
func TestIOSRuntimeRequiredAndBoundToAttemptTestAndPlatform(t *testing.T) {
	identity := owner
	identity.Target = "ios:finance"
	v, err := New().ValidateAttempt(stream(iosFrames()), identity)
	if err != nil || len(v.IOSRuntimes) != 1 || v.IOSRuntimes[0].TestID != testKey || v.TargetAssertions != 1 || len(v.AndroidRuntimes) != 0 {
		t.Fatalf("runtime attribution: %+v %v", v, err)
	}
	cases := map[string]func([]map[string]any) []map[string]any{
		"missing": func(f []map[string]any) []map[string]any { return append(f[:3], f[4:]...) },
		"duplicate": func(f []map[string]any) []map[string]any {
			return append(f[:4], append([]map[string]any{iosRuntimeFrame()}, f[4:]...)...)
		},
		"foreign test":  func(f []map[string]any) []map[string]any { f[3]["testId"] = strings.Repeat("c", 64); return f },
		"stale attempt": func(f []map[string]any) []map[string]any { f[3]["attemptId"] = "stale"; return f },
		"retry":         func(f []map[string]any) []map[string]any { f[3]["retry"] = 1; return f },
		"late":          func(f []map[string]any) []map[string]any { f[3], f[5] = f[5], f[3]; return f },
		"android payload": func(f []map[string]any) []map[string]any {
			f[3]["type"] = "target_runtime"
			f[3]["runtime"] = f[3]["iosRuntime"]
			delete(f[3], "iosRuntime")
			return f
		},
		"extra content": func(f []map[string]any) []map[string]any {
			f[3]["iosRuntime"].(map[string]any)["screen"] = "private"
			return f
		},
		"wrong runtime": func(f []map[string]any) []map[string]any {
			f[3]["iosRuntime"].(map[string]any)["os"] = "18.4"
			return f
		},
		"wrong tooling": func(f []map[string]any) []map[string]any {
			f[3]["iosRuntime"].(map[string]any)["xcuitest"] = "10.43.2"
			return f
		},
		"missing hash": func(f []map[string]any) []map[string]any {
			delete(f[3]["iosRuntime"].(map[string]any), "appSHA256")
			return f
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := New().ValidateAttempt(stream(change(iosFrames())), identity); err == nil {
				t.Fatal("invalid iOS evidence accepted")
			}
		})
	}
	for _, target := range []string{"android:finance", "web:finance"} {
		identity.Target = target
		f := iosFrames()
		for _, frame := range f {
			frame["targetId"] = TargetID(target)
		}
		if _, err := New().ValidateAttempt(stream(f), identity); err == nil {
			t.Fatal("cross-platform runtime accepted")
		}
	}
}
