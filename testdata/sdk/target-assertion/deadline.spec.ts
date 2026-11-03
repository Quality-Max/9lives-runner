import {test, expect} from '@playwright/test';
import {TargetApp, targetCapabilities, type TargetDriver, type TargetContext} from '@9l/playwright';
import {setTimeout as delay} from 'node:timers/promises';

function appWithText(read: (context: TargetContext) => Promise<string>, lifetime = 2000) {
  const driver: TargetDriver = {
    kind: 'android-fake', appId: 'assertion', version: '9l.target/2', capabilities: targetCapabilities,
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

// The assertion's own bound names itself; deadline is reserved for the attempt's.
test('a deadline before any completed observation stays inconclusive', async () => {
  const app = appWithText(blocked);
  try { await expect(app.getByTestId('receipt').toHaveText('wanted', 100)).rejects.toThrow('operation_timeout'); }
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

// Read times vary: one slow read early (300 ms), fast ones after, and a slower
// one (400 ms) once 600 ms have passed. The first read leaves room to poll on,
// so only a budget that remembers the slowest read stops before that late
// read; one on the last read alone, with or without margin, starts it near
// the end, overruns the bound and releases the Android driver.
test('a failing assertion with uneven read times ends before its bound and keeps the Android driver', async () => {
  let closed = 0, reads = 0;
  const started = Date.now();
  const read = async (context: TargetContext) => {
    reads++;
    await delay(reads === 1 ? 300 : Date.now() - started >= 600 ? 400 : 50, undefined, {signal: context.signal});
    return 'wrong';
  };
  const driver: TargetDriver = {
    kind: 'android', appId: 'assertion', version: '9l.target/2', capabilities: targetCapabilities,
    open: async () => {}, reset: async () => {}, close: async () => { closed++; },
    observeTestId: async () => [{tap: async () => {}, fill: async () => {}, visible: async () => true, state: async () => true, text: read}],
  };
  const app = new TargetApp(driver, driver, {deadlineMs: Date.now() + 5000});
  try {
    await expect(app.getByTestId('receipt').toHaveText('wanted', 1000)).rejects.toThrow('assertion_failed');
    expect(Date.now() - started).toBeLessThan(1000);
    expect(reads).toBeGreaterThan(1);
    expect(closed).toBe(0);
  } finally { await app.close(); }
});

test('an Android operation deadline closes the driver even when abort settles its transport', async () => {
  let closed = 0;
  const driver: TargetDriver = {
    kind: 'android', appId: 'assertion', version: '9l.target/2', capabilities: targetCapabilities,
    open: async () => {}, reset: async () => {}, close: async () => { closed++; },
    observeTestId: async () => [{
      tap: context => new Promise((_, reject) => {
        context.signal.addEventListener('abort', () => reject(new Error('aborted')), {once: true});
      }),
      fill: async () => {}, visible: async () => true, state: async () => true, text: async () => '',
    }],
  };
  const app = new TargetApp(driver, driver, {deadlineMs: Date.now() + 2000});
  try {
    await expect(app.getByTestId('send').tap({timeoutMs: 50})).rejects.toThrow('operation_timeout');
    expect(closed).toBe(1);
  } finally { await app.close(); }
});

for (const outcome of ['resolve', 'reject']) {
  test(`an Android transport that ${outcome}s past its deadline cannot beat the timer`, async () => {
    let closed = 0;
    const driver: TargetDriver = {
      kind: 'android', appId: 'assertion', version: '9l.target/2', capabilities: targetCapabilities,
      open: async () => {}, reset: async () => {}, close: async () => { closed++; },
      observeTestId: async () => [{
        tap: async context => {
          // Hold the event loop so the driver's completion runs before the
          // overdue SDK timer, as a native transport timeout can in practice.
          while (Date.now() <= context.deadlineMs) { /* deliberate timer delay */ }
          if (outcome === 'reject') throw new Error('transport_failed');
        },
        fill: async () => {}, visible: async () => true, state: async () => true, text: async () => '',
      }],
    };
    const app = new TargetApp(driver, driver, {deadlineMs: Date.now() + 2000});
    try {
      await expect(app.getByTestId('send').tap({timeoutMs: 30})).rejects.toThrow('operation_timeout');
      expect(closed).toBe(1);
    } finally { await app.close(); }
  });
}
