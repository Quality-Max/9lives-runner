# QUA-2151 semantic repair v4 frozen handoff

V4 implementation and local checks are complete; independent acceptance is pending. The sole source writer lease is returned with this frozen handoff. No staging, commit, push, benchmark measurement, provider call, credential access, external target, bridge/distribution change or cutover occurred.

## Confirmed red evidence and repair

Read `.context/qua2151-semantic-v4-repair-brief.md`, updated capsule and exact `.context/qua2151-semantic-v3-review.md` before editing. Original production cause was `healing.go` 247–249: a case-insensitive substring match over document-concatenated `htmlVisibleText` emitted a quoted exact Playwright selector. Genuine red `TestExactTextReviewRegressions` had exactly two failures: `<button>Save now</button>` proposed `text='Save'`, and adjacent `<div>Sa</div><div>Ve</div>` proposed `text='SaVe'`. Browser counts were both zero.

Attributes still use the unchanged mature DOM extraction. Text proposals now use a separate bounded element proof: JS-compatible normalization of immediate text runs, merged across comments but split by child elements; one eligible ordinary leaf element; live casing preserved. Matching nonleaf direct runs, input button/submit values and raw-text elements count as competitors without supplying anchors. Head/script/style and ordinary inert templates are excluded. Whole-tree unsupported-context checking precedes candidate filtering: an initial noscript inside the implicit head originally bypassed the filter, emitted `text='Save'`, and browser count was two; the new precheck refuses noscript, foreign trees and declarative shadow templates regardless of their position. No production source now consumes document-concatenated text for proposals.

A further genuine red `TestExactTextTimingCannotBypassProof` had exactly three failures (missing snapshot, substring and duplicate text). All returned `Decision:propose`, `anchor:timing`, confidence 0.75 and these bytes:

```js
await page.locator("text='save'").waitFor({ state: 'visible', timeout: 10000 });
await page.locator("text='save'").click();
```

`Heal` now requires the text element proof before any text alternative or timing return. Unsupported `text=` syntax and non-Playwright text locators refuse. The three failures now pass; a supported exact-match timing positive and escaped/chained/unquoted refusal controls also pass. Existing assertion ownership, separate assertion/action support, source lexer, conditional/callback execution trace, CLI/schema, source-only oracle, harness, CI and modules are byte-identical to v3.

## Changed scope and limitations

Exactly five files differ from v3: `healing.go`, `snapshot.go`, `semantic_test.go`, `differential_test.go`, `docs/native-tier1.md`. All ten other declared owned files and all 31 other Go/module inputs are unchanged. The installed Python bridge and its distribution remain untouched.

The text subset is deliberately conservative: quoted legacy Playwright text with a unique normalized leaf match in complete supported HTML. NonHTML accessibility input, nonleaf/input anchors, quote/backslash/chained bodies, foreign trees, declarative shadow trees, noscript, ambiguous matches and malformed snapshots refuse. Multiple case-insensitive matches refuse even if one emitted case-sensitive selector could be unique. Some valid browser selectors therefore remain unsupported; refusal fixtures are not all zero-match fixtures. This proves a bounded document text match, not visibility, application identity, runtime DOM stability, general browser conformance, assertion correctness or cutover qualification. Previous JavaScript/Python source limits remain as documented.

The historical accessibility corpus input and its actual Python successes are preserved unchanged and labeled `intentional-safety-deviation`; exact upstream proposal bytes are checked. Added a real-HTML casing parity row and an upstream-success substring deviation row, without normalizing Python output. Corpus dimensions/labels remain actual native classifier/parser/strategy/proposal comparisons.

## Validation

Native ARM64 Python 3.11.8; repository-selected compiler `go1.25.13 darwin/amd64`; explicit `GOARCH=arm64` target. The rebuilt fixture CLI executable header independently reports ARM64. Chrome 154.0.8037.98 checked 32 literal synthetic DOM fixtures with JavaScript disabled and no external navigation: all 13 supported native proposals matched exactly one element. Substring and sibling phantom counts were zero; duplicate, parent/child, input, raw-text, shadow and noscript competitors were characterized. Noscript count two now refuses. Unicode source was literal in both permanent controls and the corrected fixture harness; the initial harness's escaped-identifier refusal was not a production defect. Raw requests/responses/counts and precheck red history are retained in the hashed temporary artifacts below for coordinator preservation.

Commands executed from the runner repository unless stated otherwise:

```sh
# Actual pin/source-only fixtures were required for every Go test invocation.
GOARCH=arm64 NINELIVES_REQUIRE_CONTRACT=1 \
NINELIVES_CONTRACT_PYTHON=/tmp/qua2151-python-arm64 \
NINELIVES_UPSTREAM_DIR=<LOCAL_CHECKOUT>/.context/9lives-runner-live/testdata/upstream \
go test -race ./... -count=1

# Same explicit environment, counted actual comparison and identity suite.
GOARCH=arm64 NINELIVES_REQUIRE_CONTRACT=1 \
NINELIVES_CONTRACT_PYTHON=/tmp/qua2151-python-arm64 \
NINELIVES_UPSTREAM_DIR=<LOCAL_CHECKOUT>/.context/9lives-runner-live/testdata/upstream \
go test ./internal/healing ./internal/contracttest \
  -run 'TestPinnedPython(Differential|ClassifierAndStrategyCorpus|OracleIdentityAndCaches)' \
  -count=1 -json > /tmp/qua2151-v4-contract-tests.jsonl

GOARCH=arm64 go vet ./...
go mod verify
go list -m all
gofmt -l .
git diff --check
git diff --exit-code HEAD -- internal/healingbridge
GOARCH=arm64 go build -o /tmp/qua2151-v4-text-cli ./cmd/9l
/tmp/qua2151-python-arm64 -I /tmp/qua2151-v4-browser-check.py
```

Full strict race PASS all six packages: CLI 1.416s, adapter 1.724s, contracttest 5.578s, healing 6.856s, bridge 2.388s, runner 28.314s. Counted strict suites PASS 28 classifier/parser/strategy +27 proposals for each pin, **110 actual native comparisons +9 identity/cache tests**, zero failures/skips. Focused exact-text/source/syntax positives PASS, including realistic Playwright separate assertion/action, Cypress get/find and Selenium code/docstrings. Existing runtime conditional/callback trace passes in the full suite. Vet, module verify/list, gofmt and whitespace PASS. All 12 untracked files also passed `git diff --no-index --check /dev/null <path>` (no diagnostics).

From workspace root, `scripts/conductor-python -m ruff check` and `ruff format --check` on both unchanged `internal/contracttest/python_oracle.py` and `scripts/benchmark_tier1.py` PASS. Module graph remains `x/net v0.58.0`, `x/crypto v0.55.0`, `x/sys v0.47.0`, `x/term v0.45.0`, `x/text v0.41.0`; module bytes are unchanged. Root owns the fresh official dependency scan after freeze. Prior scan is historical; no fresh vulnerability-free claim is made here.

## Frozen manifest

All 15 declared owned files, all 33 Go files plus both module inputs, exact tracked/index/unstaged/untracked patch identity, helper/harness, clean pinned Python source identities and evidence digests are bound below. Manifest digest hashes sorted JSON before adding its own digest; report self-digest is excluded. The same source/patch identity is rechecked after writing the handoff. The benchmark harness was imported only for read-only identity helpers; its measurement entry point was never invoked. Independent review must accept the freeze before root performs any benchmark sample.

```json
{
  "all_go_and_module_inputs": {
    "cmd/9l/main.go": "f9205290c5c5019c83a397fbf9b7ab444470ed3f8f6b232b6a70c2f67c2f783f",
    "cmd/9l/main_test.go": "e27b0b17a75079b245caf3d046f1047c5cd38d7f79fb5a0e918914a65e4b12a0",
    "cmd/9l/passenv_test.go": "76357d42345d686b6243aafeb2e7f98b75efb3629ad0645b077129f821801769",
    "cmd/9l/tier1_test.go": "5e004625d0eaa79ea49d5f4c8099643d09bfc9ba31250581ee93ece126f3b65f",
    "go.mod": "e2bd8eeb8c090f9baf91d6a63284c1d4ed4b1b9246a9e26843090548bfe19e1b",
    "go.sum": "b59067d9db8cb2654be364ec1c7da3f6a92953f61a561d312ea4c14a7048a46d",
    "internal/adapters/playwright/playwright.go": "471fc87386dbdc172070a2da3ba3a2720046cd60f83ebd73e6a69e12f899e012",
    "internal/adapters/playwright/playwright_test.go": "9153d52457f6ae9bb88b0917e86971ca1cb83797d98dbee0e24cade39c00144a",
    "internal/contracttest/contracttest.go": "7f2f6481f2369b9a03c621161e7869bee87f1621d93f4efc269cd979039f3815",
    "internal/contracttest/python_oracle_test.go": "e582a5444e91b5137f3407fc57751d45ca29d17ebc5ae6069e12a3d36f1b339c",
    "internal/healing/differential_test.go": "61d18c9dfcf36bc35c8999a563adc3f194a5aa4d0a0c736d4e22ea696e6b5ac1",
    "internal/healing/healing.go": "4ca59d14435fe2f44dd2541ccd4c9b50fe24452c150e303d60bc861ae6c112d1",
    "internal/healing/healing_test.go": "6af4660b4e7dca1ac39cb3ea0939acd014f84ef108d38809483ad640c7fa9a9e",
    "internal/healing/semantic_test.go": "10f90f971427a4b47255ca96adcfa3178be94384dad7a1aa425fe73eb740d8ac",
    "internal/healing/snapshot.go": "b3a2ac44589604fa4e9e1545f9da2c54b4a315a352800728bb9db8b9757dc37f",
    "internal/healing/source.go": "e09ab11a21f3351539bd0489ec0d9395d7bfbf431d9b6ac44e145b0549466bbd",
    "internal/healingbridge/contract.go": "65a8d757de74e8aec4e778bd001b15742c7c99bac5c3cb9341acca13311ce4c7",
    "internal/healingbridge/contract_test.go": "4e49ee59c6cb577c2272f58ce2fdc5025728a58afc5faf696a9403f6ecbbbd68",
    "internal/healingbridge/upstream.go": "095908270971f87a3f4db12481af1f261c131f929c04c09be3a8d6c4991c7f97",
    "internal/runner/adapter.go": "ee2ca16215097d556508ad4cf6fb175685bbd4bdddcc055b2057d90614918c7d",
    "internal/runner/canonical.go": "955492507201b8b663062fb290944eebeb409b4c7c5539f2b23d68d28452766e",
    "internal/runner/execute.go": "7fa40bbe96cfe52c9102fca4a95199ff24238e8fec6781e24d0e27921c164cc5",
    "internal/runner/fake_adapter_test.go": "c6d20adb8966aacbfc64a527f9a943356ca01a8b996db39fe94ac7eaafd58f31",
    "internal/runner/followup_test.go": "13d1910e29d22d3aac960327167ff79202ca77e0b43a18b161980e6ef05b7cbe",
    "internal/runner/model.go": "560d8800393fdaf8821f15fcf8f8832d9015e8212f8498bc1f96a2799c825f5f",
    "internal/runner/plan.go": "ce5326e0fc23ce1a3fc23be59e21ff6ccfa751c28efbf8c5be9791e341b20a71",
    "internal/runner/ports.go": "f535b77fa46232a03e47025d937822d62d80d21376f38a4ed1236f5723775eb8",
    "internal/runner/process_other.go": "df06dd027d7d31caf7ed935c275427c8d72f8ba10c21ca15ab89ce1347a29f65",
    "internal/runner/process_unix.go": "9c050ee53525ec9a26ab50aef8b5616f97436130e0f64f41f50a2de619743b04",
    "internal/runner/process_unix_test.go": "f8e7d8e97f1575035c0ef310fcbaafc07bdd80dac08142d02f8d6989de4b6351",
    "internal/runner/python.go": "0359962533b64605c38c7170e30dd0cd960ec98afb434b3b77936362a3c7c3ad",
    "internal/runner/receipt.go": "d929a4d2522baa52546ac8cb23ef93c99576e758af4643f491294f65370e936e",
    "internal/runner/runner_bench_test.go": "84a0bcc3e59fcdb3e9b08f999ab8fea08f004ef7a84097acb57ee9f90d034baa",
    "internal/runner/runner_test.go": "f88a23bb2b870bfa5448e42d014efc050f2af728b4a6fe13e7d3bf4a05503047",
    "internal/runner/state.go": "f9652c4be5ac1374bd1a2d1ccfdd80c9694bb0234ef7307f2424fb0e89b143dd"
  },
  "all_go_and_module_inputs_sha256": "68c24c2eb270259cfcf75d5e49b095b3a9b742dc34c6c3d4d56f99d86db1569f",
  "artifacts": {
    "/tmp/qua2151-v4-browser-check.py": "6ab8f712a9a7de80f6d747be3f48718f212ff95f5da9926d9cf4d99610a19fbd",
    "/tmp/qua2151-v4-browser-results-prerepair.json": "d010b05f87633be162dac3f1df9f8954d35f6254e67e43008f4414931d1a336e",
    "/tmp/qua2151-v4-browser-results.json": "53210709a5e1277b3a21ae67723afb3e71895efc05cb31ca3f49dbeab4203f1e",
    "/tmp/qua2151-v4-contract-tests.jsonl": "09d41b34c67c5c0479ec46b0516ddc3fa122e9048934d0e398c0b5de22d30649",
    "/tmp/qua2151-v4-text-cli": "d03c02a65867b5194aff095a2706ad613a7e834ea5698dd5ce06f3781ba19b36"
  },
  "branch": "feature/qua-2151-native-tier1",
  "browser": {
    "counts": {
      "accessibility-snapshot": 0,
      "case-variant-competitor": 1,
      "comments-do-not-split": 1,
      "double-escaped-script-skipped": 1,
      "duplicate-input-attribute-first": 2,
      "duplicate-text": 2,
      "escaped-text-unsupported": 1,
      "head-only": 0,
      "head-skipped": 1,
      "input-button-only": 1,
      "input-submit-competitor": 2,
      "malformed-after-text": 1,
      "nbsp-entity": 1,
      "nel-is-not-js-whitespace": 0,
      "nested-leaf": 1,
      "nonleaf-direct-run": 1,
      "noscript-setting-unsupported": 2,
      "parent-child-competitors": 2,
      "removed-format-characters": 1,
      "script-skipped": 1,
      "shadow-tree-unsupported": 2,
      "sibling-concatenation": 0,
      "split-child-text": 0,
      "style-skipped": 1,
      "substring": 0,
      "template-skipped": 1,
      "textarea-competitor": 2,
      "textarea-only": 1,
      "unicode-entity": 1,
      "unrelated-siblings": 1,
      "whitespace": 1,
      "whole-button": 1
    },
    "external_navigation": false,
    "fixture_count": 32,
    "javascript_enabled": false,
    "supported_proposal_count": 13,
    "supported_proposal_counts_all_one": true,
    "version": "154.0.8037.98"
  },
  "changed_from_v3": [
    "docs/native-tier1.md",
    "internal/healing/differential_test.go",
    "internal/healing/healing.go",
    "internal/healing/semantic_test.go",
    "internal/healing/snapshot.go"
  ],
  "context_harness_sha256": "a6c6791da899136d9ac57d66576f4b1d58450e56d06fb6d5a3d34ad2670854b7",
  "frozen_manifest_sha256": "b7a282535ea6d57816fded39fa12f6f07dee85c30be7bfd5d1dcff9d1086cbe5",
  "head": "75c675b242c007cce657cd58671ae191646fd18f",
  "identity_rows": 9,
  "owned_file_sha256": {
    ".github/workflows/ci.yml": "c9d6f0a6566666a41eda7edcbc2387723040d3b965bbc6e4aa0d5697d352d5c4",
    "cmd/9l/main.go": "f9205290c5c5019c83a397fbf9b7ab444470ed3f8f6b232b6a70c2f67c2f783f",
    "cmd/9l/tier1_test.go": "5e004625d0eaa79ea49d5f4c8099643d09bfc9ba31250581ee93ece126f3b65f",
    "docs/native-tier1.md": "059aa410682daa3b7e6fb3c5f383c40f77587af69cf1e77abf2bf4a54bbc882e",
    "go.mod": "e2bd8eeb8c090f9baf91d6a63284c1d4ed4b1b9246a9e26843090548bfe19e1b",
    "go.sum": "b59067d9db8cb2654be364ec1c7da3f6a92953f61a561d312ea4c14a7048a46d",
    "internal/contracttest/python_oracle.py": "0bc11a38052bc0ad9bc65e20ef8610fa6c4f54c8bd0d3186ca958ba54119edd0",
    "internal/contracttest/python_oracle_test.go": "e582a5444e91b5137f3407fc57751d45ca29d17ebc5ae6069e12a3d36f1b339c",
    "internal/healing/differential_test.go": "61d18c9dfcf36bc35c8999a563adc3f194a5aa4d0a0c736d4e22ea696e6b5ac1",
    "internal/healing/healing.go": "4ca59d14435fe2f44dd2541ccd4c9b50fe24452c150e303d60bc861ae6c112d1",
    "internal/healing/healing_test.go": "6af4660b4e7dca1ac39cb3ea0939acd014f84ef108d38809483ad640c7fa9a9e",
    "internal/healing/semantic_test.go": "10f90f971427a4b47255ca96adcfa3178be94384dad7a1aa425fe73eb740d8ac",
    "internal/healing/snapshot.go": "b3a2ac44589604fa4e9e1545f9da2c54b4a315a352800728bb9db8b9757dc37f",
    "internal/healing/source.go": "e09ab11a21f3351539bd0489ec0d9395d7bfbf431d9b6ac44e145b0549466bbd",
    "scripts/benchmark_tier1.py": "a6c6791da899136d9ac57d66576f4b1d58450e56d06fb6d5a3d34ad2670854b7"
  },
  "patch_identity": {
    "exact_patch_and_untracked_sha256": "b2fe447dff884334f9e9b108f8b543927c07c202b440b5cbffda098e6fe78aa5",
    "head": "75c675b242c007cce657cd58671ae191646fd18f",
    "index_patch_sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
    "status_sha256": "44b480c2851a4f8af30834e2871b4735f2c01df9ee440f23ba1eb032207b1fab",
    "tracked_patch_sha256": "87cf46eb17baef9432b1c5fda9da91296928d84fb60a8ffa78e0d3b6a2b1eb3c",
    "unstaged_patch_sha256": "87cf46eb17baef9432b1c5fda9da91296928d84fb60a8ffa78e0d3b6a2b1eb3c",
    "untracked": {
      "cmd/9l/tier1_test.go": {
        "mode": 33188,
        "sha256": "5e004625d0eaa79ea49d5f4c8099643d09bfc9ba31250581ee93ece126f3b65f"
      },
      "docs/native-tier1.md": {
        "mode": 33188,
        "sha256": "059aa410682daa3b7e6fb3c5f383c40f77587af69cf1e77abf2bf4a54bbc882e"
      },
      "go.sum": {
        "mode": 33188,
        "sha256": "b59067d9db8cb2654be364ec1c7da3f6a92953f61a561d312ea4c14a7048a46d"
      },
      "internal/contracttest/python_oracle.py": {
        "mode": 33188,
        "sha256": "0bc11a38052bc0ad9bc65e20ef8610fa6c4f54c8bd0d3186ca958ba54119edd0"
      },
      "internal/contracttest/python_oracle_test.go": {
        "mode": 33188,
        "sha256": "e582a5444e91b5137f3407fc57751d45ca29d17ebc5ae6069e12a3d36f1b339c"
      },
      "internal/healing/differential_test.go": {
        "mode": 33188,
        "sha256": "61d18c9dfcf36bc35c8999a563adc3f194a5aa4d0a0c736d4e22ea696e6b5ac1"
      },
      "internal/healing/healing.go": {
        "mode": 33188,
        "sha256": "4ca59d14435fe2f44dd2541ccd4c9b50fe24452c150e303d60bc861ae6c112d1"
      },
      "internal/healing/healing_test.go": {
        "mode": 33188,
        "sha256": "6af4660b4e7dca1ac39cb3ea0939acd014f84ef108d38809483ad640c7fa9a9e"
      },
      "internal/healing/semantic_test.go": {
        "mode": 33188,
        "sha256": "10f90f971427a4b47255ca96adcfa3178be94384dad7a1aa425fe73eb740d8ac"
      },
      "internal/healing/snapshot.go": {
        "mode": 33188,
        "sha256": "b3a2ac44589604fa4e9e1545f9da2c54b4a315a352800728bb9db8b9757dc37f"
      },
      "internal/healing/source.go": {
        "mode": 33188,
        "sha256": "e09ab11a21f3351539bd0489ec0d9395d7bfbf431d9b6ac44e145b0549466bbd"
      },
      "scripts/benchmark_tier1.py": {
        "mode": 33188,
        "sha256": "a6c6791da899136d9ac57d66576f4b1d58450e56d06fb6d5a3d34ad2670854b7"
      }
    }
  },
  "protected_go_and_module_inputs_unchanged_count": 31,
  "protected_owned_unchanged_count": 10,
  "repo": "<LOCAL_CHECKOUT>/.context/9lives-runner-pr2",
  "strict_counts": {
    "0.1.3": {
      "classifier_parser_strategy": 28,
      "proposals": 27
    },
    "0.2.1": {
      "classifier_parser_strategy": 28,
      "proposals": 27
    }
  },
  "strict_failures": 0,
  "strict_skips": 0,
  "upstream": {
    "0.1.3": {
      "path": "<LOCAL_CHECKOUT>/.context/9lives-runner-live/testdata/upstream/ninelives-0.1.3/src",
      "revision": "8a40d8d5c83f27f84384f060aeed74a3ded7ab77",
      "runtime_version": "0.1.0",
      "tracked_source_count": 17,
      "tracked_source_manifest_sha256": "34a203794da487d5c123eb818fe1dde3cc3dade31f15d7bbe2c222ca9afeb1bf"
    },
    "0.2.1": {
      "path": "<LOCAL_CHECKOUT>/.context/9lives-runner-live/testdata/upstream/ninelives-0.2.1/src",
      "revision": "568c7a6882441c13cdb9bfe8c0190ca0bf7d8240",
      "runtime_version": "0.2.1",
      "tracked_source_count": 25,
      "tracked_source_manifest_sha256": "dca7b888ceccd8c09757e3d6e34a41b5a005ff66b06bdeab5bc6bb6b950b15d5"
    }
  }
}
```
