import {spawn, type ChildProcess, execFile} from 'node:child_process';
import {createHash, randomUUID} from 'node:crypto';
import {mkdir, mkdtemp, readFile, rm, symlink, writeFile, stat} from 'node:fs/promises';
import {join, resolve} from 'node:path';
import {homedir} from 'node:os';
import {createServer} from 'node:net';
import {setTimeout as delay} from 'node:timers/promises';
import {targetVersion} from './protocol';
import {targetCapabilities, type TargetContext, type TargetDriver, type TargetHandle} from './targets';

import {androidVersions, type AndroidRuntime} from './android-runtime';
export {androidVersions, androidRuntimeType, validAndroidRuntime, type AndroidRuntime} from './android-runtime';
/** Explicit aliases for apps without resource IDs. All matches remain strict
 * and scoped to the configured package; arbitrary XPath is not accepted. */
export type AndroidControl = ({accessibilityId: string; text?: never; accessibilityIdSuffix?: never} |
  {text: string; accessibilityId?: never; accessibilityIdSuffix?: never} |
  {accessibilityIdSuffix: string; text?: never; accessibilityId?: never}) & {
  className?: string; selected?: boolean;
};
export type AndroidTargetOptions = {
  appId: string; sdkRoot: string; toolsRoot: string;
  apkSHA256: string; appPackage: string; activity: string;
  controls?: Record<string, AndroidControl>;
} & ({
  avd: string; apk: string; headless?: boolean; deviceSerial?: never;
} | {
  /** Experimental attachment: installed, single-base APK only. Never resets,
   * reinstalls or terminates the app. Installs the pinned automation helpers. */
  deviceSerial: string; avd?: never; apk?: never; headless?: never;
});

const failure = (reason: string) => new Error(`9lives Android: ${reason}`);
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
      !/^[a-zA-Z0-9_.]+$/.test(options.activity) || !/^[a-f0-9]{64}$/.test(options.apkSHA256)) throw failure('invalid_configuration');
  if (Object.keys(controls).length > 128) throw failure('invalid_controls');
  for (const [id, control] of Object.entries(controls)) {
    if (!/^[a-zA-Z_][a-zA-Z0-9_]{0,127}$/.test(id) || !control ||
        Object.keys(control).some(k => !['text', 'accessibilityId', 'accessibilityIdSuffix', 'className', 'selected'].includes(k)) ||
        [control.text, control.accessibilityId, control.accessibilityIdSuffix].filter(v => v !== undefined).length !== 1 ||
        typeof (control.text ?? control.accessibilityId ?? control.accessibilityIdSuffix) !== 'string' ||
        !/^[^\x00-\x1f\x7f]{1,128}$/.test(control.text ?? control.accessibilityId ?? control.accessibilityIdSuffix!) ||
        (control.className !== undefined && !/^android\.(view|widget)\.[A-Za-z]{1,64}$/.test(control.className)) ||
        (control.selected !== undefined && typeof control.selected !== 'boolean')) throw failure('invalid_controls');
  }
  const sdk = resolve(options.sdkRoot), tools = resolve(options.toolsRoot);
  const adb = join(sdk, 'platform-tools', 'adb'), emulatorPath = join(sdk, 'emulator', 'emulator');
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
  const shell = (script: string) => ['shell', 'sh', '-c', `'${script}'`];
  const installedBuild = async (c: TargetContext) => {
    if (await adbRun(['get-state'], c) !== 'device' || await adbRun(['shell', 'getprop', 'ro.kernel.qemu'], c) === '1') throw failure('not_physical_device');
    const path = await adbRun(['shell', 'pm', 'path', options.appPackage], c);
    // Split builds require a digest of the entire installation; fail closed.
    if (!/^package:(\/data\/app\/[A-Za-z0-9_./=+~\-]+\/base\.apk)$/.test(path)) throw failure('unsupported_installed_build');
    const digest = await adbRun(['shell', 'sha256sum', path.slice(8)], c);
    if (digest.split(/\s+/)[0] !== options.apkSHA256) throw failure('apk_digest_mismatch');
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
    tap: async c => { await endpoint(`/element/${await resolveOne(id, c)}/click`, c, 'POST', {}); },
    fill: async (value, c) => {
      const ref = await resolveOne(id, c);
      await endpoint(`/element/${ref}/clear`, c, 'POST', {});
      await endpoint(`/element/${ref}/value`, c, 'POST', {text: value});
    },
    visible: async c => {
      const value = await endpoint(`/element/${await resolveOne(id, c)}/displayed`, c);
      if (typeof value !== 'boolean') throw failure('invalid_property'); return value;
    },
    text: async c => {
      const value = await endpoint(`/element/${await resolveOne(id, c)}/text`, c);
      if (typeof value !== 'string' || Buffer.byteLength(value) > 4096) throw failure('invalid_property'); return value;
    },
    state: async (name, c) => {
      const value = await endpoint(`/element/${await resolveOne(id, c)}/attribute/${name}`, c);
      if (value !== 'true' && value !== 'false' && typeof value !== 'boolean') throw failure('unavailable_state');
      return value === true || value === 'true';
    },
  });
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
          if (!physical) {
            const apk = resolve(options.apk!);
            const info = await stat(apk);
            if (!info.isFile() || info.size > 256 * 1024 * 1024) throw failure('invalid_apk');
            apkBytes = await readFile(apk);
            if (createHash('sha256').update(apkBytes).digest('hex') !== options.apkSHA256) throw failure('apk_digest_mismatch');
          }
          const appiumPkg = JSON.parse(await readFile(join(tools, 'node_modules/appium/package.json'), 'utf8'));
          const driverPkg = JSON.parse(await readFile(join(tools, 'node_modules/appium-uiautomator2-driver/package.json'), 'utf8'));
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
            const badging = await command(join(sdk, 'build-tools/35.0.0/aapt2'), ['dump', 'badging', installedAPK], c, env);
            if (/^package: name='([^']+)'/.exec(badging)?.[1] !== options.appPackage) throw failure('apk_package_mismatch');
          }
          await writeFile(join(directory, 'package.json'), JSON.stringify({private: true, dependencies: {appium: androidVersions.appium, 'appium-uiautomator2-driver': androidVersions.uiautomator2}}));
          // The emulator atomically reserves its own available console port.
          // Its private handshake binds the reported adb serial to this child.
          check(c);
          if (physical) {
            stage = 'device_attach';
            serial = options.deviceSerial!;
            const leaseRoot = join(homedir(), '.9lives', 'android-leases');
            const lock = join(leaseRoot, `9l-android-${createHash('sha256').update(serial).digest('hex')}.lock`);
            // Never recover a stale lease automatically: a killed worker may have
            // left automation running. Manual inspection is required in that case.
            await mkdir(leaseRoot, {recursive: true, mode: 0o700});
            await mkdir(lock, {mode: 0o700}); lease = lock;
            await installedBuild(c);
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
            stage = 'apk_install';
            await adbRun(['install', '--no-streaming', '-r', installedAPK], c);
          }
          const appiumPort = await freePort();
          base = `http://127.0.0.1:${appiumPort}/${owner}`;
          check(c);
          stage = 'server_start';
          server = spawn(process.execPath, [join(tools, 'node_modules/appium/index.js'), '--address', '127.0.0.1', '--port', String(appiumPort), '--base-path', `/${owner}`, '--use-drivers', 'uiautomator2', '--log-level', 'error', '--log-no-colors'], {env, cwd: directory, stdio: 'ignore'});
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
            identity = {device: 'physical', deviceHash: createHash('sha256').update(serial).digest('hex')};
          } else {
            const emulatorVersion = await command(emulatorPath, ['-version'], c, env);
            const version = /Android emulator version ([0-9.]+)/.exec(emulatorVersion)?.[1];
            if (!version) throw failure('unknown_emulator_version');
            identity = {emulator: version};
          }
          evidence = {
            appPackage: options.appPackage, apkSHA256: options.apkSHA256,
            api: await adbRun(['shell', 'getprop', 'ro.build.version.sdk'], c),
            release: await adbRun(['shell', 'getprop', 'ro.build.version.release'], c),
            abi: await adbRun(['shell', 'getprop', 'ro.product.cpu.abi'], c),
            ...identity, ...androidVersions, worker: createHash('sha256').update(`${owner}:${process.pid}`).digest('hex'),
          };
        } catch { closed = true; await cleanup(); throw failure(`open_failed_${stage}`); }
      })();
      return opening;
    },
    async reset(c) {
      if (physical) throw failure('reset_unsupported_on_attached_device');
      check(c);
      if (!session) throw failure('session_unavailable');
      await adbRun(['shell', 'am', 'force-stop', options.appPackage], c);
      if (await adbRun(['shell', 'pm', 'clear', options.appPackage], c) !== 'Success') throw failure('reset_failed');
      await adbRun(['shell', 'am', 'start', '-W', '-n', `${options.appPackage}/${options.activity}`], c);
    },
    async observeTestId(id, c) { return (await matches(id, c)).slice(0, 2).map(() => handle(id)); },
    close,
  };
}
