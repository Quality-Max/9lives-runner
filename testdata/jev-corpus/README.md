# Jev qualification corpus (`9l.jev-corpus/1`)

Synthetic, labeled cases for QUA-2233. Every page is an original illustration
with a known answer and an ordinary Playwright assertion. `manifest.json` freezes
each spec's SHA-256, its label and its partition. Half of each lane is held out
(`heldoutSHA256`): never tune thresholds, prompts or questions against it.
Change a frozen case only with a new corpus version.

| Lane | Cases | Labels |
|---|---|---|
| diagnose | 16: renamed button/link/label/nested control, duplicates, cookie modal, loading spinner, wrong page, disabled target, visible error, wrong price/quantity/status, error banner on a plausible page, hidden promo, blank page | expected disposition and family; the runner's failure class |
| repair | 10: single and semantic renames, shared-word trap, label and link renames, `exact` flag, duplicates only, removed target, wrong business outcome after a rename | expected outcome and the correct name |
| goal | 7: signup, delayed control, two parameters, forbidden payment, identical duplicates, wrong business outcome, no viable control | acceptable goal statuses and the test verdict; oracle scripts for completing goals |

```sh
node scripts/jev-qualify.cjs --mode validate    # the labels hold, using real runs; no AI
node scripts/jev-qualify.cjs --mode offline     # baseline: facts only and offline Tier 1
node scripts/jev-qualify.cjs --mode jev --partition heldout --confirm-paid --max-requests N
```

Only `--mode jev` makes paid requests. It refuses without `TYPESAFE_API_KEY` in
the process, `--confirm-paid` and a `--max-requests` cap covering the worst case
(one per diagnosis and repair, two per goal decision). It prints that number
first. Validate and offline results are policy and wiring evidence, never
quality claims.

A run fails on any **false green**: a changed failed result, an assertion
failure marked eligible for repair, a verified wrong or unlabeled repair, a
modified source, or a passing test whose label requires a failure.

Faults that need a provider or a clock are qualified by offline Go tests, not
by this browser corpus:

| Fault | Test |
|---|---|
| stale evidence | `TestUnresolvedAndErrorStatesMakeNoClaim` (diagnosis), `TestGroundedRepairAbstainsWithoutClearEvidence` (repair) |
| foreign evidence | `TestUnresolvedAndErrorStatesMakeNoClaim` (real J1 client) |
| missing evidence | `TestUnresolvedAndErrorStatesMakeNoClaim`, `TestFromReceiptReadsOnlyAttachedEvidence` |
| malformed decisions | `TestUnresolvedAndErrorStatesMakeNoClaim`, `TestGroundedRepairAbstainsWithoutClearEvidence`, `TestUnclearGoalChoicesAbstain` |
| provider outage | `TestJevGoalsRunInsideTheEnginesProtections`, `TestUnclearGoalChoicesAbstain` |
| timeout and cancellation | `TestCancellationAndDeadlineReachProvider`, `TestTypedRequestAndSafeRecord` |
