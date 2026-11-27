package main

import (
	"fmt"
	"io"

	"github.com/Quality-Max/9lives-runner/internal/jevcost"
)

func printJevUsage(w io.Writer, usage *jevcost.Summary) {
	if usage == nil {
		return
	}
	if usage.Requests == 0 {
		fmt.Fprintln(w, "Jev usage — no requests")
		return
	}
	fmt.Fprintf(w, "Jev usage — %d of %d requests reported tokens", usage.RequestsWithUsage, usage.Requests)
	if usage.InputRateEstimate == nil {
		fmt.Fprintln(w)
		return
	}
	fmt.Fprintf(w, " · %d input / %d output\n", usage.InputTokens, usage.OutputTokens)
	estimate := usage.InputRateEstimate
	fmt.Fprintf(w, "Jev estimated cost for reported input — $%.9f USD\n", float64(estimate.EstimatedCostNanoUSD)/1e9)
	fmt.Fprint(w, "Estimated savings at standard input rates")
	for _, comparison := range estimate.Comparisons {
		fmt.Fprintf(w, " · %s $%.9f", comparison.Model, float64(comparison.EstimatedSavingsNanoUSD)/1e9)
	}
	fmt.Fprintln(w)
}
