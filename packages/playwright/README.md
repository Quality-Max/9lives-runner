# @9l/playwright

Local Playwright fixtures for the [9lives runner](https://github.com/Quality-Max/9lives-runner).
The `n9l` fixture provides `step` and bounded natural-language `goal` methods.
Keep ordinary Playwright assertions to establish the expected behavior. A goal
completing does not verify that behavior.

## Install

Once `0.0.0` is published:

```sh
npm install --save-dev @9l/playwright@0.0.0 @playwright/test@1.61.1
npm exec playwright install chromium
```

Before the first publication, build and install a local tarball using the
[existing-project guide](https://github.com/Quality-Max/9lives-runner/blob/main/docs/run-existing-project.md).
Use Node 24 for the qualified runtime (Node 22 minimum), Playwright 1.61.1,
and Chromium. macOS and Linux on amd64/arm64 are the qualified runner targets.
Other browser engines and Playwright versions require qualification.

The Go `9l` executable is also required. Download a runner release or build it
from source with Go 1.25.13 or newer. This npm package does not install Go or
bundle the runner, browsers or a hosted service. No platform account is needed.

## Use

```ts
import {test, expect} from '@9l/playwright';

test('checkout confirms one order', async ({page, n9l}) => {
  await page.goto('http://localhost:3000/checkout');
  await n9l.step('place order', async () => {
    await page.getByRole('button', {name: 'Place order'}).click();
    await expect(page.getByRole('status')).toHaveText('Order confirmed');
  });
});
```

Start the example application, then run the selected spec through the Go engine:

```sh
9l run tests/checkout.spec.ts --sdk --format json
```

Use `--pass-env NAME` for explicitly approved application settings. Browser
handles and goal parameter values stay in the worker; provider credentials stay
in Go. The `n9l` fixture requires the engine identity supplied by `--sdk`.
Existing Playwright specs and configuration remain usable independently.

Natural-language goals use finite observed controls and typed actions, with
budgets and a conservative stop policy. See the
[goal contract](https://github.com/Quality-Max/9lives-runner/blob/main/docs/goals.md)
and [SDK bridge](https://github.com/Quality-Max/9lives-runner/blob/main/docs/sdk-bridge.md).
Live provider accuracy, independent behavioral verification, verified replay
and native mobile support are not qualified features.

## License

Apache-2.0. The package includes `LICENSE` and `NOTICE`.
