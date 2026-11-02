import {appendFileSync} from 'node:fs';
import {setTimeout as delay} from 'node:timers/promises';
import {targetCapabilities, TargetSetupError, type TargetDriver, type TargetHandle, type TargetIdentity, type TargetContext, type TargetSetupRecord} from '@9l/playwright';

// Protocol fixture only. This does not communicate with an Android device.
export function fakeFinance(target: TargetIdentity, mode: string): TargetDriver {
  let amount = '', status = '', closed = false;
  const effect = (event: string) => {
    if (process.env.NINELIVES_TARGET_EFFECTS) appendFileSync(process.env.NINELIVES_TARGET_EFFECTS, event + '\n');
  };
  return {
    ...target, version: mode === 'version' ? '9l.target/99' as '9l.target/1' : '9l.target/1',
    capabilities: mode === 'capabilities' ? [] : targetCapabilities,
    // Setup classifications a real device driver would make: a locked phone
    // before any operation, a leaked window recovered by a declared Back, and
    // a phone that locked while an assertion was polling.
    open: async () => { if (mode === 'locked') throw new TargetSetupError({classification: 'blocked', reason: 'device-locked', stage: 'open'}); },
    setup: () => mode === 'dirty-recovered' || mode === 'recovered-then-lock' ? {classification: 'dirty', reason: 'extra-window', stage: 'open', recovery: {method: 'back', presses: 2, restored: true}} as TargetSetupRecord : undefined,
    recheck: async () => mode === 'lock-recheck' || mode === 'recovered-then-lock' ? {classification: 'blocked', reason: 'device-locked', stage: 'recheck'} : undefined,
    reset: async () => { amount = ''; status = ''; }, close: async () => { closed = true; },
    observeTestId: async (id: string) => {
      if (mode === 'ambiguous') return [null, null] as unknown as TargetHandle[];
      if (mode === 'missing' || !['amount', 'send', 'receipt'].includes(id)) return [];
      return [{
        tap: async (context: TargetContext) => {
          if (id !== 'send') throw new Error('unsupported');
          if (mode === 'cancel' || mode === 'deadline' || mode === 'timeout') { effect('started'); await delay(1000, undefined, {signal: context.signal}); }
          // Honors the abort signal but not the deadline: finishes 50ms after a 100ms bound.
          if (mode === 'late') { effect('started'); await delay(150, undefined, {signal: context.signal}); }
          context.signal.throwIfAborted();
          if (closed) throw new Error('closed');
          effect('mutation'); status = mode === 'defect' || mode === 'lock-recheck' || mode === 'recovered-then-lock' ? 'Sent $50' : `Sent $${amount}`;
        },
        fill: async value => { if (id !== 'amount') throw new Error('unsupported'); amount = value; },
        visible: async () => id !== 'receipt' || !!status,
        text: async () => id === 'receipt' ? status : amount,
        state: async name => { if (name !== 'enabled') throw new Error('unsupported'); return id === 'send' && !!amount; },
      }];
    },
  };
}
