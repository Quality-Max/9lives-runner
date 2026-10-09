# @9l/playwright

Local Playwright fixtures for the [9lives runner](https://github.com/Quality-Max/9lives-runner).
The `n9l` fixture provides `step` and bounded natural-language `goal` methods.
Keep ordinary Playwright assertions to establish the expected behavior. A goal
completing does not verify that behavior.

## Install

Published on [npm](https://www.npmjs.com/package/@9l/playwright):

```sh
npm install --save-dev @9l/playwright@0.1.1 @playwright/test@1.64.0
npm exec playwright install chromium
```

Use Node 24 for the qualified runtime (Node 22 minimum), Playwright 1.61.1
through 1.64.0, and Chromium. macOS and Linux on amd64/arm64 are the qualified
runner targets. The peer range is `@playwright/test` `>=1.61.1 <2`; CI qualifies
every published release from 1.61.1 through 1.64.0 (1.61.1, 1.62.0, 1.62.1,
1.63.0 and 1.64.0). Later versions, and other browser engines, install but are
unqualified.

The Go `9l` executable is also required. Use the
[CLI installation guide](https://github.com/Quality-Max/9lives-runner/blob/main/docs/install.md)
for release availability or install from source with Go 1.25.13 or newer.
This npm package does not install Go or
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
