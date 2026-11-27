// Package jevcost summarizes provider-reported Jev token usage and applies
// published input rates without changing execution budgets or verdicts.
package jevcost

import (
	"context"
	"sync"
)

// Totals describes the usage Jev reported. When RequestsWithUsage is below
// Requests, the token counts cover only the requests with a usage record.
type Totals struct {
	Requests          int64 `json:"requests"`
	RequestsWithUsage int64 `json:"requestsWithUsage"`
	InputTokens       int64 `json:"inputTokens"`
	OutputTokens      int64 `json:"outputTokens"`
}

func (t *Totals) Add(other Totals) {
	t.Requests += other.Requests
	t.RequestsWithUsage += other.RequestsWithUsage
	t.InputTokens += other.InputTokens
	t.OutputTokens += other.OutputTokens
}

type Summary struct {
	Totals
	InputRateEstimate *Estimate `json:"inputRateEstimate,omitempty"`
}

type Estimate struct {
	InputNanoUSDPerToken int64        `json:"inputNanoUSDPerToken"`
	EstimatedCostNanoUSD int64        `json:"estimatedCostNanoUSD"`
	Comparisons          []Comparison `json:"comparisons"`
}

type Comparison struct {
	Model                   string `json:"model"`
	InputNanoUSDPerToken    int64  `json:"inputNanoUSDPerToken"`
	EstimatedCostNanoUSD    int64  `json:"estimatedCostNanoUSD"`
	EstimatedSavingsNanoUSD int64  `json:"estimatedSavingsNanoUSD"`
}

func (t Totals) Summary() *Summary {
	s := &Summary{Totals: t}
	if t.RequestsWithUsage == 0 {
		return s
	}
	// A rate in nano-USD per token preserves sub-cent sums. Published
	// standard, uncached input rates and sources are listed in docs/jev.md.
	const jevInputRate = 42
	estimate := &Estimate{InputNanoUSDPerToken: jevInputRate, EstimatedCostNanoUSD: t.InputTokens * jevInputRate}
	for _, baseline := range [...]struct {
		model string
		rate  int64
	}{
		{"gpt-6-luna", 100},
		{"gemini-3.1-flash-lite", 250},
		{"gpt-6-sol", 2000},
		{"claude-opus-5-5", 4000},
	} {
		cost := t.InputTokens * baseline.rate
		estimate.Comparisons = append(estimate.Comparisons, Comparison{
			Model: baseline.model, InputNanoUSDPerToken: baseline.rate,
			EstimatedCostNanoUSD: cost, EstimatedSavingsNanoUSD: cost - estimate.EstimatedCostNanoUSD,
		})
	}
	s.InputRateEstimate = estimate
	return s
}

// Tracker counts requests before I/O, so an in-flight or failed request at
// reporting time appears as missing usage. It retains only counters.
type Tracker struct {
	mu     sync.Mutex
	totals Totals
}

type trackerKey struct{}

func WithTracker(ctx context.Context) (context.Context, *Tracker) {
	tracker := &Tracker{}
	return context.WithValue(ctx, trackerKey{}, tracker), tracker
}

func FromContext(ctx context.Context) *Tracker {
	tracker, _ := ctx.Value(trackerKey{}).(*Tracker)
	return tracker
}

func (t *Tracker) Request() {
	if t != nil {
		t.mu.Lock()
		t.totals.Requests++
		t.mu.Unlock()
	}
}

func (t *Tracker) Usage(input, output int) {
	if t != nil {
		t.mu.Lock()
		t.totals.RequestsWithUsage++
		t.totals.InputTokens += int64(input)
		t.totals.OutputTokens += int64(output)
		t.mu.Unlock()
	}
}

func (t *Tracker) Summary() *Summary {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	totals := t.totals
	t.mu.Unlock()
	return totals.Summary()
}
