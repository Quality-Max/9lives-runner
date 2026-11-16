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
		"display": "4619827259835644672", "displayWidth": "1080", "displayHeight": "2340", "density": "420", "posture": "flat",
		"versionCode": "28", "versionName": "1.4.0", "signer": strings.Repeat("d", 64),
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
	for _, key := range []string{"appPackage", "apkSHA256", "api", "release", "abi", "emulator", "appium", "uiautomator2", "worker", "display", "displayWidth", "displayHeight", "density", "posture", "versionCode", "versionName", "signer"} {
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
	cases["posture content"] = func(f []map[string]any) []map[string]any {
		f[4]["runtime"].(map[string]any)["posture"] = "DeviceState{identifier=2, name='OPENED'}"
		return f
	}
	cases["version content"] = func(f []map[string]any) []map[string]any {
		f[4]["runtime"].(map[string]any)["versionName"] = "1.4.0 (debug build for user@host)"
		return f
	}
	cases["display name"] = func(f []map[string]any) []map[string]any {
		f[4]["runtime"].(map[string]any)["display"] = "Built-in Screen"
		return f
	}
	for _, change := range []func(f []map[string]any) []map[string]any{
		func(f []map[string]any) []map[string]any {
			f[4]["runtime"].(map[string]any)["display"] = "unknown"
			return f
		},
		func(f []map[string]any) []map[string]any {
			f[4]["runtime"].(map[string]any)["posture"] = "half-opened"
			return f
		},
		func(f []map[string]any) []map[string]any {
			r := f[4]["runtime"].(map[string]any)
			r["versionCode"], r["versionName"], r["signer"] = "unknown", "unknown", "unknown"
			return f
		},
	} {
		if _, err := New().ValidateAttempt(stream(change(androidFrames())), identity); err != nil {
			t.Fatalf("unknown display or a reported posture rejected: %v", err)
		}
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

func TestPhysicalAndroidRuntimeIsClosedAndBound(t *testing.T) {
	physical := func() []map[string]any {
		f := androidFrames()
		r := f[4]["runtime"].(map[string]any)
		delete(r, "emulator")
		r["device"] = "physical"
		r["deviceHash"] = strings.Repeat("c", 64)
		return f
	}
	identity := owner
	identity.Target = "android:finance"
	v, err := New().ValidateAttempt(stream(physical()), identity)
	if err != nil || len(v.AndroidRuntimes) != 1 || v.AndroidRuntimes[0].Device != "physical" || v.AndroidRuntimes[0].TestID != testKey {
		t.Fatal("physical runtime not bound to test")
	}
	for name, mutate := range map[string]func(map[string]any){
		"mixed":        func(r map[string]any) { r["emulator"] = "36.4.9" },
		"raw serial":   func(r map[string]any) { r["serial"] = "private" },
		"missing hash": func(r map[string]any) { delete(r, "deviceHash") },
		"bad hash":     func(r map[string]any) { r["deviceHash"] = "private" },
		"wrong kind":   func(r map[string]any) { r["device"] = "emulator" },
		"missing kind": func(r map[string]any) { delete(r, "device") },
	} {
		t.Run(name, func(t *testing.T) {
			f := physical()
			mutate(f[4]["runtime"].(map[string]any))
			if _, err := New().ValidateAttempt(stream(f), identity); err == nil {
				t.Fatal("invalid physical runtime accepted")
			}
		})
	}
}
