import {request as httpRequest} from 'node:http';
import type {Page, ElementHandle} from '@playwright/test';
import {engineIdentity} from './protocol';

export type GoalOptions = {
  /** Values stay in the browser worker; only parameter names reach the provider. */
  params?: Record<string, string>;
  maxActions?: number;
  maxDecisions?: number;
  maxTokens?: number;
  timeoutMs?: number;
  signal?: AbortSignal;
};
export type GoalResult = {goalId: string; status: 'completed'; verified: false};
type Action = 'click' | 'fill' | 'select' | 'check' | 'wait' | 'complete' | 'unresolved';
type Decision = {action: Action; targetId?: string; parameter?: string};
type Reply = {status: string; goalId?: string; decisionId?: string; decision?: Decision; timeoutMs?: number};
type Control = {id: string; role: string; label: string; actions: Action[]; blocked: boolean};
type Target = {handle: ElementHandle<HTMLElement | SVGElement>; fingerprint: string; control: Control};
const version = '9l.goal/1';
const risky = /\b(send|resend|invite|publish|broadcast|notify|share|post|reply|comment|buy|purchase|pay|payment|checkout|check out|place (your |my )?order|submit order|order now|complete (order|purchase|checkout|booking|payment)|confirm (order|purchase|payment|booking)|book now|reserve|donate|subscribe|upgrade|start (subscription|trial)|delete|remove|destroy|erase|discard|trash|deactivate|revoke|close account|cancel (account|subscription))\b/i;
const fail = (status: string): never => { throw new Error(`9lives goal stopped: ${status}`); };
// Bounded semantic labels, never input values, HTML, URLs or entire page text.
export function redactLabel(text: string): string {
  const redacted = text.replace(/\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b/gi, '[redacted]')
    .replace(/\b(?:sk-|ghp_|github_pat_|eyJ)[A-Za-z0-9_./+-]{12,}/g, '[redacted]')
    .replace(/\b[A-Za-z0-9_+-]{32,}\b/g, '[redacted]')
    .replace(/\b(?:password|token|secret|api[_ -]?key)\s*[:=]\s*\S+/gi, '[redacted]')
    .replace(/\s+/g, ' ').trim();
  let bounded = '';
  for (const char of redacted) {
    if (Buffer.byteLength(bounded + char) > 160) break;
    bounded += char;
  }
  return bounded;
}

async function metadata(handle: Target['handle']) {
  return handle.evaluate(element => {
    const input = element as HTMLInputElement;
    const tag = element.tagName.toLowerCase();
    const type = tag === 'input' ? input.type : '';
    const roles: Record<string, string> = {button: 'button', a: 'link', textarea: 'textbox', select: 'combobox'};
    let role = element.getAttribute('role') || roles[tag] || '';
    if (tag === 'input') {
      if (['password', 'file', 'hidden'].includes(type)) return null;
      role = ({checkbox: 'checkbox', radio: 'radio', button: 'button', submit: 'button', reset: 'button', search: 'searchbox', number: 'spinbutton'} as Record<string, string>)[type] || 'textbox';
    }
    // Contenteditable and arbitrary custom roles are outside this first executor.
    const mapping: Record<string, Action[]> = {button: ['click'], link: ['click'], tab: ['click'], menuitem: ['click'], radio: ['click'], switch: ['click'], textbox: ['fill'], searchbox: ['fill'], spinbutton: ['fill'], combobox: ['select'], checkbox: ['check']};
    const actions = mapping[role];
    if (!actions || !element.isConnected || element.closest('[inert]') || element.getAttribute('aria-disabled') === 'true' || (input as HTMLInputElement).disabled) return null;
    const labelled = (element.getAttribute('aria-labelledby') || '').split(/\s+/).map(id => document.getElementById(id)?.textContent || '').join(' ');
    const label = element.getAttribute('aria-label') || labelled.trim() || Array.from(input.labels || []).map(label => label.textContent || '').join(' ') || (tag === 'input' ? input.getAttribute('placeholder') || (['submit', 'button', 'reset'].includes(type) ? input.value : '') : element.textContent || '');
    // Only same-origin links are offered. The URL stays in the worker fingerprint.
    const href = role === 'link' ? element.getAttribute('href') || '' : '';
    if (role === 'link') {
      try { const url = new URL(href, document.baseURI); if (!['http:', 'https:'].includes(url.protocol) || url.origin !== location.origin) return null; } catch { return null; }
    }
    const policyText = [element.getAttribute('aria-label'), labelled, Array.from(input.labels || []).map(label => label.textContent || '').join(' '), element.textContent, ['submit', 'button', 'reset'].includes(type) ? input.value : ''].filter(Boolean).join(' ');
    return {role, label, policyText, actions, fingerprint: JSON.stringify([tag, type, role, label, href, policyText])};
  });
}
/** Redacts the test's parameter values, including whitespace-collapsed forms, then bounds the label. */
export function valueRedactor(params: Record<string, string>): (text: string) => string {
  const values = Object.values(params).flatMap(value => [value, value.replace(/\s+/g, ' ').trim()]).filter(Boolean);
  return text => redactLabel(values.reduce((result, value) => result.split(value).join('[redacted]'), text));
}

/** Up to eight heading/status labels, redacted before they are cut to size. */
export async function observeState(page: Page, redact: (text: string) => string): Promise<string[]> {
  // Collapse whitespace before bounding, and keep enough text that any value
  // overlapping the 160-byte label lies wholly inside it: parameters are
  // redacted before the label is cut, never after.
  const state = await page.locator('h1,h2,[role="status"]').evaluateAll(elements => elements.slice(0, 8).map(element => (element.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 8192)));
  return state.map(redact);
}

async function observe(page: Page, redact: (text: string) => string) {
  const locator = page.locator('button,a[href],input,textarea,select,[role="button"],[role="tab"],[role="menuitem"],[role="checkbox"],[role="radio"],[role="switch"]');
  const handles: Target['handle'][] = [];
  try {
    for (let index = 0, count = Math.min(100, await locator.count()); index < count; index++) {
      const handle = await locator.nth(index).elementHandle({timeout: 500});
      if (handle) handles.push(handle as Target['handle']);
    }
    const targets = new Map<string, Target>();
    // Bound traversal as well as the transmitted set.
    for (const handle of handles.slice(0, 100)) {
      if (targets.size >= 25 || !await handle.isVisible() || !await handle.isEnabled()) continue;
      const data = await metadata(handle);
      if (!data) continue;
      const id = `target-${targets.size + 1}`;
      targets.set(id, {handle, fingerprint: data.fingerprint, control: {id, role: data.role, label: redact(data.label), actions: data.actions, blocked: risky.test(data.policyText)}});
    }
    const state = await observeState(page, redact);
    return {targets, state, dispose: async () => { await Promise.allSettled(handles.map(handle => handle.dispose())); }};
  } catch {
    await Promise.allSettled(handles.map(handle => handle.dispose()));
    return fail('observation_failed');
  }
}

export async function executeGoal(page: Page, instruction: string, options: GoalOptions = {}): Promise<GoalResult> {
  const identity = {...engineIdentity(), version};
  const socketPath = process.env.NINELIVES_GOAL_SOCKET;
  if (!socketPath) return fail('provider_required; use --goal-provider or --goal-script');
  const params = options.params || {};
  if (typeof instruction !== 'string' || Buffer.byteLength(instruction) > 4096 || !instruction.trim() || Object.keys(params).length > 16 || Object.entries(params).some(([key, value]) => !/^[A-Za-z][A-Za-z0-9_]{0,63}$/.test(key) || typeof value !== 'string' || value.length > 4096)) return fail('invalid_options');
  const redact = valueRedactor(params);
  // Only caps the test sets are sent. Go applies them below the CLI budgets;
  // an omitted cap keeps the attempt's, so SDK defaults never override the CLI.
  const limits = Object.fromEntries((['maxActions', 'maxDecisions', 'maxTokens', 'timeoutMs'] as const).filter(key => options[key] !== undefined).map(key => [key, options[key]]));
  if (Object.values(limits).some(value => !Number.isSafeInteger(value) || (value as number) <= 0)) return fail('invalid_options');
  const controller = new AbortController();
  let stopReason = 'canceled';
  const stop = (reason: string) => {
    if (controller.signal.aborted) return;
    stopReason = reason;
    controller.abort();
    // Playwright actions have no AbortSignal option. Closing this owned page
    // cancels an in-flight action rather than allowing it to fire after abort.
    void page.close({runBeforeUnload: false}).catch(() => {});
  };
  const abort = () => stop('canceled');
  if (options.signal?.aborted) return fail('canceled');
  options.signal?.addEventListener('abort', abort, {once: true});
  page.once('close', abort);
  // Until Go returns the goal's effective time budget, bound only the start call.
  const startMs = options.timeoutMs ?? 60000;
  let deadline = Date.now() + startMs;
  let deadlineTimer = setTimeout(() => stop('budget_exhausted'), startMs);
  const call = async (body: object): Promise<Reply> => {
    if (controller.signal.aborted) return fail(stopReason);
    const raw = JSON.stringify({...identity, ...body});
    if (Buffer.byteLength(raw) > 24000) return fail('observation_too_large');
    return new Promise((resolve, reject) => {
      const req = httpRequest({socketPath, path: '/goal', method: 'POST', headers: {'content-type': 'application/json'}, signal: controller.signal}, res => {
        let output = '';
        res.setEncoding('utf8');
        res.on('data', chunk => { output += chunk; if (Buffer.byteLength(output) > 4096) req.destroy(new Error('invalid_reply')); });
        res.on('error', () => reject(new Error(`9lives goal stopped: ${controller.signal.aborted ? stopReason : 'transport_failed'}`)));
        res.on('end', () => {
          try {
            const reply = JSON.parse(output) as Reply;
            if (res.statusCode !== 200 || typeof reply.status !== 'string') throw new Error();
            resolve(reply);
          } catch { reject(new Error('9lives goal stopped: invalid_reply')); }
        });
      });
      const timer = setTimeout(() => req.destroy(new Error('deadline')), Math.max(1, deadline - Date.now()));
      req.on('close', () => clearTimeout(timer));
      req.on('error', () => reject(new Error(`9lives goal stopped: ${controller.signal.aborted ? stopReason : 'transport_failed'}`)));
      req.end(raw);
    });
  };
  try {
    const start = await call({op: 'start', instruction: Object.values(params).filter(Boolean).reduce((result, value) => result.split(value).join('[redacted]'), instruction), parameters: Object.keys(params), ...(Object.keys(limits).length ? {limits} : {})});
    if (start.status !== 'active' || !start.goalId || !start.timeoutMs) return fail(start.status);
    const goalId = start.goalId;
    deadline = Date.now() + start.timeoutMs;
    clearTimeout(deadlineTimer);
    deadlineTimer = setTimeout(() => stop('budget_exhausted'), start.timeoutMs);
    while (Date.now() < deadline && !controller.signal.aborted) {
      const observation = await observe(page, redact);
      try {
        const reply = await call({op: 'decide', goalId, candidates: [...observation.targets.values()].map(target => target.control), state: observation.state});
        if (reply.status === 'completed') return {goalId, status: 'completed', verified: false};
        if (reply.status !== 'active' || !reply.decision || !reply.decisionId) return fail(reply.status);
        const decision = reply.decision;
        const ack = async (outcome: string) => call({op: 'ack', goalId, decisionId: reply.decisionId, outcome});
        if (decision.action === 'wait') {
          await new Promise<void>(resolve => setTimeout(resolve, Math.min(200, Math.max(1, deadline - Date.now()))));
          await ack('done');
          continue;
        }
        const target = observation.targets.get(decision.targetId || '');
        // Go never decides on a blocked target; refusing one here costs nothing.
        if (!target || target.control.blocked || !target.control.actions.includes(decision.action)) return fail('invalid_target');
        // The fingerprint includes the raw stop-policy text, so a fresh match
        // proves the policy verdict observed above still holds.
        let fresh = false;
        try { fresh = await target.handle.isVisible() && await target.handle.isEnabled() && (await metadata(target.handle))?.fingerprint === target.fingerprint; } catch { /* Detached before issuing an action: safe to reobserve. */ }
        if (!fresh) { await ack('stale'); continue; }
        const timeout = Math.min(3000, Math.max(1, deadline - Date.now()));
        // After issuing a mutation, any error has an uncertain effect. Never retry it.
        try {
          if (controller.signal.aborted) return fail(stopReason);
          switch (decision.action) {
            case 'click': await target.handle.click({timeout}); break;
            case 'check': await target.handle.setChecked(true, {timeout}); break;
            case 'fill': if (!Object.hasOwn(params, decision.parameter || '')) return fail('invalid_parameter'); await target.handle.fill(params[decision.parameter!], {timeout}); break;
            case 'select': if (!Object.hasOwn(params, decision.parameter || '')) return fail('invalid_parameter'); await target.handle.selectOption(params[decision.parameter!], {timeout}); break;
            default: return fail('invalid_action');
          }
        } catch {
          await ack('unknown').catch(() => {});
          return fail('unknown_effect');
        }
        const result = await ack('done');
        if (result.status !== 'active') return fail(result.status);
      } finally { await observation.dispose(); }
    }
    return fail(controller.signal.aborted ? stopReason : 'budget_exhausted');
  } catch (error) {
    if (controller.signal.aborted) return fail(stopReason);
    throw error;
  } finally {
    clearTimeout(deadlineTimer);
    options.signal?.removeEventListener('abort', abort);
    page.removeListener('close', abort);
  }
}
