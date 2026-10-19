import {test as base, type Locator, type Page, type TestInfo} from '@playwright/test';
import {setTimeout as delay} from 'node:timers/promises';
import {engineIdentity, selectedTarget, targetStepFile, targetVersion} from './protocol';

export const targetCapabilities = ['open', 'reset', 'test-id', 'tap', 'fill', 'visible', 'text', 'state'] as const;
export type TargetIdentity = {kind: 'web' | 'android-fake'; appId: string};
export type TargetContext = {signal: AbortSignal; deadlineMs: number};
export type TargetState = 'enabled' | 'checked';
/** Per-call bound, capped by Go's worker deadline. */
export type TargetOperationOptions = {timeoutMs?: number};

/** Handles stay in the worker. Drivers must stop pending work on abort/close,
 * honor the context deadline, must not retry mutations, and must never act on
 * an arbitrary first match. */
export interface TargetHandle {
  tap(context: TargetContext): Promise<void>;
  fill(value: string, context: TargetContext): Promise<void>;
  visible(context: TargetContext): Promise<boolean>;
  text(context: TargetContext): Promise<string>;
  state(name: TargetState, context: TargetContext): Promise<boolean>;
}
export interface TargetDriver extends TargetIdentity {
  version: typeof targetVersion;
  capabilities: readonly string[];
  open(context: TargetContext): Promise<void>;
  reset(context: TargetContext): Promise<void>;
  /** Observe at most two matches; two means ambiguous and must fail. */
  observeTestId(id: string, context: TargetContext): Promise<readonly TargetHandle[]>;
  close(): Promise<void>;
}

const defaultOperationMs = 5_000;
const defaultAssertionMs = 5_000;
const maxOperationMs = 60_000;
const maxObservations = 200;
// Bounded operations settle by their deadline. After this grace the session
// closes its driver to cancel one that does not.
const settleMs = 250;
const stepLocation = {file: targetStepFile, line: 1, column: 1};

class TargetStopped extends Error {
  constructor(readonly reason: string, cause?: unknown) {
    super(`9lives target stopped: ${reason}`, cause === undefined ? undefined : {cause});
  }
}
const stopped = (reason: string, cause?: unknown) => new TargetStopped(reason, cause);
const boundedText = (value: unknown, max: number): value is string => typeof value === 'string' && Buffer.byteLength(value) <= max;

/** Deterministic shared operations. No semantic judge, replay or native driver. */
export class TargetApp {
  private controller = new AbortController();
  private timer: NodeJS.Timeout;
  private busy = false;
  private reason = 'canceled';
  private closing?: Promise<void>;
  private externalAbort = () => this.stop('canceled');

  constructor(private driver: TargetDriver, expected: TargetIdentity, private options: {deadlineMs: number; signal?: AbortSignal}) {
    if (driver.version !== targetVersion || driver.kind !== expected.kind || driver.appId !== expected.appId ||
        driver.capabilities.length !== targetCapabilities.length || driver.capabilities.some((cap, index) => cap !== targetCapabilities[index])) {
      throw stopped('unsupported_driver_contract');
    }
    if (!Number.isSafeInteger(options.deadlineMs)) throw stopped('invalid_deadline');
    this.timer = setTimeout(() => this.stop('deadline'), Math.max(0, Math.min(2 ** 31 - 1, options.deadlineMs - Date.now())));
    options.signal?.addEventListener('abort', this.externalAbort, {once: true});
    if (options.signal?.aborted || options.deadlineMs <= Date.now()) this.stop(options.signal?.aborted ? 'canceled' : 'deadline');
  }

  /** Stop the session permanently. Releasing closes the owned driver now, which
   * also cancels transports without AbortSignal support (notably Playwright
   * actions). A settled failure keeps the driver until teardown, so Playwright
   * can still capture failure artifacts. */
  private stop(reason: string, release = true) {
    if (!this.controller.signal.aborted) {
      this.reason = reason;
      this.controller.abort();
    }
    if (release) this.release();
  }

  private release() {
    if (this.closing) return;
    this.closing = Promise.resolve().then(() => this.driver.close());
    void this.closing.catch(() => {});
  }

  async close() {
    clearTimeout(this.timer);
    this.options.signal?.removeEventListener('abort', this.externalAbort);
    this.stop('closed');
    try { await this.closing; } catch { throw stopped('cleanup_failed'); }
  }

  /** Permanently stop this session, including a pending mutation. */
  cancel() { this.stop('canceled'); }

  private async perform<T>(operation: string, body: (context: TargetContext) => Promise<T>, timeoutMs = defaultOperationMs): Promise<T> {
    // The SDK location marks this as a typed target step for the reporter.
    return base.step(`9l target ${operation}`, async () => {
      if (this.controller.signal.aborted) throw stopped(this.reason);
      if (this.busy) { this.stop('concurrent_operation'); throw stopped(this.reason); }
      if (!Number.isSafeInteger(timeoutMs) || timeoutMs < 1 || timeoutMs > maxOperationMs) {
        this.stop('invalid_timeout', false); throw stopped(this.reason);
      }
      this.busy = true;
      const deadlineMs = Math.min(this.options.deadlineMs, Date.now() + timeoutMs);
      const timer = setTimeout(() => this.stop('deadline'), Math.max(0, deadlineMs - Date.now()) + settleMs);
      const signal = this.controller.signal;
      let abort: () => void = () => {};
      try {
        return await Promise.race([
          Promise.resolve().then(() => { signal.throwIfAborted(); return body({signal, deadlineMs}); }),
          new Promise<never>((_, reject) => {
            abort = () => reject(stopped(this.reason));
            signal.addEventListener('abort', abort, {once: true});
            if (signal.aborted) abort();
          }),
        ]);
      } catch (error) {
        // Keep the first reason (deadline, cancellation, a missing control...)
        // and the local cause. Neither crosses the engine bridge.
        if (signal.aborted) throw error instanceof TargetStopped ? error : stopped(this.reason, error);
        this.stop(error instanceof TargetStopped ? error.reason : 'operation_failed', false);
        throw error instanceof TargetStopped ? error : stopped(this.reason, error);
      } finally {
        clearTimeout(timer);
        signal.removeEventListener('abort', abort);
        this.busy = false;
      }
    }, {location: stepLocation});
  }

  open(options: TargetOperationOptions = {}) { return this.perform('open', context => this.driver.open(context), options.timeoutMs); }
  reset(options: TargetOperationOptions = {}) { return this.perform('reset', context => this.driver.reset(context), options.timeoutMs); }

  getByTestId(id: string) {
    const observe = async (context: TargetContext) => {
      if (!boundedText(id, 128) || !id || /[\x00-\x1f]/.test(id)) throw stopped('invalid_test_id');
      const matches = await this.driver.observeTestId(id, context);
      context.signal.throwIfAborted();
      if (!Array.isArray(matches) || matches.length > 1) throw stopped('ambiguous_test_id');
      return matches[0];
    };
    const required = async (context: TargetContext) => {
      const handle = await observe(context);
      if (!handle) throw stopped('missing_test_id');
      return handle;
    };
    const assertion = (operation: string, check: (handle: TargetHandle, context: TargetContext) => Promise<boolean>, timeoutMs = defaultAssertionMs) =>
      this.perform(operation, async context => {
        // At most maxObservations polls within the bound; the poll ends before
        // the deadline so a false assertion fails by itself.
        const interval = Math.max(25, Math.ceil(timeoutMs / maxObservations));
        let last: unknown;
        for (let poll = 0; poll < maxObservations; poll++) {
          context.signal.throwIfAborted();
          try {
            const handle = await observe(context);
            if (handle && await check(handle, context)) return;
          } catch (error) {
            // Ambiguity, invalid input and cancellation are not transient.
            if (error instanceof TargetStopped || context.signal.aborted) throw error;
            last = error;
          }
          if (Date.now() + interval >= context.deadlineMs) break;
          await delay(interval, undefined, {signal: context.signal});
        }
        throw stopped('assertion_failed', last);
      }, timeoutMs);
    return {
      tap: (options: TargetOperationOptions = {}) => this.perform('tap', async context => (await required(context)).tap(context), options.timeoutMs),
      fill: (value: string, options: TargetOperationOptions = {}) => this.perform('fill', async context => {
        if (!boundedText(value, 4096)) throw stopped('invalid_value');
        await (await required(context)).fill(value, context);
      }, options.timeoutMs),
      toBeVisible: (timeoutMs?: number) => assertion('assert-visible', (handle, context) => handle.visible(context), timeoutMs),
      toHaveText: (value: string, timeoutMs?: number) => {
        if (!boundedText(value, 4096)) return assertion('assert-text', async () => { throw stopped('invalid_value'); }, timeoutMs);
        return assertion('assert-text', async (handle, context) => {
          const actual = await handle.text(context);
          return boundedText(actual, 4096) && actual === value;
        }, timeoutMs);
      },
      toHaveState: (name: TargetState, value: boolean, timeoutMs?: number) => {
        if (!['enabled', 'checked'].includes(name) || typeof value !== 'boolean') return assertion('assert-state', async () => { throw stopped('unsupported_state'); }, timeoutMs);
        return assertion('assert-state', async (handle, context) => await handle.state(name, context) === value, timeoutMs);
      },
    };
  }
}

const remaining = (context: TargetContext) => { context.signal.throwIfAborted(); return Math.max(1, context.deadlineMs - Date.now()); };

function locatorHandle(locator: Locator): TargetHandle {
  // Strict locators re-resolve a replaced node (re-renders, hydration) but
  // throw rather than choose when more than one element matches.
  return {
    tap: async context => { await locator.click({timeout: remaining(context)}); },
    fill: async (value, context) => { await locator.fill(value, {timeout: remaining(context)}); },
    visible: async context => { remaining(context); return locator.isVisible(); },
    text: async context => await locator.textContent({timeout: remaining(context)}) ?? '',
    state: async (name, context) => name === 'enabled' ? locator.isEnabled({timeout: remaining(context)}) : locator.isChecked({timeout: remaining(context)}),
  };
}

/** Ordinary Playwright handles implement the web side of the shared contract. */
export function webTargetDriver(page: Page, appId: string, lifecycle: {
  open(page: Page, context: TargetContext): Promise<void>;
  reset(page: Page, context: TargetContext): Promise<void>;
}): TargetDriver {
  return {
    version: targetVersion, capabilities: targetCapabilities, kind: 'web', appId,
    open: context => lifecycle.open(page, context), reset: context => lifecycle.reset(page, context),
    close: () => page.close({runBeforeUnload: false}),
    observeTestId: async (id, context) => {
      remaining(context);
      const locator = page.getByTestId(id);
      const count = await locator.count();
      const handle = locatorHandle(locator);
      return count === 0 ? [] : count === 1 ? [handle] : [handle, handle];
    },
  };
}

export type TargetFixtures = {app: TargetApp};
type TargetOptions = {
  web?: Parameters<typeof webTargetDriver>[2];
  /** Construct only; acquire resources in the driver's bounded open method. */
  native?: (target: TargetIdentity) => TargetDriver;
};

/** Go selects one configured project per job. The native fixture has no page,
 * context or browser dependency, including when the default config uses them.
 * Specs load without 9l (listing, editors); using `app` requires 9l run --sdk --target. */
export function createTargetTest(options: TargetOptions) {
  const resolve = () => { engineIdentity(); return selectedTarget(); };
  const useApp = async (target: ReturnType<typeof selectedTarget>, driver: TargetDriver, use: (app: TargetApp) => Promise<void>) => {
    let app: TargetApp | undefined;
    try {
      app = new TargetApp(driver, target, {deadlineMs: Number(process.env.NINELIVES_TARGET_DEADLINE_MS)});
      await use(app);
    } finally {
      if (app) await app.close();
      else { try { await driver.close(); } catch { throw stopped('cleanup_failed'); } }
    }
  };
  const owned = (target: ReturnType<typeof selectedTarget>, info: TestInfo) => {
    if (info.retry !== 0 || info.project.name !== target.name) throw stopped('repeated_or_foreign_target');
  };
  if (process.env.NINELIVES_TARGET?.startsWith('web:')) {
    return base.extend<TargetFixtures>({app: async ({page}, use, info) => {
      const target = resolve();
      owned(target, info);
      if (!options.web) throw stopped('unsupported_target');
      await useApp(target, webTargetDriver(page, target.appId, options.web), use);
    }});
  }
  return base.extend<TargetFixtures>({app: async ({}, use, info) => {
    const target = resolve();
    owned(target, info);
    if (target.kind !== 'android-fake' || !options.native) throw stopped('unsupported_target');
    await useApp(target, options.native(target), use);
  }});
}
