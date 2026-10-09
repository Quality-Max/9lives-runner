# Architecture decisions

## Boundaries

The CLI in `cmd/9l` is presentation only. `internal/runner` owns versioned
contracts, planning, scheduling, and the `Adapter`, `ProcessExecutor`, and
`Store` ports. `internal/adapters/playwright` is the first framework adapter.
The local process and filesystem implementations are defaults behind those
ports; a future MCP server can call the core without parsing CLI output and a
durable worker can replace storage and command execution.

[Test assessment](test-assessment.md) uses an owned, bounded TypeScript syntax
helper without loading tests or configuration. Go validates requirements and
facts and builds an advisory report with explicit unknowns. Optional creation
snapshots bind assessment and execution to a selected spec, workspace, branch
and commit, with pre/post execution checks. The repository QA skill coordinates
available specialist skills; it adds no cloud or harness dependency to the
engine. General semantic analysis and authenticated agent attribution remain
planned. Assessment stays separate from execution outcomes.

The first scheduler runs independent jobs with bounded concurrency. Plans with
dependencies are topologically sorted and run conservatively in sequence.
Distributed leases, recovery, and a concurrent DAG scheduler remain QUA-1925.

## qmax-code evaluation

The terminal-neutral package produced by QUA-1913 was reviewed at
`5eb8cbef2158022b940131473b547852b0994153`. Its public contract executes
`codex exec`, resumes validated Codex thread IDs, and manages agent rollout
continuity. Those are useful primitives for a future agent-backed
healing adapter, but ordinary test execution must not depend on an agent. The
initial runner therefore does not import `qmax-code/codexrunner`.

## Planning and evidence contracts

The runner uses these contracts:

- plan before execution, with visible skip and budget-cap reasons;
- reserve the run-wide job budget before processes start;
- bind every event, attempt, artifact reference, and receipt to its originating
  run and attempt IDs;
- keep findings (`failureCount`) separate from completeness;
- distinguish process execution from structured-result validation;
- use one execution boundary for timeouts, process-tree cancellation, output
  limits, environment overlays, redaction, and evidence persistence.

## Python compatibility

The Go runner does not duplicate healing. `internal/healingbridge` defines the
version-1 request/response seam and fixtures for selector repair, assertion
refusal, and review/apply semantics. `9l heal` runs native healing and no longer
delegates to the installed Python package; the Python CLI and its API consumers
remain unchanged.
