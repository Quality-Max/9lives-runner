# The demo at the top of the repository README. A test fails, Playwright
# reports "1 passed" and exits 0; 9lives flags it before and after the run.
#
# Record from the repository root after `npm ci && npm run build`, with
# Chromium installed:
#
#   vhs demo/readme/false-green.tape
#
# The project is copied to /tmp/9lives-demo, so no local path is recorded.

Output docs/assets/false-green.gif

Require go
Require npx

Set Shell "bash"
Set FontSize 16
Set Width 1000
Set Height 600
Set Padding 24
Set Theme "GitHub Dark"
Set WindowBar Colorful
Set BorderRadius 8
Set Framerate 20
Set TypingSpeed 35ms

Hide
Type `go build -o /tmp/9lives-demo-bin/9l ./cmd/9l && rm -rf /tmp/9lives-demo && cp -R demo/readme/project /tmp/9lives-demo && ln -s "$PWD/node_modules" /tmp/9lives-demo/node_modules && export PATH="/tmp/9lives-demo-bin:$PATH" && cd /tmp/9lives-demo && PS1='$ ' && clear`
Enter
Wait@120s /^\$\s*$/
Show

# 1. The test fails, and Playwright reports it green.
Type "cat tests/checkout.spec.ts"
Sleep 500ms
Enter
Sleep 2.5s
Type `npx playwright test; echo "exit code: $?"`
Sleep 500ms
Enter
Wait@60s /^\$\s*$/
Sleep 4s

# 2. Before it runs: 9l assess names the cause.
Hide
Type "clear"
Enter
Show
Type "9l assess tests/checkout.spec.ts"
Sleep 500ms
Enter
Wait@60s /^\$\s*$/
Sleep 6s

# 3. After it runs: the receipt says why the run is green.
Hide
Type "clear"
Enter
Show
Type "9l run tests/checkout.spec.ts --sdk"
Sleep 500ms
Enter
Wait@90s /^\$\s*$/
Sleep 6s
