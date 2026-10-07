# Native Tier 1 compatibility and safety

`9l tier1 --format json` accepts one bounded version-1 JSON request from stdin
and returns an offline proposal. The command does not access the network,
files, providers or application adapters. A changed proposal always reports
`apply: false`, `requiresApproval: true` and `metadata.provenance: "unverified"`.
The original source is immutable. A refusal exits zero with no proposed code;
invalid JSON/schema input exits nonzero with a bounded diagnostic. The command
does not replace the installed Python healing bridge.

`selectedTier` records the historical strategy choice; `attemptedTier` records
the direct native operation. For example, a bare `#save` with moved-ID HTML
selects Tier 2 although direct Tier 1 proposes a repair. Fallback comments,
test IDs and historical snapshot matching influence strategy selection.
Visibility failures with scroll/viewport evidence select Tier 1; other
visibility failures select Tier 2.

Assertion evidence in the error or stack takes precedence over a caller's
locator classification. Assertion failures are refused even with an explicit
assertion-change opt-in. Balanced source boundaries also prevent changing an
assertion-owned locator, including grouped/optional/computed `expect` callees,
assertion member chains, Python logical `assert` statements with implicit
continuations, and Cypress `should`/`and` chains. An executable
action with actual locator failure evidence remains eligible when the same
test contains a separate, untouched assertion.

## Supported source and snapshot subset

Selector replacement identifies exactly one executable static locator argument:
Playwright `page.locator`/`locator`, Cypress `cy.get`/`cy.find`, or Selenium
`driver.find_element(By.CSS_SELECTOR, ...)`. Single/double quoted literals and
static JavaScript backticks are supported. Common escaped runtime characters
are decoded before matching, and replacements are escaped for the original
source quote. The lexer scans the whole file, keeps balanced delimiters and
skips comments, ordinary strings, regex bodies and Python triple quoted strings.
Locator-like documentation does not count as an executable occurrence.

A locator passed as an argument to an unknown outer call is refused: source
syntax cannot establish whether that call is an assertion alias. Direct actions
inside recognized arrow/function callback bodies remain eligible. Separate
Python statements, including semicolon boundaries after an assertion, retain
their own ownership. These checks do not establish arbitrary runtime aliasing.

The supported selectors are complete IDs, test-ID/aria attribute selectors,
quoted legacy Playwright text selectors and class selectors. Snapshot matching is case insensitive and
retains live casing. Priority is test ID, ID, aria label, text, then class. The
candidate must be unambiguous for supported attribute and class anchors; a
class token appearing on more than one element is refused. Compound CSS,
unsafe identifier forms, expressions and multiple executable matches are
refused. `getByRole` and Cypress `contains` are not rewritten as CSS locators.

Snapshot attributes and eligible text come from a complete HTML5 DOM tree
using pinned [`golang.org/x/net/html` v0.58.0](https://pkg.go.dev/golang.org/x/net@v0.58.0/html),
including tree construction and script escaped/double-escaped states. A
separate tokenizer checks the supported complete markup and only refuses
input; its tokens never supply candidates or text. The first duplicate
attribute wins. ASCII HTML name folding preserves Unicode attribute identity,
and the parser decodes character references before matching.

Comments, inert templates, noscript and raw-text elements such as
script/style/textarea contribute no candidate attributes or descendant text.
Noscript stays excluded regardless of the browser's scripting setting.
Foreign SVG/MathML snapshots are unsupported and refuse matching. Incomplete
tags, quoted attributes, comments, templates or raw-text boundaries, malformed
supported attribute syntax, bogus declarations, NUL/invalid UTF-8 and parser
errors invalidate the entire snapshot, including any earlier candidate. The
snapshot is limited to 1 MiB; the parser rejects more than 512 open elements
and extraction uses a bounded iterative walk.

Quoted Playwright text proposals require one eligible ordinary leaf element
with exact normalized immediate text, retaining the live casing. Whitespace
and format-character normalization follows Playwright's
[legacy text matching rules](https://playwright.dev/docs/other-locators#legacy-text-locator).
Comments do not split adjacent text; child elements do. Substrings, document
concatenation and text split across sibling/child elements never establish an
exact anchor. Matching direct text on nonleaf elements, input button/submit
values and raw-text elements counts as a competitor, but cannot itself supply
a proposal. Multiple case-insensitive candidates cause conservative refusal,
even where one emitted case-sensitive selector might be unique. NonHTML
accessibility text, nonleaf/input anchors, escaped or chained text bodies,
declarative shadow trees, and snapshots containing noscript are unsupported
for text proposals. Noscript's tree depends on the scripting setting, which
offline input does not prove. Ordinary inert templates and script/style/head
text are excluded. These checks prove one text match only within this bounded
document subset; they do not establish visibility or runtime DOM identity.

This is offline document-tree extraction, not browser execution or a visibility
check. Arbitrary fragment insertion contexts, runtime DOM changes, CSS
visibility and general browser conformance are outside this slice. Synthetic
script, Unicode, template, raw-text, select and table fixtures were independently
checked in Chrome 154; that evidence does not qualify every HTML construct or
browser. Selector
transformations are attempted only when the snapshot is empty:
complete ID to ID attribute, a two-part hyphenated class to class substring, or
a test ID to a valid partial attribute. A nonempty snapshot without an eligible
live candidate causes refusal rather than a transformation fallback.

Timing proposals insert a visible wait immediately before one awaited
Playwright locator action. The action must be a complete standalone statement
inside a recognized braced async function/callback (or a top-level module
statement). A braced conditional retains its original body and callback scope.
Unbraced control bodies, expression arrows, synchronous/unknown function
contexts, multiline action expressions and non-Playwright timing are refused.
Text actions require the same unique exact element proof before timing can
propose a wait; a missing, unsupported or ambiguous text anchor cannot fall
through to timing. Only the supported quoted `text=` form is eligible.

Unsupported JavaScript template interpolation (including nested templates),
unknown string escapes, escaped JavaScript identifiers, JSX/type-angle syntax,
Python prefixed strings/explicit line continuations, and unmatched boundaries cause conservative whole-file
refusal. This deliberately trades some eligible edits for known boundaries;
ordinary comparisons with a recognized left operand remain eligible. The
engine does not claim general JavaScript, TypeScript or Python parsing.

## Actual pinned-source differential evidence

The strict differential suite compares the native classifier, selector parser,
strategy and proposals with actual upstream Python responses. It runs 28
classifier/parser/strategy rows and 28 proposal rows against **each** version:

| Release label | Exact Git revision | Runtime `__version__` |
|---|---|---|
| 0.1.3 | `8a40d8d5c83f27f84384f060aeed74a3ded7ab77` | 0.1.0 |
| 0.2.1 | `568c7a6882441c13cdb9bfe8c0190ca0bf7d8240` | 0.2.1 |

Rows label `common-parity`, `version-difference` or
`intentional-safety-deviation`. They cover ordered classification and selector
precedence, framework/version extraction, strategy, anchor/transform precedence,
live casing, assertion syntax, no-heal behavior, visibility timing and
confidence/provenance. Source immutability and proposal governance are checked.
Native semantic regressions independently compile complete proposed JavaScript
and Python; a JavaScript execution trace checks conditional and callback scope.

Historical oddities remain characterized: network wording is generally
`unknown`, generic parse wording is a syntax error, and 0.2.1 changes stale
element and Cypress failure classification. Native assertion refusal takes
precedence over upstream's locator override. Native also refuses upstream
no-op successes and global comment/string replacements, corrects a malformed
test-ID transform, and supports a moved aria-label anchor where upstream has
no alternative. These are explicit deviations, not claimed parity.
The historical accessibility text-casing input remains unchanged and its
actual Python success/proposal bytes are checked as a safety deviation: native
requires an HTML element proof. A real HTML casing row compares proposals
directly. Upstream's substring text success is also preserved and labelled;
native refuses a quoted exact selector that would match no element.

`internal/contracttest/python_oracle.py` is a test-only, Go-owned oracle helper.
Isolated Python processes verify the known revision, exact Git root and clean
source tree before and after invocation. A protected finder compiles every
owned `ninelives` import directly from `.py` bytes, bypassing even valid poisoned
bytecode caches, and records the actual compiled source digests. Pre-imported,
foreign, untracked or loader-bypassed owned modules fail closed. Identity/cache
tests exercise these failures without changing the real pinned checkouts.

Set `NINELIVES_REQUIRE_CONTRACT=1`, `NINELIVES_CONTRACT_PYTHON` to the intended
Python executable/architecture wrapper, and `NINELIVES_UPSTREAM_DIR` to the
directory containing the two pinned fixture checkouts, then run:

```sh
go test -race ./... -count=1
go vet ./...
```

CI explicitly runs both actual differential suites and the adversarial oracle
identity/cache suite with required fixtures; missing fixtures cannot silently
skip that step.

## Reproducible process benchmark

`scripts/benchmark_tier1.py` measures one moved-ID fixture in fresh processes.
It requires explicit pinned `src` paths and a new output prefix:

```sh
python -I scripts/benchmark_tier1.py --repo . \
  --python /path/to/native-python --go /path/to/go \
  --upstream-0.1.3 /path/to/0.1.3/src --upstream-0.2.1 /path/to/0.2.1/src \
  --target-arch arm64 --samples 30 --seed 2151 --output-prefix /path/to/new-result
```

The release build explicitly targets the requested architecture, and executable
headers confirm it independently of the Go compiler host. Each Python sample
includes isolated startup/imports and source-only provenance checks. Results
separate the first invocation from subsequent fresh-process invocations;
filesystem cache state is uncontrolled. All Go files/module inputs, exact
tracked/untracked patch identity, upstream sources, helper/harness and release
binary are bound before/after; changes abort the run. JSON retains each raw
response and actual imported-module digests. Existing outputs are never
overwritten. This is offline process latency, not a language ratio, provider,
browser or verification benchmark.
