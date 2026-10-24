import {test, expect} from '@playwright/test';
import {TargetApp, targetCapabilities, type TargetDriver, type TargetContext} from '@9l/playwright';
import {setTimeout as delay} from 'node:timers/promises';

function appWithText(read: (context: TargetContext) => Promise<string>, lifetime = 2000) {
  const driver: TargetDriver = {
    kind: 'android-fake', appId: 'assertion', version: '9l.target/1', capabilities: targetCapabilities,
    open: async () => {}, reset: async () => {}, close: async () => {},
    observeTestId: async () => [{
      tap: async () => {}, fill: async () => {}, visible: async () => true,
      state: async () => true, text: read,
    }],
  };
  return new TargetApp(driver, driver, {deadlineMs: Date.now() + lifetime});
}
const blocked = async (context: TargetContext): Promise<string> => {
  await delay(1000, undefined, {signal: context.signal});
  return 'late';
};

test('a completed false observation rejects the assertion at its polling bound', async () => {
  let reads = 0;
  const app = appWithText(context => ++reads === 1 ? Promise.resolve('wrong') : blocked(context));
  try {
    await expect(app.getByTestId('receipt').toHaveText('wanted', 100)).rejects.toThrow('assertion_failed');
    expect(reads).toBeGreaterThan(1);
  } finally { await app.close(); }
});

test('a deadline before any completed observation stays inconclusive', async () => {
  const app = appWithText(blocked);
  try { await expect(app.getByTestId('receipt').toHaveText('wanted', 100)).rejects.toThrow('deadline'); }
  finally { await app.close(); }
});

test('cancellation after a false observation keeps cancellation', async () => {
  let reads = 0;
  const app = appWithText(async context => {
    if (++reads === 1) return 'wrong';
    app.cancel();
    return blocked(context);
  });
  try { await expect(app.getByTestId('receipt').toHaveText('wanted', 100)).rejects.toThrow('canceled'); }
  finally { await app.close(); }
});

test('the outer deadline after a false observation keeps the outer deadline', async () => {
  let reads = 0;
  const app = appWithText(context => ++reads === 1 ? Promise.resolve('wrong') : blocked(context), 100);
  try { await expect(app.getByTestId('receipt').toHaveText('wanted', 1000)).rejects.toThrow('deadline'); }
  finally { await app.close(); }
});
