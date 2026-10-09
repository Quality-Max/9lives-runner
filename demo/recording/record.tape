# Regenerate demo/9l-demo.gif from the repository root, after `npm ci` and
# `npm exec playwright install chromium`:
#
#   vhs demo/recording/record.tape

Output demo/9l-demo.gif
Require go
Require jq

Set Shell bash
Set FontSize 14
Set Width 1660
Set Height 440
Set Padding 20
Set TypingSpeed 25ms
Set Theme "GitHub Dark"

Hide
Type "source demo/recording/setup.sh" Enter
Sleep 4s
Show

Type "# 1. Static review: a fixed sleep before an absence check" Enter
Type "cat tests/dialog.spec.ts" Enter
Sleep 2s
Type "9l assess tests/dialog.spec.ts --titles | grep -v '^Limit'" Enter
Sleep 5s
Type "clear" Enter

Type "# 2. Requirement mapping: which test protects which outcome" Enter
Type "9l assess tests/checkout.spec.ts --requirements requirements.json --titles | grep -v '^Limit'" Enter
Sleep 6s
Type "clear" Enter

Type "# 3. Real Chromium run, then the same tests with a planted defect" Enter
Type "9l run tests/checkout.spec.ts" Enter
Wait+Screen@60s /PASSED/
Sleep 2s
Type "# Defect: confirmation is shown but the order is discarded" Enter
Type "NINELIVES_ASSESS_CONTROL=missing-order 9l run tests/checkout.spec.ts --pass-env NINELIVES_ASSESS_CONTROL" Enter
Wait+Screen@60s /FAILED:/
Sleep 1s
Type "echo exit=$?  # 0 passed, 1 failed, 3 incomplete" Enter
Sleep 2s
Type "last-run-tests" Enter
Sleep 2s
Type "# Banner checks still pass; only the order-count assertion catches it, and the run is not green." Enter
Sleep 8s
