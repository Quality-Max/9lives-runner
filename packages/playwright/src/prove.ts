import {createHash} from 'node:crypto';
import {closeSync, openSync, writeSync} from 'node:fs';
import {isAbsolute, join} from 'node:path';
import type {BrowserContext, Request} from '@playwright/test';
import {engineIdentity} from './protocol';

// `9l prove` runs a spec once to observe its fetch/XHR requests, then once per
// injected network fault. Go owns the plan, budgets and verdicts; this module
// only records what the browser context did and applies the one named fault.
// Records go to a private per-test file that Go names and removes. They never
// enter the 9l.engine/1 stream, whose metadata excludes URLs by design.
export const proveProtocol = '9l.prove/1';
export const faultKinds = ['abort', 'http-500', 'empty-json'] as const;
export type FaultKind = typeof faultKinds[number];
export type Target = {method: string; origin: string; path: string};
export type Fault = Target & {id: string; kind: FaultKind};

const maxRecords = 2000;
const maxRecordBytes = 4096;
const maxPathBytes = 2048;
const faultKeys = ['id', 'kind', 'method', 'origin', 'path'];

// Only fetch and XHR over http(s) are candidates. Query strings and fragments
// are dropped: one fault covers every request to the same method, origin and path.
export function requestTarget(request: Pick<Request, 'method' | 'url' | 'resourceType'>): Target | undefined {
  const type = request.resourceType();
  if (type !== 'fetch' && type !== 'xhr') return undefined;
  let url: URL;
  try { url = new URL(request.url()); } catch { return undefined; }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') return undefined;
  const target = {method: request.method(), origin: url.origin, path: url.pathname};
  return validTarget(target) ? target : undefined;
}

function validTarget(target: Target) {
  return /^[A-Z]{1,16}$/.test(target.method) && /^https?:\/\/[^/?#@\s]+$/.test(target.origin)
    && target.path.startsWith('/') && Buffer.byteLength(target.path) <= maxPathBytes
    && !/[\u0000-\u001f\u007f?#]/.test(target.path);
}

export function matches(fault: Target, target: Target | undefined) {
  return !!target && target.method === fault.method && target.origin === fault.origin && target.path === fault.path;
}

// The fault is Go-authored, but it crosses a process boundary: accept exactly
// the closed schema or refuse to run.
export function readFault(raw: string): Fault {
  let value: unknown;
  try { value = JSON.parse(raw); } catch { value = undefined; }
  const fault = value as Fault;
  if (!value || typeof value !== 'object' || Array.isArray(value)
    || Object.keys(value).sort().join() !== [...faultKeys].sort().join()
    || typeof fault.id !== 'string' || !/^fault-[1-9][0-9]{0,3}$/.test(fault.id)
    || !(faultKinds as readonly string[]).includes(fault.kind)
    || typeof fault.method !== 'string' || typeof fault.origin !== 'string' || typeof fault.path !== 'string'
    || !validTarget(fault)) {
    throw new Error('9lives prove received an invalid fault');
  }
  return fault;
}

export class ProveChannel {
  private records = 0;
  private closed = false;
  constructor(private fd: number, readonly fault?: Fault) {}

  record(fields: Record<string, unknown>) {
    if (this.closed || this.records >= maxRecords) return;
    let line = JSON.stringify(fields) + '\n';
    // A record that does not fit marks the file instead of being truncated.
    if (++this.records === maxRecords || Buffer.byteLength(line) > maxRecordBytes) {
      line = JSON.stringify({type: 'overflow'}) + '\n';
      this.records = maxRecords;
    }
    writeSync(this.fd, line);
  }
  close() {
    if (this.closed) return;
    this.closed = true;
    closeSync(this.fd);
  }
}

// Returns undefined outside `9l prove`, so ordinary runs are untouched.
export function openProveChannel(env: NodeJS.ProcessEnv, testInfo: {testId: string; retry: number}): ProveChannel | undefined {
  if (env.NINELIVES_PROVE === undefined) return undefined;
  if (env.NINELIVES_PROVE !== proveProtocol) {
    throw new Error('9lives prove requires a supported Go engine; run this spec with 9l prove');
  }
  engineIdentity(env);
  const dir = env.NINELIVES_PROVE_DIR;
  if (!dir || !isAbsolute(dir)) throw new Error('9lives prove requires an engine evidence directory');
  const fault = env.NINELIVES_PROVE_FAULT === undefined ? undefined : readFault(env.NINELIVES_PROVE_FAULT);
  // One exclusive file per worker process and test attempt; never appended to.
  const name = createHash('sha256').update(`${process.pid}\0${testInfo.testId}\0${testInfo.retry}`).digest('hex').slice(0, 32);
  const channel = new ProveChannel(openSync(join(dir, `${name}.ndjson`), 'wx', 0o600), fault);
  channel.record({type: 'hello', protocol: proveProtocol, mode: fault ? 'fault' : 'observe'});
  return channel;
}

function emptyLike(text: string): string | undefined {
  let value: unknown;
  try { value = JSON.parse(text); } catch { return undefined; }
  if (Array.isArray(value)) return '[]';
  return value !== null && typeof value === 'object' ? '{}' : undefined;
}

function isJSON(contentType: string | undefined) {
  const media = (contentType ?? '').split(';')[0].trim().toLowerCase();
  return media.endsWith('/json') || media.endsWith('+json');
}

export async function instrument(context: BrowserContext, channel: ProveChannel) {
  const fault = channel.fault;
  if (!fault) {
    // Observation is passive: listeners only, so the baseline run is unchanged.
    context.on('request', request => {
      const target = requestTarget(request);
      if (target) channel.record({type: 'request', ...target, resourceType: request.resourceType()});
    });
    context.on('response', response => {
      const target = requestTarget(response.request());
      if (target) channel.record({type: 'response', ...target, status: response.status(), json: isJSON(response.headers()['content-type'])});
    });
    return;
  }
  // Registered when the context is created, so routes the test adds later run
  // first; a request the test fulfills itself is never faulted.
  await context.route('**/*', async route => {
    if (!matches(fault, requestTarget(route.request()))) return route.fallback();
    if (fault.kind === 'abort') {
      channel.record({type: 'applied', fault: fault.id});
      return route.abort('failed');
    }
    if (fault.kind === 'http-500') {
      channel.record({type: 'applied', fault: fault.id});
      return route.fulfill({status: 500, body: ''});
    }
    let response;
    try {
      response = await route.fetch();
    } catch {
      channel.record({type: 'not-applicable', fault: fault.id});
      return route.continue().catch(() => {});
    }
    const empty = emptyLike(await response.text().catch(() => ''));
    if (empty === undefined) {
      channel.record({type: 'not-applicable', fault: fault.id});
      return route.fulfill({response});
    }
    channel.record({type: 'applied', fault: fault.id});
    return route.fulfill({response, body: empty});
  });
}
