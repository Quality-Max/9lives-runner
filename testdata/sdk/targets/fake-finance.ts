import {appendFileSync} from 'node:fs';
import {setTimeout as delay} from 'node:timers/promises';
import {targetCapabilities, TargetSetupError, type TargetDriver, type TargetHandle, type TargetIdentity, type TargetContext, type TargetSetupRecord} from '@9l/playwright';

// Protocol fixture only. This does not communicate with an Android device.
export function fakeFinance(target: TargetIdentity, mode: string): TargetDriver {
  let amount = '', status = '', closed = false, keyboard = false, running = true;
  // Deterministic geometry for the operations fixture: a 400x800 screen, a
  // button at (40,40)-(240,120) and a scrollable list below it.
  const button = {x: 40, y: 40, width: 200, height: 80};
  const list = {x: 0, y: 160, width: 400, height: 600};
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
    recheck: async (_context, failed) => mode === 'lock-recheck' || mode === 'recovered-then-lock' ? {classification: 'blocked', reason: 'device-locked', stage: 'recheck'}
      : mode === 'observer' && failed.operation === 'assert-hidden' ? {classification: 'observer', reason: 'window-gone-element-displayed', stage: 'recheck'} : undefined,
    // Opt-in diagnostics as a device driver would record them: the dialog's
    // window was gone after the outside tap, while the snapshot still listed it.
    diagnose: async failure => mode === 'observer' ? {operation: failure.operation, reason: failure.reason, elapsedMs: failure.elapsedMs, ...(failure.point ? {x: failure.point.x, y: failure.point.y, point: true} : {}),
      viewportWidth: 400, viewportHeight: 800, windowsBefore: 2, windowsAfter: 1, focusBefore: 'app', focusAfter: 'app'} : undefined,
    reset: async () => { amount = ''; status = ''; keyboard = false; }, close: async () => { closed = true; },
    back: async () => { status = keyboard ? status : 'back'; keyboard = false; },
    keyboardShown: async () => keyboard,
    hideKeyboard: async () => { keyboard = false; },
    // An outside touch dismisses the open "modal" unless the app ignores it
    // (ignored-touch) or the snapshot keeps showing it (observer).
    tapAt: async point => {
      const inside = point.x >= button.x && point.x < button.x + button.width && point.y >= button.y && point.y < button.y + button.height;
      status = inside ? 'tapped' : mode === 'observer' || mode === 'ignored-touch' ? status : '';
    },
    viewport: async () => ({width: 400, height: 800}),
    terminate: async () => { running = false; status = ''; },
    activate: async () => { running = true; status = amount ? `draft ${amount}` : ''; },
    observeTestId: async (id: string) => {
      if (mode === 'ambiguous') return [null, null] as unknown as TargetHandle[];
      if (mode === 'missing' || !running || !['amount', 'send', 'receipt', 'status', 'list', 'button'].includes(id)) return [];
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
        fill: async value => { if (id !== 'amount') throw new Error('unsupported'); amount = value; keyboard = true; },
        visible: async () => id === 'receipt' || id === 'status' ? !!status : true,
        text: async () => id === 'receipt' || id === 'status' ? status : amount,
        scroll: async (direction, percent) => { if (id !== 'list') throw new Error('unsupported'); status = `scrolled ${direction} ${percent}`; },
        bounds: async () => { if (id === 'button') return button; if (id === 'list') return list; throw new Error('unsupported'); },
        state: async name => { if (name !== 'enabled') throw new Error('unsupported'); return id === 'send' && !!amount; },
      }];
    },
  };
}
