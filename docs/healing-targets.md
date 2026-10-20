# Duplicate-target qualification

The first slice of [issue #80](https://github.com/Quality-Max/9lives-runner/issues/80)
uses a deterministic local records app with real Chromium. Two rows have stable
IDs, `record-a` and `record-b`, and identical **Edit** buttons containing nested
spans. The intended action is to edit `record-a`. A row-scoped accessible locator
selects its button; the editor must display exactly `record-a`.

```sh
npm ci
npm exec playwright install chromium
npm run smoke:healing-targets
```

The smoke script builds the local Go CLI and runs every control through ordinary
`9l run`. It uses one worker, no retries, a 20-second execution timeout and a
30-second deadline per run. Each test starts with a fresh page, the same two
records and an empty app event log. All app requests are intercepted locally;
no platform account, model call or external service is used.

| Control | Action and observation | Required result |
| --- | --- | --- |
| Baseline | Open `record-a`; app logs `record-a` | Weak editor-visible check and exact-ID check both pass |
| Wrong row | Open `record-b`; app logs `record-b` | Weak check passes; exact-ID check fails on its `toHaveText('record-a')` assertion |
| Reordered rows | Reverse the rows, then open `record-a`; app logs `record-a` | Both checks pass with the same stable row locator |
| Separate canary | Open `record-a`; deliberately expect `record-b` | Canary fails on its exact-ID assertion |
| Setup failure | Stop before loading the app | Inconclusive; cannot count as detecting the wrong target |
| Timeout | Exhaust the setup budget before loading the app | Inconclusive; cannot count as detecting the wrong target |

The main spec and its assertions stay byte-for-byte identical across baseline,
wrong-row and reordered runs. Only the fixture control changes. The canary is a
separate spec with a deliberately false expected ID. Running all fixture specs
directly will therefore fail; use the qualification command to check the
expected outcomes.

The app posts the clicked row's ID to its intercepted audit endpoint after
opening the editor. The harness records this separately from the assertions and
attaches only synthetic IDs and action names. The smoke checks the exact log,
initial row order, executed test count, absence of retries, receipt validation,
report checksum, and the failing assertion's source file, line and matcher.
An unrelated failure does not qualify as detection.

Bounded results are saved to `.context/healing-targets/qualification.json` and
the advisory assessment to `.context/healing-targets/report.json`. Raw browser
reports, attachment bodies and errors are removed with the temporary project.
The summary retains the allowlisted synthetic app observation, receipt IDs,
evidence hashes and source/config/fixture/requirements hashes. A throwaway Git
repository pins the copied fixture, and declared `fixture-qualification`
provenance is checked before and after each execution. These runtime snapshots
do not authenticate the creating agent or fingerprint an arbitrary application.

Source assessment flags the weak test's unmapped exact-ID outcome. Conditional
target selection leaves static assertion adequacy unknown; the runtime controls
supply the bounded evidence above. An assessment exit code is not a test verdict.

This qualifies the fixture and its assertions. It does not run a healer or show
that a proposed repair picked the intended target. General wrong-target
injection ([#79](https://github.com/Quality-Max/9lives-runner/issues/79)), competing
passing-candidate ambiguity protection
([#78](https://github.com/Quality-Max/9lives-runner/issues/78)), and actual-target
repair evidence ([#77](https://github.com/Quality-Max/9lives-runner/issues/77))
remain separate work. Other #80 scenarios, including duplicate forms, locator
repair preservation/refusal and failing or zero-test candidate application,
remain open. CI runs this qualification in the existing Ubuntu SDK job with
Chromium and the pinned Playwright version; other engines are unqualified.
