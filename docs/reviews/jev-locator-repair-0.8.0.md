# Jev locator repair validation for 0.8.0

On 27 November 2026, a local runner build with the locator repair patch ran the
frozen held-out partition of `testdata/jev-corpus` on macOS arm64 with Chromium.
The run used corpus `9l.jev-corpus/1` (held-out digest prefix `c0eb68f46b22`), one
repeat per case, and screenshots disabled. A deterministic run checked the
labels before the live run configured with `jev-1.14.0`.

## Frozen held-out results

| Check | Result |
| --- | --- |
| Deterministic label validation | 16 of 16 matched |
| Live locator repair | 4 of 4 repairable renames recovered and verified. The removed-target case abstained. |
| Live diagnosis | 3 of 8 expected dispositions matched. Five cases were unresolved. |
| Live goals | 2 of 3 goal statuses matched. All 3 independent test verdicts matched. |

The live run produced usable evidence for all 16 cases and recorded zero false
greens. An earlier live run of the same held-out partition recovered 0 of 4
repairable renames. In the rerun, the wrong-business-outcome case produced an
unresolved goal and the expected failing test verdict.

## Campaign workflow

A separate campaign workflow on a disposable local fixture exercised a renamed
control. Jev selected the replacement from six observed controls. The runner
changed one locator name literal, replayed the candidate in isolation, and
passed the existing workflow assertions.

## Local engineering checks

- Go race suite with the pinned compatibility fixtures and `go vet ./...`
- Qualification harness tests (8 of 8) and the offline Jev Chromium smoke

This report covers one model configuration and one local browser environment.
