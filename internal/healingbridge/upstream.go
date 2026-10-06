package healingbridge

// UpstreamResult is a Python ninelives HealingResult as serialized by its
// to_dict() method, plus healed_code. Field names follow the Python package.
type UpstreamResult struct {
	Tier             string   `json:"tier"`
	Success          bool     `json:"success"`
	HealedCode       *string  `json:"healed_code"`
	ChangesMade      []string `json:"changes_made"`
	RequiresApproval bool     `json:"requires_approval"`
}

const AssertionRefusalReason = "Possible product behavior change; forcing green could mask a bug"

// FromUpstream maps a Python healing result onto the bridge contract.
//
// The Python Tier 1 healer marks locator repairs requires_approval=false
// because its own CLI may apply them. The runner never applies a repair, so
// every proposal crossing this bridge requires a separate approval: the
// upstream flag can tighten review semantics but never loosen them. An
// assertion failure is refused unless the request explicitly allows assertion
// changes, whatever the upstream healer proposed.
func FromUpstream(request Request, failureType string, upstream UpstreamResult) (Response, error) {
	response := Response{Version: Version, FailureType: failureType, Tier: upstream.Tier, Changes: []string{}}
	switch {
	case failureType == "assertion_failed" && !request.AllowAssertionChange:
		response.Decision, response.Reason = "refuse", AssertionRefusalReason
	case !upstream.Success || upstream.HealedCode == nil || *upstream.HealedCode == "":
		response.Decision, response.Reason = "refuse", "upstream healer produced no repair"
	default:
		response.Decision = "propose"
		response.ProposedCode = *upstream.HealedCode
		response.Changes = append(response.Changes, upstream.ChangesMade...)
		response.RequiresApproval = true
	}
	return response, Validate(request, response)
}
