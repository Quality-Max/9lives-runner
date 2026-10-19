import {appendFileSync} from 'node:fs';
import {setTimeout as delay} from 'node:timers/promises';
import {targetCapabilities, type TargetDriver, type TargetHandle, type TargetIdentity, type TargetContext} from '@9l/playwright';

// Protocol fixture only. This does not communicate with an Android device.
export function fakeFinance(target: TargetIdentity, mode: string): TargetDriver {
  let amount = '', status = '', closed = false;
  const effect = (event: string) => {
    if (process.env.NINELIVES_TARGET_EFFECTS) appendFileSync(process.env.NINELIVES_TARGET_EFFECTS, event + '\n');
  };
  return {
    ...target, version: mode === 'version' ? '9l.target/99' as '9l.target/1' : '9l.target/1',
    capabilities: mode === 'capabilities' ? [] : targetCapabilities,
    open: async () => {}, reset: async () => { amount = ''; status = ''; }, close: async () => { closed = true; },
    observeTestId: async (id: string) => {
      if (mode === 'ambiguous') return [null, null] as unknown as TargetHandle[];
      if (mode === 'missing' || !['amount', 'send', 'receipt'].includes(id)) return [];
      return [{
        tap: async (context: TargetContext) => {
          if (id !== 'send') throw new Error('unsupported');
          if (mode === 'cancel' || mode === 'deadline' || mode === 'timeout') { effect('started'); await delay(1000, undefined, {signal: context.signal}); }
          context.signal.throwIfAborted();
          if (closed) throw new Error('closed');
          effect('mutation'); status = mode === 'defect' ? 'Sent $50' : `Sent $${amount}`;
        },
        fill: async value => { if (id !== 'amount') throw new Error('unsupported'); amount = value; },
        visible: async () => id !== 'receipt' || !!status,
        text: async () => id === 'receipt' ? status : amount,
        state: async name => { if (name !== 'enabled') throw new Error('unsupported'); return id === 'send' && !!amount; },
      }];
    },
  };
}
