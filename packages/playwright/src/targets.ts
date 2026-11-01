import {test as base, type Locator, type Page, type TestInfo} from '@playwright/test';
import {setTimeout as delay} from 'node:timers/promises';
import {engineIdentity, selectedTarget, targetStepFile, targetVersion} from './protocol';
import {validAndroidRuntime, androidRuntimeType, type AndroidRuntime} from './android-runtime';
import {TargetSetupError, targetSetupType, validTargetSetup, targetDiagnosticType, validTargetDiagnostic, type TargetSetupRecord, type TargetDiagnostic} from './target-setup';
export {TargetSetupError, validTargetSetup, validTargetDiagnostic, type TargetSetupRecord, type SetupClassification, type TargetDiagnostic, type WindowObservation} from './target-setup';

export const targetCapabilities = ['open', 'reset', 'test-id', 'tap', 'fill', 'visible', 'text', 'state',
  'back', 'keyboard', 'scroll', 'tap-at', 'bounds', 'viewport', 'terminate', 'activate'] as const;
export type ScrollDirection = 'up' | 'down' | 'left' | 'right';
/** Integer device coordinates: physical pixels on Android, CSS pixels in the web viewport. */
export type TargetPoint = {x: number; y: number};
export type TargetRect = TargetPoint & {width: number; height: number};
export type TargetSize = {width: number; height: number};
export type TargetIdentity = {kind: 'web' | 'android-fake' | 'android'; appId: string};
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
  /** One bounded scroll gesture inside this element; percent of its size. */
  scroll?(direction: ScrollDirection, percent: number, context: TargetContext): Promise<void>;
  bounds?(context: TargetContext): Promise<TargetRect>;
}
export interface TargetDriver extends TargetIdentity {
  version: typeof targetVersion;
  capabilities: readonly string[];
  open(context: TargetContext): Promise<void>;
  reset(context: TargetContext): Promise<void>;
  /** Observe at most two matches; two means ambiguous and must fail. */
  observeTestId(id: string, context: TargetContext): Promise<readonly TargetHandle[]>;
  // Optional operations. A driver without one fails the operation with the
  // stable reason unsupported_operation; it never substitutes another action.
  /** Platform back: Android's Back key, the browser's history. */
  back?(context: TargetContext): Promise<void>;
  /** Software keyboard visibility; an unreadable state throws. Android only. */
  keyboardShown?(context: TargetContext): Promise<boolean>;
  /** Dismiss the keyboard without changing the input method. Android only. */
  hideKeyboard?(context: TargetContext): Promise<void>;
  /** One touch down/up at device coordinates. */
  tapAt?(point: TargetPoint, context: TargetContext): Promise<void>;
  viewport?(context: TargetContext): Promise<TargetSize>;
  /** Stop and relaunch the configured app, keeping its data. Android only. */
  terminate?(context: TargetContext): Promise<void>;
  activate?(context: TargetContext): Promise<void>;
  close(): Promise<void>;
  runtime?(): AndroidRuntime | undefined;
  /** After a successful open: the setup record of a declared recovery that
   * restored the baseline, so the receipt shows the test ran from it. */
  setup?(): TargetSetupRecord | undefined;
  /** After a completed assertion failure, while the driver is still open:
   * classify the device state (a locked phone, for example) so the failure
   * reads as inconclusive setup, never as an application defect. It must only
   * read state; it never acts on the device. Its own errors are ignored. */
  recheck?(context: TargetContext, failed: {operation: string}): Promise<TargetSetupRecord | undefined>;
  /** Opt-in: describe a failed operation as bounded metadata, captured before
   * fixture teardown. Read-only; errors are ignored; one record per test. */
  diagnose?(failure: {operation: string; reason: string; elapsedMs: number; point?: TargetPoint}, context: TargetContext): Promise<TargetDiagnostic | undefined>;
}

const defaultOperationMs = 5_000;
const defaultAssertionMs = 5_000;
const maxOperationMs = 60_000;
const maxObservations = 200;
// An operation is aborted and rejected at its deadline. If it is still pending
// after this grace, the session closes its driver to cancel it.
const settleMs = 250;
const stepLocation = {file: targetStepFile, line: 1, column: 1};

class TargetStopped extends Error {
  constructor(readonly reason: string, cause?: unknown) {
    super(`9lives target stopped: ${reason}`, cause === undefined ? undefined : {cause});
  }
}
const stopped = (reason: string, cause?: unknown) => new TargetStopped(reason, cause);
const recheckMs = 5_000;
// A closed metadata record; the reporter forwards it as one target_setup frame.
const attachSetup = (record: TargetSetupRecord) => base.info().attach('9l.target-setup', {body: Buffer.from(JSON.stringify(record)), contentType: targetSetupType});
const attachDiagnostic = (record: TargetDiagnostic) => base.info().attach('9l.target-diagnostic', {body: Buffer.from(JSON.stringify(record)), contentType: targetDiagnosticType});
const boundedText = (value: unknown, max: number): value is string => typeof value === 'string' && Buffer.byteLength(value) <= max;

/** Deterministic shared operations; browser/device handles stay in the worker. */
export class TargetApp {
  private controller = new AbortController();
  private timer: NodeJS.Timeout;
  private busy = false;
  private reason = 'canceled';
  private closing?: Promise<void>;
  private diagnosed = false;
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

  /** Opt-in failure diagnostics: ask the driver once per test, while it is
   * still open, and attach only a validated record. Never changes the outcome. */
  private async diagnose(operation: string, reason: string, startedAt: number, point?: TargetPoint) {
    if (this.diagnosed || !this.driver.diagnose || this.closing) return;
    this.diagnosed = true;
    let record: TargetDiagnostic | undefined;
    try {
      record = await this.driver.diagnose({operation, reason, elapsedMs: Math.max(0, Date.now() - startedAt), ...(point ? {point} : {})}, {signal: new AbortController().signal, deadlineMs: Math.min(this.options.deadlineMs, Date.now() + recheckMs)});
    } catch { record = undefined; }
    if (record !== undefined && validTargetDiagnostic(record)) await attachDiagnostic(record);
  }

  private async perform<T>(operation: string, body: (context: TargetContext) => Promise<T>, timeoutMs = defaultOperationMs, maxTimeoutMs = maxOperationMs, deadlineReason = () => 'deadline', point?: TargetPoint): Promise<T> {
    // The SDK location marks this as a typed target step for the reporter.
    return base.step(`9l target ${operation}`, async () => {
      if (this.controller.signal.aborted) throw stopped(this.reason);
      if (this.busy) { this.stop('concurrent_operation'); throw stopped(this.reason); }
      if (!Number.isSafeInteger(timeoutMs) || timeoutMs < 1 || timeoutMs > maxTimeoutMs) {
        this.stop('invalid_timeout', false); throw stopped(this.reason);
      }
      this.busy = true;
      const deadlineMs = Math.min(this.options.deadlineMs, Date.now() + timeoutMs);
      const startedAt = Date.now();
      const signal = this.controller.signal;
      const work = Promise.resolve().then(() => { signal.throwIfAborted(); return body({signal, deadlineMs}); });
      let settled = false;
      void work.then(() => { settled = true; }, () => { settled = true; });
      // Abort and reject at the deadline: a late result never succeeds. The
      // Android close stops native work; web retains its artifact-preservation grace.
      const expire = () => {
        this.stop(deadlineMs < this.options.deadlineMs ? deadlineReason() : 'deadline', false);
        if (this.driver.kind === 'android') this.release();
        else setTimeout(() => { if (!settled) this.release(); }, settleMs);
      };
      const timer = setTimeout(expire, Math.max(0, deadlineMs - Date.now()));
      let abort: () => void = () => {};
      try {
        const result = await Promise.race([
          work,
          new Promise<never>((_, reject) => {
            abort = () => reject(stopped(this.reason));
            signal.addEventListener('abort', abort, {once: true});
            if (signal.aborted) abort();
          }),
        ]);
        // A driver completion can run before an overdue timer. Enforce the
        // bound here too, so late success or transport failure cannot win.
        if (!signal.aborted && Date.now() >= deadlineMs) expire();
        if (signal.aborted) throw stopped(this.reason);
        return result;
      } catch (error) {
        // Keep the first reason (deadline, cancellation, a missing control...)
        // and the local cause. Neither crosses the engine bridge.
        if (!signal.aborted && Date.now() >= deadlineMs) expire();
        if (signal.aborted) {
          // The driver may already be released (an Android deadline closes
          // it); the driver answers with what it still knows, or nothing.
          await this.diagnose(operation, this.reason, startedAt, point);
          throw error instanceof TargetStopped ? error : stopped(this.reason, error);
        }
        if (error instanceof TargetSetupError) {
          // The driver classified the device/app state before the operation
          // could establish anything about the application.
          this.stop(`setup_${error.record.classification}`, false);
          await attachSetup(error.record);
          throw stopped(this.reason, error);
        }
        this.stop(error instanceof TargetStopped ? error.reason : 'operation_failed', false);
        await this.diagnose(operation, this.reason, startedAt, point);
        throw error instanceof TargetStopped ? error : stopped(this.reason, error);
      } finally {
        clearTimeout(timer);
        signal.removeEventListener('abort', abort);
        this.busy = false;
      }
    }, {location: stepLocation});
  }

  open(options: TargetOperationOptions = {}) { return this.perform('open', async context => {
    await this.driver.open(context);
    if (this.driver.kind === 'android') {
      const runtime = this.driver.runtime?.();
      if (!validAndroidRuntime(runtime)) throw stopped('missing_android_runtime');
      await base.info().attach('9l.android-runtime', {body: Buffer.from(JSON.stringify(runtime)), contentType: androidRuntimeType});
    }
    const recovered = this.driver.setup?.();
    if (recovered !== undefined) {
      if (!validTargetSetup(recovered) || !recovered.recovery?.restored) throw stopped('invalid_setup_record');
      await attachSetup(recovered);
    }
  }, options.timeoutMs ?? (this.driver.kind === 'android' ? 180_000 : undefined), this.driver.kind === 'android' ? 180_000 : maxOperationMs); }
  reset(options: TargetOperationOptions = {}) { return this.perform('reset', context => this.driver.reset(context), options.timeoutMs); }

  /** Poll a check to its bound. A completed false observation is an assertion
   * failure even if a later read stalls; a bound reached before any completed
   * check is a deadline; failed reads alone keep their own operation error.
   * After a completed failure, the driver may reclassify the device state. */
  private async assertLoop(operation: string, attempt: (context: TargetContext) => Promise<boolean>, timeoutMs = defaultAssertionMs) {
    let observedFalse = false;
    try {
      return await this.perform(operation, async context => {
        const interval = Math.max(25, Math.ceil(timeoutMs / maxObservations));
        let last: unknown;
        for (let poll = 0; poll < maxObservations; poll++) {
          context.signal.throwIfAborted();
          try {
            if (await attempt(context)) return;
            observedFalse = true;
          } catch (error) {
            // Ambiguity, invalid input and cancellation are not transient.
            if (error instanceof TargetStopped || context.signal.aborted) throw error;
            last = error;
          }
          if (Date.now() + interval >= context.deadlineMs) break;
          await delay(interval, undefined, {signal: context.signal});
        }
        if (observedFalse) throw stopped('assertion_failed', last);
        // Failed reads alone establish no business mismatch. Keep their
        // operation failure distinct from an executed false comparison.
        if (last !== undefined) throw last;
        throw stopped('deadline');
      }, timeoutMs, maxOperationMs, () => observedFalse ? 'assertion_failed' : 'deadline');
    } catch (error) {
      // A completed mismatch on a device the driver can still read: ask it
      // whether the device became unavailable (locked, screen off) during
      // the readiness poll. A positive answer reclassifies the failure as
      // setup; the assertion step stays failed and the test still fails.
      if (!(error instanceof TargetStopped) || error.reason !== 'assertion_failed' || !this.driver.recheck || this.closing) throw error;
      let record: TargetSetupRecord | undefined;
      try {
        record = await this.driver.recheck({signal: new AbortController().signal, deadlineMs: Math.min(this.options.deadlineMs, Date.now() + recheckMs)}, {operation});
      } catch { record = undefined; }
      if (record === undefined) throw error;
      if (!validTargetSetup(record) || record.stage !== 'recheck' || record.recovery) throw error;
      this.reason = `setup_${record.classification}`;
      await attachSetup(record);
      throw stopped(this.reason, error);
    }
  }

  private optional<K extends 'back' | 'keyboardShown' | 'hideKeyboard' | 'tapAt' | 'viewport' | 'terminate' | 'activate'>(name: K): NonNullable<TargetDriver[K]> {
    const method = this.driver[name];
    if (typeof method !== 'function') throw stopped('unsupported_operation');
    return method.bind(this.driver) as NonNullable<TargetDriver[K]>;
  }

  /** Platform back: Android's Back key, the browser's history. */
  back(options: TargetOperationOptions = {}) { return this.perform('back', context => this.optional('back')(context), options.timeoutMs); }
  /** Dismiss the software keyboard, keeping the selected input method. Android only. */
  hideKeyboard(options: TargetOperationOptions = {}) { return this.perform('hide-keyboard', context => this.optional('hideKeyboard')(context), options.timeoutMs); }
  /** Android only: assert the software keyboard is shown or hidden. */
  toHaveKeyboard(visible: boolean, timeoutMs?: number) {
    if (typeof visible !== 'boolean') return this.assertLoop('assert-keyboard', async () => { throw stopped('invalid_value'); }, timeoutMs);
    return this.assertLoop('assert-keyboard', async context => await this.optional('keyboardShown')(context) === visible, timeoutMs);
  }
  /** One touch down/up at device coordinates, for example outside a pane.
   * Coordinates are integers inside the viewport; nothing is retried. */
  tapAt(point: TargetPoint, options: TargetOperationOptions = {}) {
    return this.perform('tap-at', async context => {
      if (!validPoint(point)) throw stopped('invalid_point');
      await this.optional('tapAt')({x: point.x, y: point.y}, context);
    }, options.timeoutMs, maxOperationMs, () => 'deadline', validPoint(point) ? {x: point.x, y: point.y} : undefined);
  }
  viewport(options: TargetOperationOptions = {}) {
    return this.perform('viewport', async context => {
      const size = await this.optional('viewport')(context);
      if (!validSize(size)) throw stopped('invalid_property');
      return {width: size.width, height: size.height};
    }, options.timeoutMs);
  }
  /** Stop the configured app without clearing its data. Android only. */
  terminate(options: TargetOperationOptions = {}) { return this.perform('terminate', context => this.optional('terminate')(context), options.timeoutMs); }
  /** Bring the configured app back to the foreground. Android only. */
  activate(options: TargetOperationOptions = {}) { return this.perform('activate', context => this.optional('activate')(context), options.timeoutMs); }

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
    // A missing control is a completed false observation, except for a
    // hidden assertion, where absence is exactly what it asserts.
    const assertion = (operation: string, check: (handle: TargetHandle, context: TargetContext) => Promise<boolean>, timeoutMs?: number, absent = false) =>
      this.assertLoop(operation, async context => {
        const handle = await observe(context);
        return handle ? check(handle, context) : absent;
      }, timeoutMs);
    return {
      tap: (options: TargetOperationOptions = {}) => this.perform('tap', async context => (await required(context)).tap(context), options.timeoutMs),
      fill: (value: string, options: TargetOperationOptions = {}) => this.perform('fill', async context => {
        if (!boundedText(value, 4096)) throw stopped('invalid_value');
        await (await required(context)).fill(value, context);
      }, options.timeoutMs),
      /** One bounded scroll gesture inside this control, by a fraction of its size. */
      scroll: (direction: ScrollDirection, options: TargetOperationOptions & {percent?: number} = {}) => this.perform('scroll', async context => {
        const percent = options.percent ?? 0.5;
        if (!['up', 'down', 'left', 'right'].includes(direction) || typeof percent !== 'number' || !(percent > 0 && percent <= 1)) throw stopped('invalid_value');
        const handle = await required(context);
        if (typeof handle.scroll !== 'function') throw stopped('unsupported_operation');
        await handle.scroll(direction, percent, context);
      }, options.timeoutMs),
      /** The control's rectangle in device coordinates. */
      bounds: (options: TargetOperationOptions = {}) => this.perform('bounds', async context => {
        const handle = await required(context);
        if (typeof handle.bounds !== 'function') throw stopped('unsupported_operation');
        const rect = await handle.bounds(context);
        if (!validRect(rect)) throw stopped('invalid_property');
        return {x: rect.x, y: rect.y, width: rect.width, height: rect.height};
      }, options.timeoutMs),
      toBeVisible: (timeoutMs?: number) => assertion('assert-visible', (handle, context) => handle.visible(context), timeoutMs),
      /** Passes when the control is absent or not displayed. */
      toBeHidden: (timeoutMs?: number) => assertion('assert-hidden', async (handle, context) => !await handle.visible(context), timeoutMs, true),
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

const coordinate = (value: unknown): value is number => Number.isInteger(value) && (value as number) >= 0 && (value as number) <= 65_535;
const validPoint = (value: unknown): value is TargetPoint => !!value && typeof value === 'object' && coordinate((value as TargetPoint).x) && coordinate((value as TargetPoint).y);
const validSize = (value: unknown): value is TargetSize => !!value && typeof value === 'object' && coordinate((value as TargetSize).width) && coordinate((value as TargetSize).height) && (value as TargetSize).width > 0 && (value as TargetSize).height > 0;
const validRect = (value: unknown): value is TargetRect => validPoint(value) && coordinate((value as TargetRect).width) && coordinate((value as TargetRect).height);

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
    scroll: async (direction, percent, context) => {
      remaining(context);
      await locator.evaluate((element, {direction, percent}) => {
        const dx = direction === 'left' ? -1 : direction === 'right' ? 1 : 0;
        const dy = direction === 'up' ? -1 : direction === 'down' ? 1 : 0;
        element.scrollBy({left: dx * element.clientWidth * percent, top: dy * element.clientHeight * percent, behavior: 'instant'});
      }, {direction, percent});
    },
    bounds: async context => {
      const box = await locator.boundingBox({timeout: remaining(context)});
      if (!box) throw new Error('control has no box');
      return {x: Math.round(box.x), y: Math.round(box.y), width: Math.round(box.width), height: Math.round(box.height)};
    },
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
    back: async context => { await page.goBack({timeout: remaining(context)}); },
    tapAt: async (point, context) => { remaining(context); await page.mouse.click(point.x, point.y); },
    viewport: async context => {
      remaining(context);
      const size = page.viewportSize();
      if (!size) throw new Error('viewport unavailable');
      return size;
    },
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
    if ((target.kind !== 'android-fake' && target.kind !== 'android') || !options.native) throw stopped('unsupported_target');
    await useApp(target, options.native(target), use);
  }});
}
