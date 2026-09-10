// Package healingbridge defines the versioned, language-neutral seam between
// the Go runner and the existing Python ninelives healing engine.
package healingbridge

import "fmt"

const Version = 1

type Request struct {
	Version              int    `json:"version"`
	Framework            string `json:"framework"`
	ErrorMessage         string `json:"errorMessage"`
	StackTrace           string `json:"stackTrace,omitempty"`
	FailedSelector       string `json:"failedSelector,omitempty"`
	TestCode             string `json:"testCode"`
	PageSnapshot         string `json:"pageSnapshot,omitempty"`
	AllowAssertionChange bool   `json:"allowAssertionChange"`
}

type Response struct {
	Version          int      `json:"version"`
	FailureType      string   `json:"failureType"`
	Tier             string   `json:"tier"`
	Decision         string   `json:"decision"`
	ProposedCode     string   `json:"proposedCode,omitempty"`
	Changes          []string `json:"changes"`
	RequiresApproval bool     `json:"requiresApproval"`
	Apply            bool     `json:"apply"`
	Reason           string   `json:"reason,omitempty"`
}

type Fixture struct {
	Name     string   `json:"name"`
	Request  Request  `json:"request"`
	Expected Response `json:"expected"`
}

func Validate(request Request, response Response) error {
	if request.Version != Version || response.Version != Version {
		return fmt.Errorf("unsupported healing bridge version")
	}
	if response.FailureType == "assertion_failed" && !request.AllowAssertionChange && response.Decision != "refuse" {
		return fmt.Errorf("assertion-refusal invariant violated")
	}
	if response.Apply && response.RequiresApproval {
		return fmt.Errorf("an unapplied proposal cannot claim it was applied")
	}
	if response.Decision == "propose" && response.ProposedCode == "" {
		return fmt.Errorf("proposed repair is missing code")
	}
	if response.Decision == "propose" && !response.RequiresApproval {
		return fmt.Errorf("proposed repair must preserve review semantics")
	}
	return nil
}
