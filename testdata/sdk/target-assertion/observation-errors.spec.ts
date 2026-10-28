import {test, expect} from '@playwright/test';
import {TargetApp, targetCapabilities, type TargetDriver} from '@9l/playwright';

function session(read: () => Promise<string>, absent = false) {
  const driver: TargetDriver = {
    kind: 'android-fake', appId: 'assertion', version: '9l.target/1', capabilities: targetCapabilities,
    open: async () => {}, reset: async () => {}, close: async () => {},
    observeTestId: async () => absent ? [] : [{
      tap: async () => {}, fill: async () => {}, visible: async () => true,
      state: async () => true, text: read,
    }],
  };
  return new TargetApp(driver, driver, {deadlineMs: Date.now() + 2000});
}

test('unavailable reads cannot claim a completed business mismatch', async () => {
  let reads = 0;
  const app = session(async () => { reads++; throw new Error('synthetic unavailable read'); });
  try {
    await expect(app.getByTestId('receipt').toHaveText('wanted', 100)).rejects.toThrow('operation_failed');
    expect(reads).toBeGreaterThan(1);
  } finally { await app.close(); }
});

test('a completed mismatch still rejects when later reads fail', async () => {
  let reads = 0;
  const app = session(async () => {
    if (++reads === 1) return 'wrong';
    throw new Error('synthetic unavailable read');
  });
  try { await expect(app.getByTestId('receipt').toHaveText('wanted', 100)).rejects.toThrow('assertion_failed'); }
  finally { await app.close(); }
});

test('observed absence rejects the target assertion', async () => {
  const app = session(async () => 'unused', true);
  try { await expect(app.getByTestId('receipt').toHaveText('wanted', 100)).rejects.toThrow('assertion_failed'); }
  finally { await app.close(); }
});
