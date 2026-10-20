import {createHash} from 'node:crypto';
import {realpathSync} from 'node:fs';
import {isAbsolute, join, sep} from 'node:path';

export const protocolVersion = '9l.engine/1';
export const maxFrameBytes = 16 * 1024;
export const maxEvents = 10000;
export const capabilities = ['steps', 'artifact-metadata', 'terminal-outcomes'] as const;
export const targetVersion = '9l.target/1';
// Typed target steps carry this module's own location. The reporter counts only
// those, so a user step titled "9l target ..." stays an ordinary step. Code in the
// worker can still forge one deliberately; the worker remains trusted.
export const targetStepFile = realpathSync(join(__dirname, 'targets.js'));

export function selectedTarget(env: NodeJS.ProcessEnv = process.env) {
  const name = env.NINELIVES_TARGET;
  if (!name || !/^(web|android-fake):[a-zA-Z0-9_-]{1,64}$/.test(name)) {
    throw new Error('9lives target requires --sdk --target web:<app-alias> or android-fake:<app-alias>');
  }
  const [kind, appId] = name.split(':') as ['web' | 'android-fake', string];
  return {name, kind, appId, id: createHash('sha256').update(name).digest('hex')};
}

export function engineIdentity(env: NodeJS.ProcessEnv = process.env) {
  if (env.NINELIVES_ENGINE_PROTOCOL !== protocolVersion) {
    throw new Error('9lives SDK requires a supported Go engine; run this spec with 9l run --sdk');
  }
  const runId = env.NINELIVES_RUN_ID;
  const jobId = env.NINELIVES_JOB_ID;
  const attemptId = env.NINELIVES_ATTEMPT_ID;
  const valid = (value: string | undefined): value is string => !!value && /^[a-zA-Z0-9_-]{1,128}$/.test(value);
  if (!valid(runId) || !valid(jobId) || !valid(attemptId)) {
    throw new Error('9lives SDK requires complete engine attempt identity');
  }
  return {version: protocolVersion, runId, jobId, attemptId};
}

// Go creates a private per-attempt directory and names the evidence file in it.
// Configuration, hooks and dependencies own stdout, so evidence never uses it.
export function engineEventsPath(env: NodeJS.ProcessEnv = process.env) {
  const path = env.NINELIVES_ENGINE_EVENTS;
  if (!path || !isAbsolute(path)) {
    throw new Error('9lives SDK requires an engine evidence channel; run this spec with 9l run --sdk');
  }
  return path;
}

// A skip pin names a test as "<file> › <describe>... › <title>": Playwright's
// title path without the root and project suites, with / separators. Only this
// digest crosses the bridge; Go compares it with SkipPinKey of each --pin-skip.
export function skipPinKey(titlePath: readonly string[], separator = sep) {
  const [, , file = '', ...titles] = titlePath;
  return createHash('sha256').update([file.split(separator).join('/'), ...titles].join(' › ')).digest('hex');
}
