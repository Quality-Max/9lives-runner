# Security policy

## Report a vulnerability

Use [GitHub private vulnerability reporting](https://github.com/Quality-Max/9lives-runner/security/advisories/new).
Do not post vulnerabilities, credentials or sensitive receipts in public issues.
Include affected CLI/SDK versions, platform, a minimal reproduction, impact
and a proposed fix when available. Use synthetic data and redact secret values.

## Supported versions

This early project focuses fixes on current `main`, the latest published CLI
release, currently v0.2.0, and the latest published SDK, currently
`@9l/playwright` 0.1.3. Older releases and development snapshots have no
separate security maintenance commitment.

## Local execution boundaries

The runner executes test code with the local user's permissions. It is not a
sandbox for untrusted tests. Browser activity can reach the network and mutate
the selected application; use approved targets and isolated test data.

Only explicitly named application environment keys are forwarded with
`--pass-env`. Provider credentials belong in Go's calling environment, never
in browser settings, command arguments, goal text or published evidence.
Diagnostics and receipts are bounded and sanitized; page content and attachment
bodies are omitted by default. See the [SDK evidence contract](docs/sdk-bridge.md)
and [goal policy](docs/goals.md) for the exact boundaries.
