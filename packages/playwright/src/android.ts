import {spawn, type ChildProcess, execFile} from 'node:child_process';
import {createHash, randomUUID} from 'node:crypto';
import {mkdir, mkdtemp, readFile, rm, symlink, writeFile, stat} from 'node:fs/promises';
import {dirname, join, resolve} from 'node:path';
import {homedir} from 'node:os';
import {createServer} from 'node:net';
import {setTimeout as delay} from 'node:timers/promises';
import {targetVersion} from './protocol';
import {targetCapabilities, TargetSetupError, type TargetContext, type TargetDriver, type TargetHandle, type TargetSetupRecord, type TargetRect, type TargetSize, type TargetDiagnostic, type WindowObservation} from './targets';
import {activeDisplay, deviceStatePattern, displayPattern, imePattern, packagePattern, parseDeviceState, parseDisplays, parseIme, parseLogicalDisplays, parsePackageVersion, parsePosture, parseSigner, parseWindows, posturePattern, windowPattern, type DisplayInfo, type Posture} from './android-device';
export {TargetSetupError, type TargetSetupRecord} from './targets';

import {androidVersions, type AndroidRuntime} from './android-runtime';
import {androidDefaults, androidToolPaths, androidToolingPaths} from './android-env';
export {androidVersions, androidRuntimeType, validAndroidRuntime, type AndroidRuntime} from './android-runtime';
export {androidDefaults, androidSdkCandidates, androidSdkVariables, androidToolPaths, androidToolingPaths, androidToolsInstall, androidBuildTools, type AndroidDefaults, type AndroidSource} from './android-env';
/** Explicit aliases for apps without resource IDs. All matches remain strict
 * and scoped to the configured package; arbitrary XPath is not accepted. */
export type AndroidControl = ({accessibilityId: string; text?: never; accessibilityIdSuffix?: never} |
  {text: string; accessibilityId?: never; accessibilityIdSuffix?: never} |
  {accessibilityIdSuffix: string; text?: never; accessibilityId?: never}) & {
  className?: string; selected?: boolean;
};
export type AndroidTargetOptions = {
  appId: string;
  /** The Android SDK root. Defaults to androidDefaults(): ANDROID_HOME,
   * ANDROID_SDK_ROOT, then common install locations. */
  sdkRoot?: string;
  /** The directory whose node_modules holds the pinned Appium and
   * UiAutomator2 packages. Defaults to NINELIVES_ANDROID_TOOLS, else
   * ~/.9l/android-tools. Nothing is downloaded. */
  toolsRoot?: string;
  /** The expected SHA-256 of the installed base APK. May be omitted only
   * when `expected.apk` names the artifact to compare the installation with. */
  apkSHA256?: string; appPackage: string; activity: string;
  controls?: Record<string, AndroidControl>;
  /** What the installed build must be. A digest that differs from the
   * declared one, or from `expected.apk`, a different versionCode or
   * versionName, or another signing certificate stops the test as
   * `setup mismatch` before any application operation. The signer is read
   * by pulling the installed base APK through the local apksigner when
   * declared; versions come from the device's package manager. */
  expected?: {apk?: string; versionCode?: string; versionName?: string; signerSHA256?: string};
  /** Attached device only: the app state expected before each test. The
   * driver reads the app's window count, focus and keyboard visibility after
   * launching the activity; a difference is classified `dirty` (state left by
   * a previous test) instead of running the test against it. */
  baseline?: {windows?: number};
  /** Attached device only: on a dirty baseline, press Back up to this many
   * times until the baseline returns. Recorded in the receipt; never clears
   * data, force-stops or changes settings. */
  recovery?: {back: number};
  /** Opt-in failure diagnostics: the app's window count and focus are read
   * before each gesture and again when an operation fails, and a metadata
   * record (timing, requested point, viewport, windows) is attached to the
   * failed test. With it, a hidden-assertion failure whose window the window
   * manager no longer lists is classified as observer staleness. No
   * screenshot, hierarchy, text or log is captured. */
  diagnostics?: {gestures: boolean};
  /** What the spec needs from the device: a fold posture and/or a minimum
   * active-display width in dp. Another posture or a narrower display is
   * reported as a setup mismatch before any application operation. */
  requires?: {posture?: 'opened' | 'half-opened' | 'closed' | 'flat'; minWidthDp?: number};
} & ({
  avd: string; apk: string; headless?: boolean; deviceSerial?: never;
} | {
  /** Experimental attachment: installed, single-base APK only. Never resets,
   * reinstalls or terminates the app. Installs the pinned automation helpers. */
  deviceSerial: string; avd?: never; apk?: never; headless?: never;
});

const failure = (reason: string) => new Error(`9lives Android: ${reason}`);
// One fixed script per call; every interpolated value is validated by the
// caller's option checks. The device shell runs it; nothing else does.
const shellArgs = (script: string) => ['shell', 'sh', '-c', `'${script}'`];
/** The host-local lease that gives one owner exclusive use of a device.
 * The driver and installInPlace both take it, so an installation cannot
 * replace the app under a running test or interleave with another installer. */
export function androidLeasePath(serial: string) {
  return join(homedir(), '.9lives', 'android-leases', `9l-android-${createHash('sha256').update(serial).digest('hex')}.lock`);
}
const elementKey = 'element-6066-11e4-a52e-4f735466cecf';
const timeout = (c: TargetContext) => {
  c.signal.throwIfAborted();
  if (c.deadlineMs <= Date.now()) throw failure('deadline');
  return c.deadlineMs - Date.now();
};
// Native command output stays in memory and never becomes an error message,
// diagnostic artifact or receipt. These commands have explicit, fixed arguments.
function command(file: string, args: string[], c: TargetContext, env: NodeJS.ProcessEnv): Promise<string> {
  return new Promise((yes, no) => execFile(file, args, {
    signal: c.signal, timeout: timeout(c), maxBuffer: 64 * 1024, env,
  }, (error, stdout) => error ? no(failure('device_command_failed')) : yes(stdout.trim())));
}
async function freePort() {
  const server = createServer();
  await new Promise<void>((yes, no) => { server.once('error', no); server.listen(0, '127.0.0.1', yes); });
  const port = (server.address() as {port: number}).port;
  await new Promise<void>((yes, no) => server.close(error => error ? no(error) : yes()));
  return port;
}
// The emulator reports its allocated console port to a private local socket
// before boot. This ties adb's serial to our child without relying on custom
// Android properties (which modern images may silently omit).
async function reportedConsole(path: string, c: TargetContext, start: () => ChildProcess) {
  const sockets = new Set<import('node:net').Socket>();
  const server = createServer();
  const signal = AbortSignal.any([c.signal, AbortSignal.timeout(timeout(c))]);
  let abort = () => {};
  let child: ChildProcess | undefined;
  let exited = () => {};
  try {
    await new Promise<void>((yes, no) => { server.once('error', no); server.listen(path, yes); });
    return await new Promise<string>((yes, no) => {
      abort = () => no(failure('console_handshake_aborted'));
      signal.addEventListener('abort', abort, {once: true});
      server.once('error', no);
      server.on('connection', socket => {
        sockets.add(socket); let value = '';
        socket.on('error', () => no(failure('console_handshake_failed')));
        socket.on('data', bytes => {
          value += bytes.toString('ascii');
          if (value.length > 8) no(failure('console_handshake_failed'));
        });
        socket.on('end', () => {
          const port = value.trim();
          if (/^[0-9]{4}$/.test(port) && Number(port) >= 5554 && Number(port) <= 5682 && Number(port) % 2 === 0) yes(port);
          else no(failure('foreign_emulator'));
        });
      });
      if (signal.aborted) abort(); else {
        child = start();
        exited = () => no(failure('emulator_exited_before_console'));
        child.once('error', exited);
        child.once('exit', exited);
        if (child.exitCode !== null || child.signalCode !== null) exited();
      }
    });
  } finally {
    signal.removeEventListener('abort', abort);
    child?.removeListener('error', exited);
    child?.removeListener('exit', exited);
    for (const socket of sockets) socket.destroy();
    if (server.listening) await new Promise<void>(yes => server.close(() => yes()));
  }
}
async function stop(child?: ChildProcess) {
  if (!child || !child.pid || child.exitCode !== null || child.signalCode !== null) return;
  const exited = new Promise<void>(yes => child.once('exit', () => yes()));
  child.kill('SIGTERM');
  const timer = setTimeout(() => child.kill('SIGKILL'), 1500);
  try { await exited; } finally { clearTimeout(timer); }
}

/** Optional local Android adapter. Appium/UiAutomator2 owns native automation;
 * Go still owns scheduling/deadlines/receipts and this worker owns all handles.
 * No emulator, server, action or assertion is retried. */
export function androidTargetDriver(options: AndroidTargetOptions): TargetDriver {
  const physical = options.deviceSerial !== undefined;
  const controls = structuredClone(options.controls ?? {});
  if (!/^[a-zA-Z0-9_-]{1,64}$/.test(options.appId) ||
      (physical ? !/^[a-zA-Z0-9_.:-]{1,128}$/.test(options.deviceSerial!) || options.deviceSerial!.startsWith('emulator-') || options.avd !== undefined || options.apk !== undefined
        : !/^[a-zA-Z0-9_.-]{1,100}$/.test(options.avd!)) ||
      !/^[a-zA-Z][a-zA-Z0-9_]*(\.[a-zA-Z0-9_]+)+$/.test(options.appPackage) ||
      !/^[a-zA-Z0-9_.]+$/.test(options.activity) || (options.apkSHA256 === undefined ? options.expected?.apk === undefined : !/^[a-f0-9]{64}$/.test(options.apkSHA256))) throw failure('invalid_configuration');
  const expected = options.expected;
  if (expected !== undefined && (Object.keys(expected).some(k => !['apk', 'versionCode', 'versionName', 'signerSHA256'].includes(k)) || Object.keys(expected).length === 0 ||
      (expected.apk !== undefined && (typeof expected.apk !== 'string' || !expected.apk)) ||
      (expected.versionCode !== undefined && !/^[0-9]{1,10}$/.test(expected.versionCode)) ||
      (expected.versionName !== undefined && !/^[A-Za-z0-9._+-]{1,64}$/.test(expected.versionName)) ||
      (expected.signerSHA256 !== undefined && !/^[a-f0-9]{64}$/.test(expected.signerSHA256)))) throw failure('invalid_configuration');
  // Resolved at open: the declared artifact's digest wins over apkSHA256.
  let expectedDigest = options.apkSHA256 ?? '';
  if (Object.keys(controls).length > 128) throw failure('invalid_controls');
  const expectedWindows = options.baseline?.windows ?? 1;
  const backPresses = options.recovery?.back ?? 0;
  if ((options.baseline !== undefined && (Object.keys(options.baseline).some(k => k !== 'windows') || !Number.isInteger(expectedWindows) || expectedWindows < 1 || expectedWindows > 16)) ||
      (options.recovery !== undefined && (Object.keys(options.recovery).some(k => k !== 'back') || !Number.isInteger(backPresses) || backPresses < 1 || backPresses > 16)) ||
      ((options.baseline !== undefined || options.recovery !== undefined) && !physical) ||
      (options.diagnostics !== undefined && (Object.keys(options.diagnostics).some(k => k !== 'gestures') || typeof options.diagnostics.gestures !== 'boolean'))) throw failure('invalid_configuration');
  const diagnostics = options.diagnostics?.gestures === true;
  const requires = options.requires;
  if (requires !== undefined && (Object.keys(requires).some(k => !['posture', 'minWidthDp'].includes(k)) || Object.keys(requires).length === 0 ||
      (requires.posture !== undefined && !['opened', 'half-opened', 'closed', 'flat'].includes(requires.posture)) ||
      (requires.minWidthDp !== undefined && (!Number.isInteger(requires.minWidthDp) || requires.minWidthDp < 1 || requires.minWidthDp > 10000)))) throw failure('invalid_configuration');
  for (const [id, control] of Object.entries(controls)) {
    if (!/^[a-zA-Z_][a-zA-Z0-9_]{0,127}$/.test(id) || !control ||
        Object.keys(control).some(k => !['text', 'accessibilityId', 'accessibilityIdSuffix', 'className', 'selected'].includes(k)) ||
        [control.text, control.accessibilityId, control.accessibilityIdSuffix].filter(v => v !== undefined).length !== 1 ||
        typeof (control.text ?? control.accessibilityId ?? control.accessibilityIdSuffix) !== 'string' ||
        !/^[^\x00-\x1f\x7f]{1,128}$/.test(control.text ?? control.accessibilityId ?? control.accessibilityIdSuffix!) ||
        (control.className !== undefined && !/^android\.(view|widget)\.[A-Za-z]{1,64}$/.test(control.className)) ||
        (control.selected !== undefined && typeof control.selected !== 'boolean')) throw failure('invalid_controls');
  }
  // Explicit roots win; omitted ones come from the same discovery doctor reports.
  const defaults = options.sdkRoot === undefined || options.toolsRoot === undefined ? androidDefaults() : {};
  const sdkRoot = options.sdkRoot ?? defaults.sdkRoot, toolsRoot = options.toolsRoot ?? defaults.toolsRoot;
  if (!sdkRoot) throw failure('sdk_not_found');
  if (!toolsRoot) throw failure('tools_not_found');
  const sdk = resolve(sdkRoot), tools = resolve(toolsRoot);
  const sdkTools = androidToolPaths(sdk), tooling = androidToolingPaths(tools);
  const adb = sdkTools.adb, emulatorPath = sdkTools.emulator;
  // Explicit environment allowlist: no application/provider credentials reach
  // Appium, adb or the emulator. Go --pass-env remains necessary for test data.
  const env: NodeJS.ProcessEnv = {};
  for (const key of ['PATH', 'HOME', 'TMPDIR', 'JAVA_HOME', 'SYSTEMROOT']) if (process.env[key]) env[key] = process.env[key];
  env.ANDROID_HOME = sdk; env.ANDROID_SDK_ROOT = sdk;
  let emulator: ChildProcess | undefined, server: ChildProcess | undefined;
  let directory: string | undefined, serial = '', base = '', session = '';
  let closing: Promise<void> | undefined, opening: Promise<void> | undefined, closed = false, evidence: AndroidRuntime | undefined;
  const controller = new AbortController();
  const owner = randomUUID();
  let lease: string | undefined, helperOwned = false, systemPort = 0;
  const policies = new Map<string, string>();
  const mockStore = '/data/local/tmp/mock_apps.json';
  const mockBackup = `/data/local/tmp/9l-${owner}-mock-apps.json`;
  let mockStorePresent: boolean | undefined, mockPermission: string | undefined;
  const shell = shellArgs;
  // Device-state reads filter on the device and stay in memory. They never
  // request, store or bypass a credential, and never change lock settings.
  // A failed device read during cancellation, past the deadline or after
  // close is that interruption, never a reading of the device.
  const interrupted = (c: TargetContext) => c.signal.aborted || Date.now() >= c.deadlineMs || closed;
  const readDeviceState = async (c: TargetContext) => parseDeviceState(await adbRun(shell(`(dumpsys window; dumpsys power) 2>/dev/null | grep -E "${deviceStatePattern}" | head -c 4096`), c));
  // A dump that yields neither a window nor a focus line was not observed
  // (unavailable or empty); it is never read as zero windows. Nor was one cut
  // at the byte bound: only output ending in the completion marker counts.
  const windowsEnd = '9l-windows-end';
  const readWindows = async (c: TargetContext): Promise<WindowObservation | undefined> => {
    const dump = await adbRun(shell(`{ dumpsys window windows 2>/dev/null | grep -E "${windowPattern}"; echo ${windowsEnd}; } | head -c 8192`), c);
    if (!dump.endsWith(windowsEnd)) return undefined;
    const windows = parseWindows(dump.slice(0, -windowsEnd.length), options.appPackage);
    return windows.windows === 0 && windows.focus === 'unknown' ? undefined : windows;
  };
  // With diagnostics, the window state before the last gesture; a read that
  // fails leaves the gesture to proceed and the observation absent. The
  // observation stays as diagnostic metadata, but any operation other than a
  // visibility read between the gesture and a hidden assertion disarms the
  // observer heuristic: it describes a dismissal sequence (gesture, then the
  // control should be gone), nothing else.
  // A touch's requested point stays with its observation, so a failure
  // after the touch (the hidden assertion) still reports where it was sent;
  // any other gesture replaces it.
  let lastGesture: WindowObservation | undefined, lastPoint: {x: number; y: number} | undefined, armed = false;
  const observeGesture = async (c: TargetContext, point?: {x: number; y: number}) => {
    if (!diagnostics) return;
    lastPoint = point;
    try { lastGesture = await readWindows(c); } catch { lastGesture = undefined; }
    armed = lastGesture !== undefined;
  };
  const disarm = () => { armed = false; };
  // A baseline reading is clean only when every component was observed as
  // declared; a difference is a dirty reason; anything else is unknown and
  // can neither classify the state nor count as a restored baseline.
  const readBaseline = async (c: TargetContext): Promise<'clean' | 'unknown' | 'foreign-focus' | 'extra-window' | 'ime-showing'> => {
    const windows = await readWindows(c);
    if (!windows) return 'unknown';
    const ime = parseIme(await adbRun(shell(`dumpsys input_method 2>/dev/null | grep -E "${imePattern}" | head -c 1024`), c));
    if (windows.focus === 'foreign') return 'foreign-focus';
    if (windows.windows > expectedWindows) return 'extra-window';
    if (ime === 'shown') return 'ime-showing';
    return windows.focus === 'app' && windows.windows === expectedWindows && ime === 'hidden' ? 'clean' : 'unknown';
  };
  const dirty = (reading: Awaited<ReturnType<typeof readBaseline>>): reading is 'foreign-focus' | 'extra-window' | 'ime-showing' => reading !== 'clean' && reading !== 'unknown';
  let setupRecord: TargetSetupRecord | undefined;
  // The display the session targets. Screenshots name it explicitly, so a
  // multi-display device never answers with its first display by default.
  // The recorded size and density are the display's current logical layout
  // (rotation and density overrides applied) when the dump reports one, and
  // its physical metrics otherwise. Read before any session can launch the
  // app, so a mismatch changes nothing.
  let display: DisplayInfo | undefined, posture: Posture = 'unknown';
  // A dump cut at its byte bound may have lost a display, so it names none:
  // only output ending in the completion marker is discovery data.
  const displaysEnd = '9l-displays-end';
  const readDisplays = async (c: TargetContext) => {
    const output = await adbRun(shell(`{ dumpsys display 2>/dev/null | grep -E "${displayPattern}"; echo ${displaysEnd}; } | head -c 32768`), c);
    const dump = output.endsWith(displaysEnd) ? output.slice(0, -displaysEnd.length) : '';
    display = activeDisplay(parseDisplays(dump));
    const logical = display && parseLogicalDisplays(dump).get(display.id);
    if (display && logical) display = {...display, ...logical};
    posture = parsePosture(await adbRun(shell(`dumpsys device_state 2>&1 | grep -E "${posturePattern}" | head -c 2048`), c));
    if (requires?.posture !== undefined && posture !== requires.posture) throw new TargetSetupError({classification: 'mismatch', reason: 'posture', stage: 'open'});
    if (requires?.minWidthDp !== undefined && (!display || Math.floor(display.width * 160 / display.density) < requires.minWidthDp)) throw new TargetSetupError({classification: 'mismatch', reason: 'display-width', stage: 'open'});
  };
  // The PNG stays in memory and leaves only as a digest.
  // Closing the driver cancels a pending capture, as it does a request.
  const screenDigest = (c: TargetContext) => new Promise<string>((yes, no) => execFile(adb, ['-s', serial, 'exec-out', 'screencap', '-p', ...(display ? ['-d', display.id] : [])], {
    signal: AbortSignal.any([c.signal, controller.signal]), timeout: timeout(c), maxBuffer: 64 * 1024 * 1024, env, encoding: 'buffer',
  }, (error, stdout) => {
    if (error || !Buffer.isBuffer(stdout) || stdout.length < 8 || stdout.subarray(0, 8).toString('latin1') !== '\x89PNG\r\n\x1a\n') return no(failure('screenshot_failed'));
    yes(createHash('sha256').update(stdout).digest('hex'));
  }));
  const installedPath = async (c: TargetContext) => {
    const path = await adbRun(['shell', 'pm', 'path', options.appPackage], c);
    // Split builds require a digest of the entire installation; fail closed.
    if (!/^package:(\/data\/app\/[A-Za-z0-9_./=+~\-]+\/base\.apk)$/.test(path)) throw failure('unsupported_installed_build');
    return path.slice(8);
  };
  // A wrong or stale installed build is a setup mismatch, not a driver fault.
  const installedBuild = async (c: TargetContext) => {
    if (await adbRun(['get-state'], c) !== 'device' || await adbRun(['shell', 'getprop', 'ro.kernel.qemu'], c) === '1') throw failure('not_physical_device');
    const digest = await adbRun(['shell', 'sha256sum', await installedPath(c)], c);
    if (digest.split(/\s+/)[0] !== expectedDigest) throw new TargetSetupError({classification: 'mismatch', reason: 'build-digest', stage: 'open'});
  };
  // apksigner prints only certificate facts; the first signer's digest is kept.
  const signerOf = async (apk: string, c: TargetContext) => {
    const signer = parseSigner(await command(sdkTools.apksigner, ['verify', '--print-certs', apk], c, env));
    if (!signer) throw failure('unreadable_signer');
    return signer;
  };
  let versionCode = 'unknown', versionName = 'unknown', signer = 'unknown';
  // Version from the package manager; the signer from the local APK (owned
  // emulator) or, when declared, from the installed base APK pulled into the
  // attempt's private scratch directory and removed with it.
  const buildIdentity = async (c: TargetContext, localAPK?: string) => {
    ({versionCode, versionName} = parsePackageVersion(await adbRun(shell(`dumpsys package ${options.appPackage} 2>/dev/null | grep -E "${packagePattern}" | head -c 2048`), c)));
    if (expected?.versionCode !== undefined && versionCode !== expected.versionCode) throw new TargetSetupError({classification: 'mismatch', reason: 'build-version', stage: 'open'});
    if (expected?.versionName !== undefined && versionName !== expected.versionName) throw new TargetSetupError({classification: 'mismatch', reason: 'build-version', stage: 'open'});
    if (localAPK) signer = await signerOf(localAPK, c);
    else if (expected?.signerSHA256 !== undefined) {
      const pulled = join(directory!, 'installed.apk');
      await adbRun(['pull', await installedPath(c), pulled], c);
      if (createHash('sha256').update(await readFile(pulled)).digest('hex') !== expectedDigest) throw failure('pulled_build_changed');
      signer = await signerOf(pulled, c);
      await rm(pulled, {force: true});
    }
    if (expected?.signerSHA256 !== undefined && signer !== expected.signerSHA256) throw new TargetSetupError({classification: 'mismatch', reason: 'build-signer', stage: 'open'});
  };
  const children = () => [emulator, server].filter((child): child is ChildProcess => !!child);
  const check = (c: TargetContext) => {
    timeout(c);
    if (closed || children().some(child => child.exitCode !== null || child.signalCode !== null)) throw failure('runtime_unavailable');
  };
  const adbRun = (args: string[], c: TargetContext) => command(adb, ['-s', serial, ...args], c, env);
  const request = async (path: string, c: TargetContext, method = 'GET', body?: unknown): Promise<any> => {
    check(c);
    const signal = AbortSignal.any([c.signal, controller.signal, AbortSignal.timeout(timeout(c))]);
    let response: Response;
    try {
      response = await fetch(`${base}${path}`, {method, signal, headers: {'content-type': 'application/json'}, ...(body === undefined ? {} : {body: JSON.stringify(body)})});
    } catch { throw failure('transport_failed'); }
    // Bound every response before parsing; never retain or surface server text.
    const reader = response.body?.getReader();
    if (!reader) throw failure('invalid_response');
    const route = path.endsWith('/elements') ? 'find' : path.endsWith('/clear') ? 'clear' : path.endsWith('/value') ? 'value' : 'other';
    const chunks: Uint8Array[] = []; let size = 0;
    for (;;) {
      const {done, value} = await reader.read();
      if (done) break;
      size += value.length;
      if (size > 64 * 1024) { await reader.cancel(); throw failure(`response_limit_${route}`); }
      chunks.push(value);
    }
    let data: any;
    try { data = JSON.parse(Buffer.concat(chunks).toString('utf8')); } catch { throw failure('invalid_response'); }
    if (!response.ok || !data || !Object.hasOwn(data, 'value') || data.value?.error) {
      const known = ['invalid element state', 'invalid selector', 'no such element', 'stale element reference', 'unknown command', 'unsupported operation', 'timeout', 'unknown error'];
      const code = known.includes(data?.value?.error) ? data.value.error.replaceAll(' ', '_') : 'unknown';
      throw failure(`driver_command_failed_${code}`);
    }
    check(c);
    return data.value;
  };
  const endpoint = (path: string, c: TargetContext, method = 'GET', body?: unknown) => {
    if (!session) throw failure('session_unavailable');
    return request(`/session/${session}${path}`, c, method, body);
  };
  const matches = async (id: string, c: TargetContext): Promise<string[]> => {
    if (!/^[a-zA-Z_][a-zA-Z0-9_]{0,127}$/.test(id)) throw failure('invalid_resource_id');
    const control = Object.hasOwn(controls, id) ? controls[id] : undefined;
    let selector = `new UiSelector().packageName(${JSON.stringify(options.appPackage)})`;
    if (control) {
      if (control.accessibilityIdSuffix !== undefined) {
        const literal = control.accessibilityIdSuffix.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
        selector += `.descriptionMatches(${JSON.stringify(`.*${literal}$`)})`;
      } else selector += control.accessibilityId === undefined ? `.text(${JSON.stringify(control.text)})` : `.description(${JSON.stringify(control.accessibilityId)})`;
      if (control.className) selector += `.className(${JSON.stringify(control.className)})`;
      if (control.selected !== undefined) selector += `.selected(${control.selected})`;
    }
    const result = await endpoint('/elements', c, 'POST', control ? {using: '-android uiautomator', value: selector} : {using: 'id', value: `${options.appPackage}:id/${id}`});
    if (!Array.isArray(result) || result.length > 512 || result.some(value => typeof value?.[elementKey] !== 'string' || !/^[a-zA-Z0-9_-]{1,128}$/.test(value[elementKey]))) throw failure('invalid_elements');
    return result.map(value => value[elementKey]);
  };
  const resolveOne = async (id: string, c: TargetContext) => {
    const result = await matches(id, c);
    if (result.length !== 1) throw failure(result.length ? 'ambiguous_resource_id' : 'missing_resource_id');
    return result[0];
  };
  const handle = (id: string): TargetHandle => ({
    tap: async c => { const ref = await resolveOne(id, c); await observeGesture(c); await endpoint(`/element/${ref}/click`, c, 'POST', {}); },
    fill: async (value, c) => {
      disarm();
      const ref = await resolveOne(id, c);
      await endpoint(`/element/${ref}/clear`, c, 'POST', {});
      await endpoint(`/element/${ref}/value`, c, 'POST', {text: value});
    },
    visible: async c => {
      const value = await endpoint(`/element/${await resolveOne(id, c)}/displayed`, c);
      if (typeof value !== 'boolean') throw failure('invalid_property'); return value;
    },
    text: async c => {
      disarm();
      const value = await endpoint(`/element/${await resolveOne(id, c)}/text`, c);
      if (typeof value !== 'string' || Buffer.byteLength(value) > 4096) throw failure('invalid_property'); return value;
    },
    state: async (name, c) => {
      disarm();
      const value = await endpoint(`/element/${await resolveOne(id, c)}/attribute/${name}`, c);
      if (value !== 'true' && value !== 'false' && typeof value !== 'boolean') throw failure('unavailable_state');
      return value === true || value === 'true';
    },
    // One supported UiAutomator2 gesture inside the resolved element; the
    // driver reports whether more content exists, which is not retained.
    scroll: async (direction, percent, c) => {
      const ref = await resolveOne(id, c);
      await observeGesture(c);
      await endpoint('/execute/sync', c, 'POST', {script: 'mobile: scrollGesture', args: [{elementId: ref, direction, percent}]});
    },
    bounds: async c => { disarm(); return rect(await endpoint(`/element/${await resolveOne(id, c)}/rect`, c)); },
  });
  const pixel = (value: unknown): value is number => Number.isInteger(value) && (value as number) >= 0 && (value as number) <= 65_535;
  const offset = (value: unknown): value is number => Number.isInteger(value) && (value as number) >= -65_535 && (value as number) <= 65_535;
  // Only finite numbers are rounded: a null or boolean field is a malformed
  // response, never a zero. Origins may be negative for a partly scrolled-out control.
  const rect = (value: any): TargetRect => {
    const fields = [value?.x, value?.y, value?.width, value?.height];
    if (!fields.every(field => typeof field === 'number' && Number.isFinite(field))) throw failure('invalid_property');
    const r = {x: Math.round(value.x), y: Math.round(value.y), width: Math.round(value.width), height: Math.round(value.height)};
    if (!offset(r.x) || !offset(r.y) || !pixel(r.width) || !pixel(r.height)) throw failure('invalid_property');
    return r;
  };
  const keyboardShown = async (c: TargetContext) => {
    check(c);
    const ime = parseIme(await adbRun(shell(`dumpsys input_method 2>/dev/null | grep -E "${imePattern}" | head -c 1024`), c));
    if (ime === 'unknown') throw failure('unavailable_state');
    return ime === 'shown';
  };
  const appCommand = (script: 'mobile: terminateApp' | 'mobile: activateApp', c: TargetContext) =>
    endpoint('/execute/sync', c, 'POST', {script, args: [{appId: options.appPackage}]});
  const cleanup = async () => {
    await Promise.all([stop(emulator), stop(server)]);
    // Aborting HTTP alone cannot stop an already dispatched native action.
    // Only the exclusively leased automation helper is stopped on a phone.
    if (physical && lease) {
      const c = {signal: new AbortController().signal, deadlineMs: Date.now() + 15000};
      if (helperOwned) {
        for (const pkg of ['io.appium.uiautomator2.server', 'io.appium.uiautomator2.server.test']) {
          await adbRun(['shell', 'am', 'force-stop', pkg], c);
        }
        const alive = await adbRun(['shell', 'sh', '-c', "'pidof io.appium.uiautomator2.server io.appium.uiautomator2.server.test || true'"], c);
        if (alive) throw failure('helper_cleanup_failed');
        // Removing only our own forward never touches another device/session.
        if (systemPort) {
          const forwards = await adbRun(['forward', '--list'], c);
          if (forwards.split('\n').some(line => line.split(/\s+/)[0] === serial && line.split(/\s+/)[1] === `tcp:${systemPort}`)) {
            await adbRun(['forward', '--remove', `tcp:${systemPort}`], c);
          }
        }
        if (mockPermission) await adbRun(['shell', 'appops', 'set', 'io.appium.settings', 'android:mock_location', mockPermission], c);
        if (mockStorePresent !== undefined) {
          await adbRun(shell(mockStorePresent ? `cp ${mockBackup} ${mockStore} && rm ${mockBackup}` : `rm -f ${mockStore}`), c);
          mockStorePresent = undefined;
        }
        for (const [key, value] of policies) {
          await adbRun(['shell', 'settings', value === 'null' ? 'delete' : 'put', 'global', key, ...(value === 'null' ? [] : [value])], c);
          if (await adbRun(['shell', 'settings', 'get', 'global', key], c) !== value) throw failure('policy_restore_failed');
        }
      }
      if (mockStorePresent === true) await adbRun(shell(`rm -f ${mockBackup}`), c);
      mockStorePresent = undefined;
      await rm(lease, {recursive: true}); lease = undefined;
    }
    if (directory) await rm(directory, {recursive: true, force: true});
  };
  const close = () => closing ??= (async () => {
    closed = true;
    controller.abort();
    await Promise.all([stop(emulator), stop(server)]);
    await opening?.catch(() => {});
    await cleanup();
  })();
  return {
    kind: 'android', appId: options.appId, version: targetVersion, capabilities: targetCapabilities,
    runtime: () => evidence,
    open(context) {
      if (opening || closed) return Promise.reject(failure('already_open_or_closed'));
      const c = {...context, signal: AbortSignal.any([context.signal, controller.signal])};
      opening = (async () => {
        check(c);
        let stage = 'configuration';
        try {
          let apkBytes: Buffer | undefined;
          if (expected?.apk !== undefined) {
            const artifact = resolve(expected.apk);
            const info = await stat(artifact);
            if (!info.isFile() || info.size > 256 * 1024 * 1024) throw failure('invalid_expected_apk');
            const digest = createHash('sha256').update(await readFile(artifact)).digest('hex');
            // Two declarations that disagree are a configuration error, not a device finding.
            if (options.apkSHA256 !== undefined && digest !== options.apkSHA256) throw failure('expected_apk_digest_conflict');
            expectedDigest = digest;
          }
          if (!physical) {
            const apk = resolve(options.apk!);
            const info = await stat(apk);
            if (!info.isFile() || info.size > 256 * 1024 * 1024) throw failure('invalid_apk');
            apkBytes = await readFile(apk);
            if (createHash('sha256').update(apkBytes).digest('hex') !== expectedDigest) throw new TargetSetupError({classification: 'mismatch', reason: 'build-digest', stage: 'open'});
          }
          const appiumPkg = JSON.parse(await readFile(tooling.appium, 'utf8'));
          const driverPkg = JSON.parse(await readFile(tooling.uiautomator2, 'utf8'));
          if (appiumPkg.version !== androidVersions.appium || driverPkg.version !== androidVersions.uiautomator2) throw failure('driver_version_mismatch');
          const scratch = process.env.NINELIVES_TARGET_SCRATCH_DIR;
          if (!scratch) throw failure('missing_owned_scratch');
          directory = await mkdtemp(join(scratch, 'android-'));
          check(c);
          env.APPIUM_HOME = directory;
          env.TMPDIR = directory;
          await mkdir(join(directory, 'node_modules'));
          for (const pkg of ['appium', 'appium-uiautomator2-driver']) await symlink(join(tools, 'node_modules', pkg), join(directory, 'node_modules', pkg), 'dir');
          const installedAPK = join(directory, 'fixture.apk');
          if (apkBytes) {
            await writeFile(installedAPK, apkBytes, {mode: 0o400});
            const badging = await command(sdkTools.aapt2, ['dump', 'badging', installedAPK], c, env);
            if (/^package: name='([^']+)'/.exec(badging)?.[1] !== options.appPackage) throw failure('apk_package_mismatch');
          }
          await writeFile(join(directory, 'package.json'), JSON.stringify({private: true, dependencies: {appium: androidVersions.appium, 'appium-uiautomator2-driver': androidVersions.uiautomator2}}));
          // The emulator atomically reserves its own available console port.
          // Its private handshake binds the reported adb serial to this child.
          check(c);
          if (physical) {
            stage = 'device_attach';
            serial = options.deviceSerial!;
            const lock = androidLeasePath(serial);
            // Never recover a stale lease automatically: a killed worker may have
            // left automation running. Manual inspection is required in that case.
            await mkdir(dirname(lock), {recursive: true, mode: 0o700});
            await mkdir(lock, {mode: 0o700}); lease = lock;
            await installedBuild(c);
            // Version and signer are checked before any device change or session.
            stage = 'build_identity';
            await buildIdentity(c);
            stage = 'device_state';
            // A phone that cannot be read before the test is in an unknown
            // state, not an open failure; cancellation, the deadline and a
            // closed driver still propagate.
            let state: Awaited<ReturnType<typeof readDeviceState>>;
            try { state = await readDeviceState(c); } catch (error) {
              if (interrupted(c)) throw error;
              throw new TargetSetupError({classification: 'unknown', reason: 'device-state-unknown', stage: 'open'});
            }
            if (state === 'locked' || state === 'screen-off') throw new TargetSetupError({classification: 'blocked', reason: state === 'locked' ? 'device-locked' : 'screen-off', stage: 'open'});
            stage = 'displays';
            await readDisplays(c);
            stage = 'device_attach';
            if (await adbRun(['shell', 'sh', '-c', "'pidof io.appium.uiautomator2.server io.appium.uiautomator2.server.test || true'"], c)) throw failure('device_automation_in_use');
            for (const key of ['hidden_api_policy_pre_p_apps', 'hidden_api_policy_p_apps', 'hidden_api_policy']) {
              const value = await adbRun(['shell', 'settings', 'get', 'global', key], c);
              if (!/^(null|-1|0|1|2)$/.test(value)) throw failure('unsupported_policy');
              policies.set(key, value);
            }
            // The pinned Android driver sets mock-location permission on its
            // Settings helper even with skipDeviceInitialization. Preserve the
            // one helper permission and its private index without reading content.
            if (await adbRun(shell('if pm path io.appium.settings >/dev/null 2>&1; then echo present; else echo absent; fi'), c) === 'present') {
              const op = await adbRun(['shell', 'appops', 'get', 'io.appium.settings', 'android:mock_location'], c);
              mockPermission = /(?:MOCK_LOCATION|mock_location): (allow|deny|ignore|default|foreground)/.exec(op)?.[1];
              if (!mockPermission && /^No operations\./.test(op)) mockPermission = 'default';
              if (!mockPermission) throw failure('unsupported_mock_permission');
            }
            const backup = await adbRun(shell(`if [ -f ${mockStore} ]; then cp ${mockStore} ${mockBackup} && echo present; else echo absent; fi`), c);
            if (!['present', 'absent'].includes(backup)) throw failure('mock_backup_failed');
            mockStorePresent = backup === 'present';
            // Leave only recovery metadata if the host worker is killed. A stale
            // lease blocks reuse rather than silently abandoning device changes.
            await writeFile(join(lease, 'recovery.json'), JSON.stringify({policies: Object.fromEntries(policies), mockPermission, mockStorePresent, mockBackup}), {mode: 0o600});
          } else {
            stage = 'emulator_boot';
            // AF_UNIX paths on macOS are limited to 104 bytes, including NUL.
            const consoleSocket = join(directory, 'c');
            if (Buffer.byteLength(consoleSocket) > 103) throw failure('scratch_path_too_long');
            const port = await reportedConsole(consoleSocket, c, () => {
              emulator = spawn(emulatorPath, ['-avd', options.avd!, '-read-only', '-no-snapshot', '-no-audio', '-report-console', `unix:${consoleSocket}`, ...(options.headless === false ? [] : ['-no-window']), '-gpu', 'swiftshader_indirect'], {env, stdio: 'ignore'});
              emulator.on('error', () => {});
              return emulator;
            });
            serial = `emulator-${port}`;
            for (;;) {
              check(c);
              try {
                const booted = await adbRun(['shell', 'getprop', 'sys.boot_completed'], c);
                if (booted === '1') {
                  break;
                }
              } catch (error) { check(c); if (error instanceof Error && error.message.endsWith('foreign_emulator')) throw error; }
              await delay(250, undefined, {signal: c.signal});
            }
            stage = 'displays';
            await readDisplays(c);
            stage = 'apk_install';
            await adbRun(['install', '--no-streaming', '-r', installedAPK], c);
            stage = 'build_identity';
            await buildIdentity(c, installedAPK);
          }
          const appiumPort = await freePort();
          base = `http://127.0.0.1:${appiumPort}/${owner}`;
          check(c);
          stage = 'server_start';
          server = spawn(process.execPath, [tooling.appiumEntry, '--address', '127.0.0.1', '--port', String(appiumPort), '--base-path', `/${owner}`, '--use-drivers', 'uiautomator2', '--log-level', 'error', '--log-no-colors'], {env, cwd: directory, stdio: 'ignore'});
          server.on('error', () => {});
          for (;;) {
            check(c);
            try { if ((await request('/status', c)).ready === true) break; } catch { check(c); }
            await delay(100, undefined, {signal: c.signal});
          }
          stage = 'session_start';
          systemPort = await freePort();
          helperOwned = physical;
          const value = await request('/session', c, 'POST', {capabilities: {alwaysMatch: {
            platformName: 'Android', 'appium:automationName': 'UiAutomator2',
            'appium:udid': serial, 'appium:appPackage': options.appPackage, 'appium:appActivity': options.activity,
            'appium:systemPort': systemPort, 'appium:noReset': true, 'appium:autoLaunch': true,
            'appium:printPageSourceOnFindFailure': false, 'appium:skipLogcatCapture': true,
            'appium:newCommandTimeout': 60, 'appium:uiautomator2ServerReadTimeout': 15000,
            'appium:showChromedriverLog': false, 'appium:suppressKillServer': true,
            // A deterministic synthetic fixture uses UiAutomator2's empty IME.
            // This does not qualify software-keyboard interaction or layout.
            ...(!physical ? {'appium:hideKeyboard': true} : {}),
            ...(physical ? {
              'appium:fullReset': false, 'appium:dontStopAppOnReset': true,
              'appium:forceAppLaunch': false, 'appium:shouldTerminateApp': false,
              'appium:skipDeviceInitialization': true, 'appium:skipUnlock': true,
              'appium:autoGrantPermissions': false,
            } : {}),
            'appium:settings[waitForIdleTimeout]': 1000,
          }, firstMatch: [{}]}});
          stage = 'session_identity';
          const returnedUDID = value.capabilities?.['appium:udid'] ?? value.capabilities?.udid;
          if (value.capabilities?.['appium:udid'] !== undefined && value.capabilities?.udid !== undefined && value.capabilities['appium:udid'] !== value.capabilities.udid) throw failure('foreign_session');
          if (typeof value.sessionId !== 'string' || !/^[a-zA-Z0-9-]{1,128}$/.test(value.sessionId) || returnedUDID !== serial) throw failure('foreign_session');
          session = value.sessionId;
          stage = 'runtime_identity';
          let identity: {emulator: string} | {device: 'physical'; deviceHash: string};
          if (physical) {
            await installedBuild(c);
            await adbRun(['shell', 'am', 'start', '-W', '-n', `${options.appPackage}/${options.activity}`], c);
            stage = 'baseline';
            let reading = await readBaseline(c);
            if (dirty(reading) && backPresses > 0) {
              // Declared, recorded and bounded: Back until the baseline is
              // observed again. An unknown reading after Back is not a
              // restored baseline.
              const reason = reading;
              let presses = 0;
              // A failed Back or a failed read after one leaves an unknown,
              // unrestored baseline: the presses already completed stay on the
              // record. Cancellation, the deadline and a closed driver still propagate.
              while (dirty(reading) && presses < backPresses) {
                check(c);
                try { await adbRun(['shell', 'input', 'keyevent', 'KEYCODE_BACK'], c); } catch (error) {
                  if (interrupted(c)) throw error;
                  reading = 'unknown';
                  break;
                }
                presses++;
                await delay(500, undefined, {signal: c.signal});
                try { reading = await readBaseline(c); } catch (error) {
                  if (interrupted(c)) throw error;
                  reading = 'unknown';
                }
              }
              const recovery = {method: 'back' as const, presses, restored: reading === 'clean'};
              if (reading !== 'clean') throw new TargetSetupError({classification: 'dirty', reason, stage: 'open', recovery});
              setupRecord = {classification: 'dirty', reason, stage: 'open', recovery};
            } else if (dirty(reading)) throw new TargetSetupError({classification: 'dirty', reason: reading, stage: 'open'});
            identity = {device: 'physical', deviceHash: createHash('sha256').update(serial).digest('hex')};
          } else {
            const emulatorVersion = await command(emulatorPath, ['-version'], c, env);
            const version = /Android emulator version ([0-9.]+)/.exec(emulatorVersion)?.[1];
            if (!version) throw failure('unknown_emulator_version');
            identity = {emulator: version};
          }
          evidence = {
            appPackage: options.appPackage, apkSHA256: expectedDigest, versionCode, versionName, signer,
            api: await adbRun(['shell', 'getprop', 'ro.build.version.sdk'], c),
            release: await adbRun(['shell', 'getprop', 'ro.build.version.release'], c),
            abi: await adbRun(['shell', 'getprop', 'ro.product.cpu.abi'], c),
            display: display?.id ?? 'unknown', displayWidth: String(display?.width ?? 0), displayHeight: String(display?.height ?? 0), density: String(display?.density ?? 0), posture,
            ...identity, ...androidVersions, worker: createHash('sha256').update(`${owner}:${process.pid}`).digest('hex'),
          };
        } catch (error) {
          closed = true; await cleanup();
          if (error instanceof TargetSetupError) throw error;
          throw failure(`open_failed_${stage}`);
        }
      })();
      return opening;
    },
    async reset(c) {
      disarm();
      if (physical) throw failure('reset_unsupported_on_attached_device');
      check(c);
      if (!session) throw failure('session_unavailable');
      await adbRun(['shell', 'am', 'force-stop', options.appPackage], c);
      if (await adbRun(['shell', 'pm', 'clear', options.appPackage], c) !== 'Success') throw failure('reset_failed');
      await adbRun(['shell', 'am', 'start', '-W', '-n', `${options.appPackage}/${options.activity}`], c);
    },
    async observeTestId(id, c) { return (await matches(id, c)).slice(0, 2).map(() => handle(id)); },
    setup: () => setupRecord,
    async back(c) { await observeGesture(c); await endpoint('/back', c, 'POST', {}); },
    async keyboardShown(c) { disarm(); return keyboardShown(c); },
    // Back dismisses a shown keyboard without touching the selected input
    // method; the result is read back rather than assumed.
    async hideKeyboard(c) {
      disarm();
      if (!await keyboardShown(c)) return;
      await endpoint('/back', c, 'POST', {});
      for (;;) {
        if (!await keyboardShown(c)) return;
        if (Date.now() + 100 >= c.deadlineMs) throw failure('keyboard_not_hidden');
        await delay(100, undefined, {signal: c.signal});
      }
    },
    async tapAt(point, c) {
      if (!pixel(point.x) || !pixel(point.y)) throw failure('invalid_point');
      await observeGesture(c, {x: point.x, y: point.y});
      await endpoint('/actions', c, 'POST', {actions: [{type: 'pointer', id: 'finger1', parameters: {pointerType: 'touch'}, actions: [
        {type: 'pointerMove', duration: 0, x: point.x, y: point.y}, {type: 'pointerDown', button: 0}, {type: 'pause', duration: 50}, {type: 'pointerUp', button: 0},
      ]}]});
    },
    async screenDigest(c) { check(c); return screenDigest(c); },
    async viewport(c): Promise<TargetSize> {
      disarm();
      const r = rect(await endpoint('/window/rect', c));
      if (r.width === 0 || r.height === 0) throw failure('invalid_property');
      return {width: r.width, height: r.height};
    },
    // Terminate is a force-stop of the configured package only; data, the
    // installation and device settings are untouched. Activate relaunches it.
    async terminate(c) { disarm(); await appCommand('mobile: terminateApp', c); },
    async activate(c) { disarm(); await appCommand('mobile: activateApp', c); },
    // After a completed assertion failure: did the phone become unavailable?
    // Read-only; an owned emulator cannot lock itself. With diagnostics, a
    // hidden assertion that failed although the app lost a window since the
    // last gesture is the accessibility snapshot disagreeing with the window
    // manager: observer staleness, attributed to neither the app nor the tap.
    async recheck(c, failed) {
      if (!session) return undefined;
      check(c);
      if (physical) {
        let state: Awaited<ReturnType<typeof readDeviceState>>;
        try { state = await readDeviceState(c); } catch (error) {
          // A phone that cannot be read (disconnected, adb gone) has an unknown
          // state; cancellation, the deadline and a closed driver still propagate.
          if (interrupted(c)) throw error;
          state = 'unknown';
        }
        if (state === 'unknown') return {classification: 'unknown', reason: 'device-state-unknown', stage: 'recheck'};
        if (state !== 'unlocked') return {classification: 'blocked', reason: state === 'locked' ? 'device-locked' : 'screen-off', stage: 'recheck'};
      }
      // Heuristic, opt-in and limited to a dismissal sequence: UiAutomator2
      // does not say which window an element belongs to, so a hidden
      // assertion that fails right after a gesture while the app lost a
      // window is attributed to the snapshot, not proven. The diagnostic
      // keeps the counts either way.
      if (diagnostics && armed && failed.operation === 'assert-hidden' && lastGesture && lastGesture.windows > 0) {
        const now = await readWindows(c);
        if (now && now.windows < lastGesture.windows) return {classification: 'observer', reason: 'window-gone-element-displayed', stage: 'recheck'};
      }
      return undefined;
    },
    // Metadata only: what was asked, how long it took, and the app's windows
    // before the last gesture and now. Nothing is retried and no cause is claimed.
    async diagnose(f, c) {
      if (!diagnostics) return undefined;
      const point = f.point ?? lastPoint;
      const record: TargetDiagnostic = {operation: f.operation, reason: f.reason, elapsedMs: f.elapsedMs, windowsBefore: lastGesture?.windows ?? -1, windowsAfter: -1,
        ...(point ? {x: point.x, y: point.y, point: true as const} : {}), ...(lastGesture ? {focusBefore: lastGesture.focus} : {})};
      if (closed || !session) return record;
      try {
        const after = await readWindows(c);
        if (after) { record.windowsAfter = after.windows; record.focusAfter = after.focus; }
        const r = rect(await endpoint('/window/rect', c));
        if (r.width > 0 && r.height > 0) { record.viewportWidth = r.width; record.viewportHeight = r.height; }
      } catch { /* partial record */ }
      return record;
    },
    close,
  };
}

const maxApkBytes = 256 * 1024 * 1024;

/** Install or update an app on an explicitly selected device without
 * uninstalling or clearing its data. The APK's signing certificate must
 * match the installed app's, read through the local apksigner; a different
 * signer refuses before any device change. Returns the installed identity
 * for the project's reviewed build metadata. Never runs inside a test. */
export async function installInPlace(options: {sdkRoot?: string; deviceSerial: string; apk: string; timeoutMs?: number}) {
  if (!/^[a-zA-Z0-9_.:-]{1,128}$/.test(options.deviceSerial) || options.deviceSerial.startsWith('emulator-') || !options.apk) throw failure('invalid_configuration');
  const sdkRoot = options.sdkRoot ?? androidDefaults().sdkRoot;
  if (!sdkRoot) throw failure('sdk_not_found');
  const sdk = resolve(sdkRoot), {adb, aapt2, apksigner} = androidToolPaths(sdk);
  const env: NodeJS.ProcessEnv = {ANDROID_HOME: sdk, ANDROID_SDK_ROOT: sdk};
  for (const key of ['PATH', 'HOME', 'TMPDIR', 'JAVA_HOME', 'SYSTEMROOT']) if (process.env[key]) env[key] = process.env[key];
  const c: TargetContext = {signal: new AbortController().signal, deadlineMs: Date.now() + Math.min(Math.max(options.timeoutMs ?? 300_000, 1_000), 900_000)};
  const apk = resolve(options.apk);
  const info = await stat(apk);
  if (!info.isFile() || info.size > maxApkBytes) throw failure('invalid_apk');
  // One private snapshot is hashed, inspected, verified and installed, so a
  // build or download replacing the caller's file meanwhile changes nothing.
  const bytes = await readFile(apk);
  if (bytes.length > maxApkBytes) throw failure('invalid_apk');
  const apkSHA256 = createHash('sha256').update(bytes).digest('hex');
  const scratch = await mkdtemp(join(process.env.TMPDIR ?? homedir(), '9l-install-'));
  try {
    const snapshot = join(scratch, 'app.apk');
    await writeFile(snapshot, bytes, {mode: 0o600});
    const badging = await command(aapt2, ['dump', 'badging', snapshot], c, env);
    const appPackage = /^package: name='([a-zA-Z][a-zA-Z0-9_]*(?:\.[a-zA-Z0-9_]+)+)'/m.exec(badging)?.[1];
    if (!appPackage) throw failure('unreadable_package');
    const signer = parseSigner(await command(apksigner, ['verify', '--print-certs', snapshot], c, env));
    if (!signer) throw failure('unreadable_signer');
    const run = (args: string[]) => command(adb, ['-s', options.deviceSerial, ...args], c, env);
    // The same exclusive lease a test takes: never install under a running
    // test or beside another installer. Only the lease this call created is released.
    const lock = androidLeasePath(options.deviceSerial);
    await mkdir(dirname(lock), {recursive: true, mode: 0o700});
    try { await mkdir(lock, {mode: 0o700}); } catch { throw failure('device_in_use'); }
    try {
      if (await run(['get-state']) !== 'device' || await run(['shell', 'getprop', 'ro.kernel.qemu']) === '1') throw failure('not_physical_device');
      // pm path exits nonzero for an absent package: a first installation, not a transport failure.
      const path = await run(shellArgs(`pm path ${appPackage} 2>/dev/null || true`));
      if (path) {
        if (!/^package:(\/data\/app\/[A-Za-z0-9_./=+~\-]+\/base\.apk)$/.test(path)) throw failure('unsupported_installed_build');
        const pulled = join(scratch, 'installed.apk');
        await run(['pull', path.slice(8), pulled]);
        const installed = parseSigner(await command(apksigner, ['verify', '--print-certs', pulled], c, env));
        if (installed !== signer) throw failure('signer_mismatch');
      }
      await run(['install', '--no-streaming', '-r', snapshot]);
      const installedPath = await run(['shell', 'pm', 'path', appPackage]);
      if (!/^package:(\/data\/app\/[A-Za-z0-9_./=+~\-]+\/base\.apk)$/.test(installedPath)) throw failure('unsupported_installed_build');
      const digest = (await run(['shell', 'sha256sum', installedPath.slice(8)])).split(/\s+/)[0];
      if (digest !== apkSHA256) throw failure('installed_build_changed');
      const version = parsePackageVersion(await run(shellArgs(`dumpsys package ${appPackage} 2>/dev/null | grep -E "${packagePattern}" | head -c 2048`)));
      return {appPackage, apkSHA256, signer, ...version};
    } finally {
      await rm(lock, {recursive: true, force: true});
    }
  } finally {
    await rm(scratch, {recursive: true, force: true});
  }
}
