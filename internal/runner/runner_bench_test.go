package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
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

// BenchmarkBoundedThroughput reports scheduler work from inside Execute. The
// expected delay is 240ms: four 120ms jobs dispatched by two workers.
func BenchmarkBoundedThroughput(b *testing.B) {
	const jobs = 4
	const workers = 2
	const jobDelay = 120
	b.ReportMetric(float64(jobs*jobDelay/workers), "expected-delay-ms")
	receiptsRoot := b.TempDir()
	for i := 0; i < b.N; i++ {
		plan := Plan{Version: 1, RunID: fmt.Sprintf("run-benchthroughput%d", i)}
		for job := 0; job < jobs; job++ {
			plan.Jobs = append(plan.Jobs, fakeJob(fmt.Sprintf("job-%03d", job), "sleep", jobDelay))
		}
		b.StartTimer()
		summary, err := Execute(context.Background(), plan, ExecuteOptions{Workers: workers, Timeout: time.Second, ReceiptDir: receiptsRoot, Adapters: []Adapter{fakeAdapter{}}})
		b.StopTimer()
		if err != nil || len(summary.Receipts) != jobs {
			b.Fatalf("throughput run failed: summary=%#v err=%v", summary, err)
		}
		b.ReportMetric(float64(jobs)/summaryDurationSeconds(summary), "jobs/s")
	}
}

func summaryDurationSeconds(summary RunSummary) float64 {
	var earliest, latest time.Time
	for _, receipt := range summary.Receipts {
		if earliest.IsZero() || receipt.StartedAt.Before(earliest) {
			earliest = receipt.StartedAt
		}
		if receipt.FinishedAt.After(latest) {
			latest = receipt.FinishedAt
		}
	}
	return latest.Sub(earliest).Seconds()
}
