# QUA-2151 frozen v4 correctness review — ACCEPT

No confirmed correctness blocker in this bounded review. The v3 nonexistent exact-text proposals now refuse; a genuine whole-element casing repair and supported timing repair still work. Source and patch identities match the returned freeze. This accepts the reviewed offline subset for the coordinator's next evidence step; it is not browser-wide conformance, live execution verification, visibility qualification or cutover acceptance. Benchmark measurement remains **UNRUN**.

## Independent fixture evidence

Built the frozen CLI with `GOARCH=arm64 go build -o /tmp/qua2151-v4-review-9l ./cmd/9l`. For each row below, invoked that binary with `tier1 --format json` and JSON-serialized stdin. The first22 rows implement the prepared matrix; the remaining17 are predeclared adjacent competitor, timing and prior regression controls. No adaptive expansion was needed.

Base request: version1, framework playwright, failureType locator_not_found, errorMessage `locator not found`, testCode `await page.locator(<JSON-encoded selector>).click();`. Unless explicitly shown, failedSelector is `text='save'`. Timing rows instead use failureType locator_timeout and errorMessage `waiting for locator timeout exceeded`.

Fresh headless Chrome **154.0.8037.98**, existing ARM Python Playwright runtime, JavaScript disabled, synthetic `page.set_content` only, no external navigation or existing profile. **39 CLI/browser controls: 14 proposals, all actual returned selectors count1; 25 expected refusals.** Complete proposed code passed `node --check` inside an async scenario function. Original requests/source stayed unchanged; every proposal was changed/nonempty with apply:false, requiresApproval:true and provenance:unverified.

For refusals, the table's check selector is an explicit diagnostic control, not a returned proposal. A count1 or count2 refusal is an allowed documented conservative limit. The quote/backslash refusal's diagnostic `text='Save'` does not claim the unsupported full quoted body was browser-qualified. Snapshot strings are JSON-escaped for reproducibility.

| Case | Exact snapshot string | Input selector | Decision | Browser check selector | Count |
|---|---|---|---|---|---|
| whole | `"<button>Save</button>"` | `text='save'` | propose | `text='Save'` | 1 |
| substring | `"<button>Save now</button>"` | `text='save'` | refuse | `text='Save'` | 0 |
| adjacent-block | `"<div>Sa</div><div>Ve</div>"` | `text='save'` | refuse | `text='Save'` | 0 |
| adjacent-inline | `"<span>Sa</span><span>Ve</span>"` | `text='save'` | refuse | `text='Save'` | 0 |
| nested-leaf | `"<button><span>Save</span></button>"` | `text='save'` | propose | `text='Save'` | 1 |
| mixed-split | `"<button>Sa<span>Ve</span></button>"` | `text='save'` | refuse | `text='Save'` | 0 |
| comment-split | `"<button>Sa<!-- benign -->Ve</button>"` | `text='save'` | propose | `text='SaVe'` | 1 |
| direct-around-child | `"<button>Save<span> now</span></button>"` | `text='save'` | refuse | `text='Save'` | 1 |
| qualifying-sibling | `"<button>Save now</button><button>Save</button>"` | `text='save'` | propose | `text='Save'` | 1 |
| duplicates | `"<button>Save</button><button>Save</button>"` | `text='save'` | refuse | `text='Save'` | 2 |
| whitespace | `"<button> \n Save\t now \r\n </button>"` | `text='save now'` | propose | `text='Save now'` | 1 |
| nbsp | `"<button>Save&nbsp;now</button>"` | `text='save now'` | propose | `text='Save now'` | 1 |
| unicode-entity | `"<button>Sav&#101; &#x130;🙂</button>"` | `text='save İ🙂'` | propose | `text='Save İ🙂'` | 1 |
| quotes-backslash | `"<button>Save &quot;it's&quot; &#92; ready</button>"` | `text='save "it's" \ ready'` | refuse | `text='Save'` | 0 |
| input-button | `"<input type=\"button\" value=\"Save\">"` | `text='save'` | refuse | `text='Save'` | 1 |
| input-text | `"<input type=\"text\" value=\"Save\">"` | `text='save'` | refuse | `text='Save'` | 0 |
| textarea | `"<textarea>Save</textarea>"` | `text='save'` | refuse | `text='Save'` | 1 |
| script-style | `"<script>Save</script><style>Save</style>"` | `text='save'` | refuse | `text='Save'` | 0 |
| template | `"<template><button>Save</button></template>"` | `text='save'` | refuse | `text='Save'` | 0 |
| raw-inert-real | `"<script>Save now</script><template><button>Save</button></template><button>Save</button>"` | `text='save'` | propose | `text='Save'` | 1 |
| foreign | `"<svg><text>Save</text></svg>"` | `text='save'` | refuse | `text='Save'` | 1 |
| malformed | `"<button>Save</button><script>unfinished"` | `text='save'` | refuse | `text='Save'` | 1 |
| head-noscript | `"<noscript><button>Save</button></noscript><button>Save</button>"` | `text='save'` | refuse | `text='Save'` | 2 |
| nonleaf-competitor | `"<div>Save<span>more</span></div><button>Save</button>"` | `text='save'` | refuse | `text='Save'` | 2 |
| input-competitor | `"<input type=\"submit\" value=\"Save\"><button>Save</button>"` | `text='save'` | refuse | `text='Save'` | 2 |
| textarea-competitor | `"<textarea>Save</textarea><button>Save</button>"` | `text='save'` | refuse | `text='Save'` | 2 |
| shadow-competitor | `"<div><template shadowrootmode=\"open\"><button>Save</button></template></div><button>Save</button>"` | `text='save'` | refuse | `text='Save'` | 2 |
| format-removal | `"<button>Sa&#8203;&#173;ve</button>"` | `text='save'` | propose | `text='Save'` | 1 |
| nel-not-whitespace | `"<button>Save&#133;now</button>"` | `text='save now'` | refuse | `text='Save'` | 0 |
| timing-missing | `""` | `text='save'` | refuse | `text='Save'` | 0 |
| timing-substring | `"<button>Save now</button>"` | `text='save'` | refuse | `text='Save'` | 0 |
| timing-duplicate | `"<button>Save</button><button>Save</button>"` | `text='save'` | refuse | `text='Save'` | 2 |
| timing-whole | `"<button>save</button>"` | `text='save'` | propose | `text='save'` | 1 |
| double-escaped-phantom | `"<script><!--<script></script><button id=\"old-new\">x</button></script>"` | `#old` | refuse | `#old-new` | 0 |
| double-escaped-comment-phantom | `"<script><!--<script></script><button id=\"old-new\">x</button>--></script>"` | `#old` | refuse | `#old-new` | 0 |
| double-escaped-real | `"<script><!--<script></script><button id=\"old-fake\">x</button>--></script><button id=\"old-new\">x</button>"` | `#old` | propose | `#old-new` | 1 |
| unicode-raw-real | `"<script>İ</script><button id=\"old-new\">x</button>"` | `#old` | propose | `#old-new` | 1 |
| unicode-attribute | `"<button data-testid=\"old-İ🙂\">x</button>"` | `[data-testid='old']` | propose | `[data-testid='old-İ🙂']` | 1 |
| duplicate-first-attr | `"<button id=\"old-new\" id=\"old-fake\">x</button>"` | `#old` | propose | `#old-new` | 1 |

An additional exact prior-phantom browser check used `<div>Sa</div><div>Ve</div>` and `text='SaVe'`: count0, body.innerText `"Sa\nVe"`. Whole-element `Save` remains count1. No phantom joins across siblings, substring, input/raw/nonleaf competitor, head-noscript or shadow bypass was observed.

### Exact key stdin and response bytes

All four invocations exit0. Browser checks use the actual newSelector for proposals.

**substring**

Stdin:
```json
{"version":1,"framework":"playwright","failureType":"locator_not_found","errorMessage":"locator not found","failedSelector":"text='save'","testCode":"await page.locator(\"text='save'\").click();","pageSnapshot":"<button>Save now</button>"}
```
Response:
```json
{"version":1,"failureType":"locator_not_found","failedSelector":"text='save'","selectedTier":"tier1_auto","attemptedTier":"tier1_auto","decision":"refuse","changes":[],"requiresApproval":false,"apply":false,"confidence":0,"metadata":{"engine":"native-tier1","provenance":"unverified"},"reason":"no unique supported exact text anchor"}
```

**adjacent-block**

Stdin:
```json
{"version":1,"framework":"playwright","failureType":"locator_not_found","errorMessage":"locator not found","failedSelector":"text='save'","testCode":"await page.locator(\"text='save'\").click();","pageSnapshot":"<div>Sa</div><div>Ve</div>"}
```
Response:
```json
{"version":1,"failureType":"locator_not_found","failedSelector":"text='save'","selectedTier":"tier2_ai_suggest","attemptedTier":"tier1_auto","decision":"refuse","changes":[],"requiresApproval":false,"apply":false,"confidence":0,"metadata":{"engine":"native-tier1","provenance":"unverified"},"reason":"no unique supported exact text anchor"}
```

**whole**

Stdin:
```json
{"version":1,"framework":"playwright","failureType":"locator_not_found","errorMessage":"locator not found","failedSelector":"text='save'","testCode":"await page.locator(\"text='save'\").click();","pageSnapshot":"<button>Save</button>"}
```
Response:
```json
{"version":1,"failureType":"locator_not_found","failedSelector":"text='save'","selectedTier":"tier1_auto","attemptedTier":"tier1_auto","decision":"propose","proposedCode":"await page.locator(\"text='Save'\").click();","changes":["re-found selector"],"requiresApproval":true,"apply":false,"confidence":0.85,"metadata":{"anchor":"text","engine":"native-tier1","newSelector":"text='Save'","oldSelector":"text='save'","provenance":"unverified"}}
```

**timing-whole**

Stdin:
```json
{"version":1,"framework":"playwright","failureType":"locator_timeout","errorMessage":"waiting for locator timeout exceeded","failedSelector":"text='save'","testCode":"await page.locator(\"text='save'\").click();","pageSnapshot":"<button>save</button>"}
```
Response:
```json
{"version":1,"failureType":"locator_timeout","failedSelector":"text='save'","selectedTier":"tier1_auto","attemptedTier":"tier1_auto","decision":"propose","proposedCode":"await page.locator(\"text='save'\").waitFor({ state: 'visible', timeout: 10000 });\nawait page.locator(\"text='save'\").click();","changes":["added visible wait"],"requiresApproval":true,"apply":false,"confidence":0.75,"metadata":{"anchor":"timing","engine":"native-tier1","newSelector":"text='save'","oldSelector":"text='save'","provenance":"unverified"}}
```

## Source and contract review

The new proof in `internal/healing/snapshot.go:74` obtains the HTML5 tree, applies whole-tree unsupported-context checking, normalizes immediate text runs and counts matching elements before returning a single ordinary leaf candidate. Comments merge direct runs; element children split them. Input button/submit values and matching direct runs on unsupported/nonleaf/raw elements contribute competitors. Head/script/style and inert ordinary templates do not supply text. Noscript, foreign trees and declarative shadow templates refuse before subtree filtering. Normalization uses the intended JavaScript whitespace subset and removes the documented format characters, rather than broad Unicode IsSpace.

`internal/healing/healing.go:174` requires this proof before both alternative replacement and timing. Missing, substring and duplicate timing snapshots all refuse; an exact unchanged-case timing positive emits the wait in the same supported action scope. Unquoted/chained/escaped text and non-Playwright text refusal controls pass in the focused suite. The old flattened-text helper remains present but production text proposals no longer consume it.

The five authorized v4 changes are healing.go, snapshot.go, semantic_test.go, differential_test.go and native-tier1.md. All ten other owned files—including CLI, source lexer, oracle/helper, harness, modules and CI—remain byte-identical to v3. No broader source re-review or new arbitrary-runtime-aliasing qualification is implied.

Independent focused ARM gate:
```sh
GOARCH=arm64 go test ./internal/healing -run '^Test(ExactText.*|ScriptDoubleEscapedDOMRegressions|DOMSnapshotTreeAndTextControls|DOMSnapshotMalformedAndDepthRefusals|SemanticFinalReviewV2Regressions|AssertionExpressionOwnershipBoundaries|SeparateStatementOwnershipAndUnicodeBoundaries|SupportedSourceContextsAndIndependentSyntax|TimingPreservesControlledBodyAndCallbackScope)$' -count=1
```
PASS: healing **12.172s**. It covers prior grouped/optional assertion ownership, Python logical assertions, Unicode-offset regressions, separate action/assertion positives, independent JS/Python syntax and conditional/callback timing execution trace. The CLI/browser controls separately confirm both double-escaped script phantom refusals, real siblings, Unicode attributes and first duplicate attributes.

Independent strict actual-source differential invocation:
```sh
GOARCH=arm64 NINELIVES_REQUIRE_CONTRACT=1 \
NINELIVES_CONTRACT_PYTHON=/tmp/qua2151-python-arm64 \
NINELIVES_UPSTREAM_DIR=<LOCAL_CHECKOUT>/.context/9lives-runner-live/testdata/upstream \
go test ./internal/healing ./internal/contracttest \
  -run 'TestPinnedPython(Differential|ClassifierAndStrategyCorpus|OracleIdentityAndCaches)' -count=1 -json
```
Exit0. The first raw event result was tool-truncated, so the same strict invocation was rerun with subprocess stdout captured and counted before emitting compact JSON. The complete independent count is **28 classifier/parser/strategy +27 proposals per pin =110 actual comparisons**, **nine identity/cache controls**, **zero failures/skips**. Compact run package times: healing5.004s, contracttest9.888s. Identity controls cover poisoned cache, wrong pin, nonexact source root, dirty/ignored source, preimported/foreign owned modules, loader bypass and changes after import.

The historical nonHTML accessibility input is preserved byte-for-byte and labeled intentional-safety-deviation; both actual Python successes and original proposal bytes are asserted. Added genuine HTML casing parity and upstream-success substring deviation rows run against both pinned sources. The inspected differential assertions directly call native Heal and compare actual upstream code/confidence for parity; they do not relabel native DOM controls as Python parity. The new totals replace106. The writer reports full strict six-package ARM race, vet/format and module checks; this reviewer independently reran the focused gate and complete counted strict comparisons, not the full race suite.

Documented conservative limits agree with observations: nonleaf/input/raw anchors, ambiguous case-insensitive candidates, escaped/chained bodies, foreign/noscript/shadow contexts and malformed snapshots may refuse despite a browser match. Locator existence alone does not prove visibility, identity, actionable uniqueness under future DOM changes or assertion correctness.

## Immutable freeze and next step

Read the handoff with SHA256 `775e2be759d9272133ffc371b57e2089e8f54e0373802042c346e946061de4d1`. Independently recomputed its sorted-JSON manifest self-digest, excluding that digest field, and checked all15 owned files before execution. After controls, independently enumerated **all33 Go files plus both module inputs**, matched every digest and the aggregate, and matched exact tracked/index/unstaged/untracked patch identity. Comparing v3 owned hashes confirms exactly the five authorized changed files.

| Identity | Before / after controls / after report |
|---|---|
| Frozen manifest | `b7a282535ea6d57816fded39fa12f6f07dee85c30be7bfd5d1dcff9d1086cbe5` |
| All Go/module inputs | `68c24c2eb270259cfcf75d5e49b095b3a9b742dc34c6c3d4d56f99d86db1569f` |
| Exact patch | `b2fe447dff884334f9e9b108f8b543927c07c202b440b5cbffda098e6fe78aa5` |

New healing.go digest: `4ca59d14435fe2f44dd2541ccd4c9b50fe24452c150e303d60bc861ae6c112d1`; snapshot.go: `b3a2ac44589604fa4e9e1545f9da2c54b4a315a352800728bb9db8b9757dc37f`. All remaining exact digests are bound by the checked handoff manifest.

Only this review artifact was written. No source/test/harness edits, staging, commits, review triggers, provider/credential access, external target, dependency scan or benchmark occurred. The benchmark harness was imported only for read-only source/patch identity functions; its measurement entry point was never invoked. Root owns the separate fresh official dependency scan and later benchmark. The correctness review permits proceeding to those remaining evidence steps while preserving this frozen source.
