package assessment

// Error contains a stable allowlisted code and fixed text, never raw helper or
// source errors. The CLI writes this on stderr and emits no partial JSON report.
type Error struct{ Code string }

func (e *Error) Error() string {
	return "assessment [" + e.Code + "]: " + diagnostics[e.Code]
}

func diagnostic(code string) error { return &Error{Code: code} }

var diagnostics = map[string]string{
	"node-unavailable":         "Node executable is unavailable; install Node 22 or newer",
	"parser-unavailable":       "bundled TypeScript parser could not be prepared or loaded",
	"syntax":                   "source is not valid syntax for the bundled TypeScript parser",
	"typescript-in-javascript": "source uses TypeScript-only syntax in a JavaScript file; Playwright loads .js, .mjs, .cjs and .jsx without the TypeScript transform",
	"annotation":               "requirement or outcome annotation has an invalid identifier",
	"limit":                    "source analysis exceeded its fact or input limit",
	"output-limit":             "analyzer output exceeded 1 MiB",
	"timeout":                  "analyzer exceeded its time budget",
	"cancelled":                "analyzer was cancelled",
	"helper-failed":            "analyzer process failed; raw diagnostics were suppressed",
	"invalid-evidence":         "analyzer returned malformed or incomplete evidence",
}

var analysisLimits = map[string]string{
	"dynamic-callback":       "The test callback is not an inline block; its assertions were not inspected.",
	"declaration-generation": "This declaration is inside generated or conditional code; runtime case count is unknown.",
	"nested-function":        "Assertions in nested functions may not execute or may execute later.",
	"conditional-flow":       "Conditional control flow prevents complete assertion analysis.",
	"loop":                   "A loop repeats a fixed wait an unknown number of times; the order of waits and assertions across iterations was not analyzed.",
	"shadowed-binding":       "An imported test or expect binding is shadowed; affected matcher calls were excluded.",
	"runtime-skip":           "A body-level skip or fixme depends on runtime behavior that was not evaluated.",
	"unresolved-helper":      "A helper call was not resolved; assertions inside that helper remain unknown.",
	"possibly-soft":          "A matcher's failure is caught, but its expect is configured from options that were not read, so whether it is soft, and still fails the test, is unknown; it was counted as an assertion.",
	"unknown-matcher":        "A matcher outside the Playwright 1.61.1–1.64.1 API, or an assertion helper not declared async, is neither awaited nor returned, or only one of a thrown failure and a rejection from it is caught; whether it returns a promise is unknown.",
}
