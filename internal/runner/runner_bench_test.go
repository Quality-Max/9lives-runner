package runner

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// BenchmarkBuildPlan tracks runner-side startup overhead separately from
// Playwright/browser startup.
func BenchmarkBuildPlan(b *testing.B) {
	project := b.TempDir()
	for i := 0; i < 100; i++ {
		name := "case" + strconv.Itoa(i) + ".fake"
		if err := os.WriteFile(filepath.Join(project, name), nil, 0o600); err != nil {
			b.Fatal(err)
		}
	}
	pattern := filepath.Join(project, "*.fake")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := BuildPlan([]string{pattern}, PlanOptions{Adapters: []Adapter{fakeAdapter{}}}); err != nil {
			b.Fatal(err)
		}
	}
}
