package playwrightsdk

import (
	"bytes"
	"strings"
	"testing"
)

func androidFrames() []map[string]any {
	f := targetFrames()
	for _, frame := range f {
		frame["targetId"] = TargetID("android:finance")
	}
	r := map[string]any{"type": "target_runtime", "testId": testKey, "retry": 0, "targetId": TargetID("android:finance"), "runtime": map[string]any{
		"appPackage": "dev.ninelives.fixture", "apkSHA256": strings.Repeat("a", 64), "api": "35", "release": "15", "abi": "arm64-v8a", "emulator": "36.4.9", "appium": "3.8.0", "uiautomator2": "8.7.0", "worker": strings.Repeat("b", 64),
	}}
	return append(f[:4], append([]map[string]any{r}, f[4:]...)...)
}

func TestAndroidRuntimeIsRequiredAndBoundToActiveAttempt(t *testing.T) {
	identity := owner
	identity.Target = "android:finance"
	v, err := New().ValidateAttempt(stream(androidFrames()), identity)
	if err != nil || v.TargetAssertions != 1 || len(v.AndroidRuntimes) != 1 || v.AndroidRuntimes[0].TestID != testKey || v.AndroidRuntimes[0].AppPackage != "dev.ninelives.fixture" {
		t.Fatalf("missing bound runtime: %+v %v", v, err)
	}
	cases := map[string]func([]map[string]any) []map[string]any{
		"missing": func(f []map[string]any) []map[string]any { return append(f[:4], f[5:]...) },
		"duplicate": func(f []map[string]any) []map[string]any {
			return append(f[:5], append([]map[string]any{f[4]}, f[5:]...)...)
		},
		"late":           func(f []map[string]any) []map[string]any { f[4], f[5] = f[5], f[4]; return f },
		"foreign test":   func(f []map[string]any) []map[string]any { f[4]["testId"] = strings.Repeat("c", 64); return f },
		"foreign target": func(f []map[string]any) []map[string]any { f[4]["targetId"] = TargetID("android:other"); return f },
		"stale attempt":  func(f []map[string]any) []map[string]any { f[4]["attemptId"] = "old"; return f },
		"wrong retry":    func(f []map[string]any) []map[string]any { f[4]["retry"] = 1; return f },
	}
	for _, key := range []string{"appPackage", "apkSHA256", "api", "release", "abi", "emulator", "appium", "uiautomator2", "worker"} {
		key := key
		cases["invalid "+key] = func(f []map[string]any) []map[string]any {
			f[4]["runtime"].(map[string]any)[key] = "untrusted content"
			return f
		}
		cases["absent "+key] = func(f []map[string]any) []map[string]any { delete(f[4]["runtime"].(map[string]any), key); return f }
	}
	cases["unknown field"] = func(f []map[string]any) []map[string]any {
		f[4]["runtime"].(map[string]any)["screen"] = "synthetic content"
		return f
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := New().ValidateAttempt(stream(change(androidFrames())), identity); err == nil {
				t.Fatal("invalid runtime accepted")
			}
		})
	}
	raw := stream(androidFrames())
	raw = bytes.Replace(raw, []byte(`"api":"35"`), []byte(`"api":"35","api":"35"`), 1)
	if _, err := New().ValidateAttempt(raw, identity); err == nil {
		t.Fatal("duplicate nested key accepted")
	}
}
