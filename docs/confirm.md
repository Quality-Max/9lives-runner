# Confirm: does the reproduction fail before the fix?

Status: experimental. `9l confirm` needs `@9l/playwright` 0.1.1 or newer and
a CLI built from `main` until the next CLI release. Qualified with real
Chromium on one synthetic repository, see [Qualification](#qualification). No
accuracy, latency or cost claim is made for other applications.

A reviewer, a bot or a user reports a finding. Reading the code can make a
claim look plausible or wrong, but it cannot show that the defect is real or
that a change fixes it. `9l confirm` checks that directly: it runs one
reproduction spec on the revision the finding was reported against and again
on the revision that claims to fix it, each in its own checkout, and compares
the results.

```sh
# The fix is uncommitted in the working tree; the finding was reported on HEAD.
9l confirm tests/repro/order-count.spec.ts --unfixed HEAD

# Both sides are commits.
9l confirm tests/repro/order-count.spec.ts --unfixed main --fixed fix/order-count \
  --finding-id QUA-14 --finding "Checkout confirms one item fewer than ordered"
```

```
unfixed 3f9c2a1b7d40 (main)
fixed 8e01d4c5a9f2 (fix/order-count)
confirm tests/repro/order-count.spec.ts
  unfixed  3f9c2a1b7d40 (main)  run run-20261010T101500Z-1a2b3c4d5e6f: failed
  fixed    8e01d4c5a9f2 (fix/order-count)  run run-20261010T101503Z-5e6f7a8b9c0d: passed
  CONFIRMED       test 1f0c7e9a2b3d  unfixed assertion-failed, fixed passed
Verdict: confirmed
Report: .9lives/receipts/confirmations/confirm-20261010T101500Z-9c8d7e6f.json
```

Write the reproduction before the fix, run it on the unfixed revision to see
it fail, then confirm. A confirmed verdict shows that the spec's assertions
tell the two revisions apart. It does not show that the spec tests the
reported finding; that is still a review of the spec.

## How it works

1. **Revisions.** `--unfixed` names the revision the finding was reported
   against. `--fixed` names the fixing revision; without it, the working tree
   as it is, including uncommitted changes, is the fixed side. Revisions are
   resolved to commits; a value that looks like an option is refused. The two
   sides must differ: the same commit twice, or a working tree with no
   changes beyond the spec since `--unfixed`, is a usage error.
2. **Checkouts.** Each commit is checked out as a detached Git worktree in a
   private temporary directory, with repository hooks disabled and Git LFS
   downloads skipped. The working tree is never modified. The reproduction
   spec is read from the working tree and written at the same path into each
   checkout, so both sides run identical spec bytes, whether or not the spec
   exists at that revision. Every `node_modules` directory from the
   repository root down to the spec is linked from the working tree. A
   revision that commits a symbolic link, junction or file where one of the
   spec's parent directories should be is refused (exit 2) before anything is
   written, so neither the spec nor a dependency link can land outside the
   checkout.
3. **Runs.** The spec runs on the unfixed checkout, then on the fixed side, as
   two ordinary receipted `9l run --sdk` runs with the project's Playwright
   retries disabled (`--retries=0`). Each checkout is removed after its run;
   the dependency links are removed first, so removal never reaches the
   installed dependencies.
4. **Classification**, per test and then overall, from the engine evidence of
   each run:

| Test result | Unfixed | Fixed |
| --- | --- | --- |
| `confirmed` | an assertion failed | passed |
| `not-reproduced` | passed | passed |
| `fix-ineffective` | an assertion failed | an assertion failed |
| `regressed` | passed | an assertion failed |
| `inconclusive` | a test failed without a failed assertion (for example an action timed out), was retried, was skipped, or ran on one side only | |

The verdict is `inconclusive` if any test is, or if either run was canceled,
timed out or produced invalid evidence; otherwise `regressed` if any test
regressed; otherwise `fix-ineffective` if any test still fails on the fixed
side; otherwise `confirmed` if any test was confirmed; otherwise
`not-reproduced`. A passing sanity test beside the reproduction does not hide
a confirmation, but a reproduction that still fails after the fix does. An
inconclusive report carries `inconclusiveReason`: `run-incomplete` (also
when a goal failed or stopped, even if Playwright reported the test as
passed),
`no-tests`, `tests-differ`, `retried`, `skipped` or
`failed-without-assertion`.

## Exit status

| Exit | Meaning |
| --- | --- |
| 0 | `confirmed` |
| 1 | A conclusive verdict other than confirmed: `not-reproduced`, `fix-ineffective` or `regressed` |
| 2 | Usage or setup error: missing or unknown revision, a spec outside the repository, the same revision twice, a spec this runner cannot plan, a revision with a linked parent directory, or links that could not be created |
| 3 | `inconclusive`, an operational failure, or a report that could not be saved |

## Use it

The spec must import `test` from `@9l/playwright`, as for `9l run --sdk`.

- **Run the code under test from the checkout.** Only what the spec, its
  fixtures or the Playwright configuration start or import from the
  checkout differs between the runs: modules the spec imports, a server a
  fixture starts, or the configuration's `webServer` command, which runs in
  the checkout. An application you started separately, for example from
  your working tree on port 3000, is the same application for both runs,
  and the result is `not-reproduced` or `fix-ineffective` whatever the fix
  does. The text output says so on those verdicts.
- **Dependencies.** Both sides use the dependencies installed in the working
  tree. When a revision's `package.json` or lockfile, in any directory from
  the root to the spec, differs from the working tree's, the report sets
  `dependenciesDiffer` and the text output warns: that side did not run with
  its own dependencies.
- **Committed files only.** A checkout holds what the commit holds. Untracked
  and ignored files, such as `.env`, exist only in the working tree. Pass
  required settings with `--pass-env`, as for `9l run`.
- **Limits.** `--timeout` bounds each run, `--deadline` both, `--workers` each
  run's concurrency, `--max-output-bytes` each captured stream.
- **Windows.** Linking `node_modules` needs permission to create symbolic
  links (Developer Mode or an elevated shell); without it, confirm exits 2.

## Evidence and privacy

Both runs are ordinary runs with receipts under `--receipt-dir`, inspectable
with `9l result <run-id>`. The confirmation report is saved to
`<receipt-dir>/confirmations/<id>.json`. It records the spec's path within
the repository and its SHA-256, each side's ref, commit, run and how the spec
related to that revision's file (`absent`, `same` or `replaced`), and whether
the working tree had other changes (`dirty`). `--finding-id` is recorded as
given (up to 64 letters, digits or `._#:/-`); `--finding` text is recorded
only as its SHA-256. Test identities are the engine's hashed test IDs, never
titles.

## MCP

`9l mcp` serves the same command as the `confirm_finding` tool, with
`spec`, `unfixed`, optional `fixed`, `finding_id`, `finding` and
`run_timeout`. The result carries the report and `reportPath`; a usage or
setup error is an error result. See [MCP server](mcp.md).

## Boundaries

- Each side runs once. A nondeterministic application can give another
  verdict on another run.
- Git LFS content and submodules are not present in checkouts.
- The command compares assertions, not behavior: a reproduction whose
  assertions check the wrong thing can be confirmed.

The policy name `confirm-v1` in each report identifies these rules; it
changes whenever they do.

## Qualification

`npm run smoke:confirm` builds the CLI and a throwaway Git repository with an
order-count module: a buggy commit (`items.length - 1`), a fixing commit and
an unrelated commit. An untracked reproduction spec imports the module,
renders the count in real Chromium and asserts "2 items".

| Unfixed | Fixed | Verdict | Exit |
| --- | --- | --- | --- |
| buggy commit | fixing commit | `confirmed` (assertion failed, then passed) | 0 |
| fixing commit | unrelated commit | `not-reproduced` | 1 |
| buggy commit (`HEAD`) | the fix, uncommitted in the working tree | `confirmed`, `dirty: true` | 0 |

Six receipts, all validated with one executed test, three saved reports, no
checkout left registered and the installed dependencies intact. What this
does and does not establish: one repository, one defect and one execution
per side. It shows that each side runs its own revision's code and that the
classification separates a real fix from an unrelated change. It is not a
benchmark and says nothing about other applications.
