import {createTargetTest, expect} from '@9l/playwright';
import {fakeFinance} from './fake-finance';

const mode = process.env.NINELIVES_TARGET_CASE ?? 'healthy';
const test = createTargetTest({
  web: {
    open: async page => { await page.setContent(`<input data-testid="amount"><button data-testid="send" onclick="document.querySelector('[data-testid=receipt]').textContent='Sent $'+document.querySelector('input').value">Send</button><output data-testid="receipt"></output>`); },
    reset: async page => { await page.evaluate(() => { document.querySelector('input')!.value = ''; document.querySelector('output')!.textContent = ''; }); },
  },
  native: target => fakeFinance(target, mode),
}).extend({
  // Executed tripwires: a native fixture must not implicitly resolve any
  // Playwright browser fixture. Web still uses the ordinary page fixture.
  ...(process.env.NINELIVES_TARGET?.startsWith('android-fake:') ? {
    page: async () => { throw new Error('native requested page'); },
    context: async () => { throw new Error('native requested context'); },
    browser: async () => { throw new Error('native requested browser'); },
  } : {}),
});

// @9l-requirement QUA-2203
test('one deterministic scenario dispatches to both targets', async ({app}) => {
  await app.open();
  await app.reset();
  await app.getByTestId('amount').fill('5');
  await app.getByTestId('send').toHaveState('enabled', true);
  if (mode === 'cancel') setTimeout(() => app.cancel(), 25);
  if (mode === 'deadline') {
    // Start a mutation across the Go-owned deadline, after real setup ran.
    await new Promise(resolve => setTimeout(resolve, Math.max(0, Number(process.env.NINELIVES_TARGET_DEADLINE_MS) - Date.now() - 100)));
  }
  // A short operation bound must cancel the pending mutation inside the worker.
  await app.getByTestId('send').tap(mode === 'timeout' ? {timeoutMs: 100} : {});
  await app.getByTestId('receipt').toBeVisible();
  // @9l-outcome exact-receipt
  await app.getByTestId('receipt').toHaveText('Sent $5');
});

test('caught failures cannot produce successful evidence', async ({app}) => {
  await app.open();
  await app.getByTestId('receipt').toHaveText('impossible', 40).catch(() => {});
  await expect(app.getByTestId('send').tap()).rejects.toThrow('9lives target stopped:');
});

test('a step named like a target assertion is not one', async ({app}) => {
  await app.open();
  await test.step('9l target assert-visible', async () => {});
});

test('skipped on the native target', async ({app}) => {
  test.skip(process.env.NINELIVES_TARGET?.startsWith('android-fake:'), 'web only');
  await app.open();
  await app.getByTestId('send').toBeVisible();
});
