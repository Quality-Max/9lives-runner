# README demo

`docs/assets/false-green.gif`, shown at the top of the repository README,
is recorded from `project/`. That project has one test that fails its
assertion while a shared `afterEach` hook calls `test.fail()`.

1. On Playwright 1.61.1 through 1.65.1, `npx playwright test` marks the test
   failed, then reports `1 passed` and exits 0
   ([microsoft/playwright#43738](https://github.com/microsoft/playwright/issues/43738)).
2. Before the run, `9l assess` flags the `test.fail()` in the after hook.
3. After the run, `9l run --sdk` keeps Playwright's verdict, and the
   receipt says the failed test counts as expected only because
   `test.fail()` ran during it.

Every line in the recording is real command output. To record it again,
run this from the repository root after `npm ci && npm run build`, with
Chromium installed (`npm exec playwright install chromium`) and
[VHS](https://github.com/charmbracelet/vhs) on your PATH:

```sh
vhs demo/readme/false-green.tape
```

The tape builds `9l` and copies `project/` to `/tmp/9lives-demo`, so the
recording shows no local paths.
