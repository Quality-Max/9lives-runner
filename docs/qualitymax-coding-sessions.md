# QualityMax coding sessions

QualityMax runs coding agents (Claude Code or Codex) in cloud sandboxes for
its mobile app and web. A session can connect two MCP servers: the hosted
QualityMax server and a local `9l mcp`. After every turn QualityMax reads the
9lives receipts back and reports them next to the QualityMax calls its server
recorded for that session. This page describes what QualityMax expects from
9lives. The platform side is `services/coding_agent/ninelives_tools.py` and
`provenance.py` in the QualityMax backend.

## How QualityMax runs 9lives

- It pins one release, starting with `0.6.0-beta.1`. It installs the Linux
  archive only when the archive matches the SHA-256 it pinned from that
  release's `SHA256SUMS`, and only when `9l version --format json` reports the
  pinned `cliVersion`, `go-runner`, the `mcp`, `run`, `assess` and
  `provenance` commands and receipt contract `1`.
- It starts the server from the workspace root as
  `9l mcp --provider none --receipt-dir .9lives/receipts --heal-receipt-dir .9lives/healing-receipts`,
  with `NINELIVES_AUTODETECT_PROVIDER=off` and model, GitHub and QualityMax
  credentials removed from the environment. 9lives makes no model calls in a
  session. `heal_test` is limited to Tier 1, and the coding agent proposes
  any larger repair itself.
- It gives the agent the
  [`qualitymax-connected-qa` skill](../skills/qualitymax-connected-qa/SKILL.md)
  from the same release tag, checked against a pinned digest.
- It adds `/.9lives/` to `.git/info/exclude`, so receipts never appear in the
  user's diff or commits.

## What QualityMax reads

QualityMax lists only files at
`.9lives/receipts/<runId>/<jobId>/<attemptId>/receipt.json` smaller than
64 KiB. It hashes each one and reads these fields: `version`, `runId`,
`jobId`, `attemptId`, `spec`, `adapter`, `status`, `executed`, `validated`,
`executedTests`, `failureCount`, `skippedTests`, `flakyTests`,
`verifiedAssertions`, `reportedAssertions`, `passedWithoutAssertions`,
`assertionCoverage`, `agentProvenance.status`, `durationMs`, `startedAt`
and `finishedAt`. It rejects a receipt when the bytes change between listing
and reading, when the identity fields do not match the directory, or when the
status is unknown. It never copies failure messages, attachments or
validation text. `TestReceiptLayoutAndFieldsReadByQualityMaxCodingSessions`
in `internal/runner` pins this layout. Change both sides together.

QualityMax counts a receipt as a local pass only when `status` is `passed`,
the run was executed and validated, at least one test ran, none failed and the
spec is inside the workspace. The session reports the newest receipt for each
spec. That local verdict is separate from QualityMax's cloud verdict, which
reads `verified` only when the QualityMax server recorded a passed execution.
A local pass never becomes a cloud verification.

## Limits

- 9lives tools cover only the primary workspace, not any additional
  repositories in the session.
- `agentProvenance` is a declared snapshot. It records whether the branch,
  commit and spec source still matched. It does not prove who wrote the
  test.
- The release is a beta. Its documented qualification limits apply inside a
  session too.
