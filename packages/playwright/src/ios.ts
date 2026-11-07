import {execFile, spawn, type ChildProcess} from 'node:child_process';
import {createHash, randomUUID} from 'node:crypto';
import {lstatSync, readdirSync, readFileSync} from 'node:fs';
import {mkdir, mkdtemp, readFile, writeFile, symlink, copyFile, chmod, rm} from 'node:fs/promises';
import {join, resolve, isAbsolute} from 'node:path';
import {homedir} from 'node:os';
import {createServer} from 'node:net';
import {setTimeout as delay} from 'node:timers/promises';
import {targetVersion} from './protocol';
import {TargetSetupError, targetCapabilities, controlPattern, type TargetContext, type TargetDriver, type TargetHandle} from './targets';
import {iosVersions, validIOSRuntime, type IOSRuntime} from './ios-runtime';
export {iosVersions, validIOSRuntime, iosRuntimeType, type IOSRuntime} from './ios-runtime';

export type IOSTargetOptions = {
  appId: string;
  /** Unpacked Simulator .app, never an IPA or a device build. Copied into
   * Go's private scratch and verified before provisioning. Symlinks refused. */
  app: string; appSHA256: string; bundleId: string;
  /** Exact installed CoreSimulator identifiers. No automatic latest runtime,
   * existing device attachment, downloads, permission grants or recovery. */
  runtime: string; deviceType: string;
  /** Directory containing the pinned packages; defaults to ~/.9l/ios-tools. */
  toolsRoot?: string;
};
const fail = (reason: string) => new Error(`9lives iOS: ${reason}`);
const uuid = /^[A-Fa-f0-9]{8}(-[A-Fa-f0-9]{4}){3}-[A-Fa-f0-9]{12}$/;
const elementKey = 'element-6066-11e4-a52e-4f735466cecf';
const milliseconds = (c: TargetContext) => {
  c.signal.throwIfAborted();
  const remaining = c.deadlineMs - Date.now();
  if (remaining <= 0) throw fail('deadline');
  return Math.min(remaining, 2 ** 31 - 1);
};

// A canonical bounded tree hash, including relative names and executable modes.
// Installed and source trees use the same algorithm; no app bytes enter evidence.
function appFiles(directory: string, check = () => {}) {
  const files: string[] = []; let bytes = 0, entries = 0;
  const walk = (relative: string) => {
    check();
    const full = join(directory, relative), stat = lstatSync(full);
    if (++entries > 4096 || stat.isSymbolicLink()) throw fail('unsupported_app_tree');
    if (stat.isDirectory()) {
      const names = readdirSync(full).sort();
      if (names.length > 4096) throw fail('unsupported_app_tree');
      for (const name of names) walk(join(relative, name));
    } else if (stat.isFile()) {
      bytes += stat.size;
      if (bytes > 256 * 1024 * 1024 || relative.length > 512) throw fail('app_limit');
      files.push(relative);
    } else throw fail('unsupported_app_tree');
  };
  if (!lstatSync(directory).isDirectory()) throw fail('invalid_app');
  walk('');
  if (!files.includes('Info.plist')) throw fail('invalid_app');
  return files;
}
export function iosAppDigest(directory: string): string {
  const hash = createHash('sha256');
  for (const file of appFiles(directory)) {
    const full = join(directory, file), info = lstatSync(full), bytes = readFileSync(full);
    if (bytes.length !== info.size) throw fail('app_changed');
    hash.update(JSON.stringify([file, info.mode & 0o111, bytes.length]) + '\n').update(bytes);
  }
  return hash.digest('hex');
}
function command(file: string, args: string[], c: TargetContext, env: NodeJS.ProcessEnv): Promise<string> {
  return new Promise((yes, no) => execFile(file, args, {signal: c.signal, timeout: milliseconds(c), maxBuffer: 256 * 1024, env},
    (error, stdout) => error ? no(fail('device_command_failed')) : yes(stdout.trim())));
}
async function freePort() {
  const server = createServer();
  await new Promise<void>((yes, no) => { server.once('error', no); server.listen(0, '127.0.0.1', yes); });
  const port = (server.address() as {port: number}).port;
  await new Promise<void>((yes, no) => server.close(error => error ? no(error) : yes()));
  return port;
}

/** Experimental: one new Simulator and local Appium/XCUITest server per test.
 * Shared TargetApp owns assertion polling. This driver dispatches mutations
 * once, re-observes identifiers, and never substitutes labels or coordinates. */
export function iosTargetDriver(options: IOSTargetOptions): TargetDriver {
  if (!/^[A-Za-z0-9_-]{1,64}$/.test(options.appId) || !/^[a-f0-9]{64}$/.test(options.appSHA256) ||
      !/^[A-Za-z][A-Za-z0-9-]*(\.[A-Za-z0-9-]+)+$/.test(options.bundleId) || options.bundleId.length > 200 ||
      !/^com\.apple\.CoreSimulator\.SimRuntime\.iOS-[0-9]{1,3}(-[0-9]{1,5}){1,2}$/.test(options.runtime) ||
      !/^com\.apple\.CoreSimulator\.SimDeviceType\.[A-Za-z0-9-]{1,80}$/.test(options.deviceType)) throw fail('invalid_options');
  const owner = `9l-ios-${randomUUID()}`, controller = new AbortController();
  const tools = resolve(options.toolsRoot ?? join(homedir(), '.9l/ios-tools'));
  const env: NodeJS.ProcessEnv = {};
  for (const key of ['PATH', 'HOME', 'USER', 'LOGNAME', 'LANG', 'LC_ALL', 'DEVELOPER_DIR']) if (process.env[key]) env[key] = process.env[key];
  let directory = '', app = '', udid = '', session = '', base = '';
  let server: ChildProcess | undefined, serverFailed = false;
  let opening: Promise<void> | undefined, closing: Promise<void> | undefined, closed = false;
  let evidence: IOSRuntime | undefined, creationStarted = false;
  const check = (c: TargetContext) => {
    milliseconds(c);
    if (closed || serverFailed || (server && (server.exitCode !== null || server.signalCode !== null))) throw fail('runtime_unavailable');
  };
  const simctl = (args: string[], c: TargetContext) => command('/usr/bin/xcrun', ['simctl', ...args], c, env);
  const request = async (path: string, c: TargetContext, method = 'GET', body?: unknown): Promise<any> => {
    check(c);
    const signal = AbortSignal.any([c.signal, controller.signal, AbortSignal.timeout(milliseconds(c))]);
    try {
      const response = await fetch(base + path, {method, signal, redirect: 'error', headers: {'content-type': 'application/json'},
        ...(body === undefined ? {} : {body: JSON.stringify(body)})});
      const reader = response.body?.getReader();
      if (!reader) throw fail('invalid_response');
      const chunks: Uint8Array[] = []; let size = 0;
      for (;;) {
        const {done, value} = await reader.read();
        if (done) break;
        size += value.length;
        if (size > 64 * 1024) { await reader.cancel(); throw fail('response_limit'); }
        chunks.push(value);
      }
      const data = JSON.parse(Buffer.concat(chunks).toString('utf8'));
      if (!response.ok || !data || !Object.hasOwn(data, 'value') || data.value?.error) throw fail('driver_command_failed');
      check(c);
      return data.value;
    } catch { throw fail('transport_or_driver_failed'); }
  };
  const endpoint = (path: string, c: TargetContext, method = 'GET', body?: unknown) => {
    if (!session) throw fail('session_unavailable');
    return request(`/session/${session}${path}`, c, method, body);
  };
  const appCommand = (name: 'terminateApp' | 'activateApp', c: TargetContext) =>
    endpoint('/execute/sync', c, 'POST', {script: `mobile: ${name}`, args: [{bundleId: options.bundleId}]});
  const install = async (c: TargetContext) => {
    // Reverify the frozen copy before every install and the actual installed
    // tree afterwards. A changed build never reaches a target assertion.
    check(c);
    if (iosAppDigest(app) !== options.appSHA256) throw new TargetSetupError({classification: 'mismatch', reason: 'build-digest', stage: 'open'});
    await simctl(['install', udid, app], c);
    const installed = await simctl(['get_app_container', udid, options.bundleId, 'app'], c);
    if (!isAbsolute(installed) || iosAppDigest(installed) !== options.appSHA256) throw fail('installed_build_mismatch');
    check(c);
  };
  const find = async (id: string, c: TargetContext): Promise<string[]> => {
    if (!controlPattern.test(id)) throw fail('invalid_control');
    // WDA exposes name = identifier || label, but no raw identifier attribute.
    // Excluding label equality prevents fallback. Identifiers equal to their
    // labels are deliberately unsupported, even if an identifier is present.
    const values = await endpoint('/elements', c, 'POST', {using: '-ios predicate string', value: `name == "${id}" AND label != "${id}"`});
    if (!Array.isArray(values) || values.some(v => !v || typeof v[elementKey] !== 'string' || !/^[A-Za-z0-9-]{1,128}$/.test(v[elementKey]))) throw fail('invalid_elements');
    const ids = values.map(v => v[elementKey] as string);
    if (new Set(ids).size !== ids.length) throw fail('invalid_elements');
    return ids.slice(0, 2);
  };
  const element = async (id: string, c: TargetContext) => {
    const ids = await find(id, c);
    if (ids.length !== 1) throw fail(ids.length ? 'ambiguous_control' : 'missing_control');
    return `/element/${ids[0]}`;
  };
  const bool = (value: unknown) => {
    if (value === true || value === 'true') return true;
    if (value === false || value === 'false') return false;
    throw fail('invalid_property');
  };
  const cleanup = async () => {
    // Closing the transport alone does not stop a native action. Stop our
    // server's process group, then shut down and delete only our simulator.
    let failed = false;
    if (server?.pid) {
      for (const signal of ['SIGTERM', 'SIGKILL'] as const) {
        try { process.kill(-server.pid, signal); } catch (e) { if ((e as NodeJS.ErrnoException).code !== 'ESRCH') failed = true; }
        if (signal === 'SIGTERM') await delay(250);
      }
    }
    const c = {signal: AbortSignal.timeout(20_000), deadlineMs: Date.now() + 20_000};
    if (creationStarted) {
      try {
        // A canceled simctl create can lose its stdout. Recover ownership by
        // the unguessable name and exact runtime/type, never by "booted".
        const devices = JSON.parse(await simctl(['list', 'devices', '-j'], c)).devices;
        if (!devices || typeof devices !== 'object') throw fail('invalid_devices');
        const named = Object.entries(devices).flatMap(([runtime, list]) =>
          Array.isArray(list) ? list.filter(d => d.name === owner).map(d => ({...d, runtime})) : []);
        if (named.some(d => d.runtime !== options.runtime || d.deviceTypeIdentifier !== options.deviceType)) throw fail('ownership_mismatch');
        const owned = named;
        if (owned.length > 1 || (udid && owned.some(d => d.udid !== udid))) throw fail('ownership_mismatch');
        for (const device of owned) {
          if (typeof device.udid !== 'string' || !uuid.test(device.udid)) throw fail('ownership_mismatch');
          if (device.state !== 'Shutdown') await simctl(['shutdown', device.udid], c);
          await simctl(['delete', device.udid], c);
        }
        const after = JSON.parse(await simctl(['list', 'devices', '-j'], c)).devices;
        if (!after || Object.values(after).some(list => Array.isArray(list) && list.some(d => d.name === owner || (udid && d.udid === udid)))) throw fail('cleanup_failed');
      } catch { failed = true; }
    }
    if (directory) { try { await rm(directory, {recursive: true, force: true}); } catch { failed = true; } }
    if (failed) throw fail('cleanup_failed');
  };
  const driver: TargetDriver = {
    kind: 'ios', appId: options.appId, version: targetVersion, capabilities: targetCapabilities,
    open(context) {
      if (opening || closed) return Promise.reject(fail('already_open_or_closed'));
      const c = {...context, signal: AbortSignal.any([context.signal, controller.signal])};
      opening = (async () => {
        try {
          check(c);
          if (process.platform !== 'darwin') throw fail('requires_macos');
          const scratch = process.env.NINELIVES_TARGET_SCRATCH_DIR;
          if (!scratch || !isAbsolute(scratch)) throw fail('missing_owned_scratch');
          for (const [name, version] of [['appium', iosVersions.appium], ['appium-xcuitest-driver', iosVersions.xcuitest]]) {
            if (JSON.parse(await readFile(join(tools, 'node_modules', name, 'package.json'), 'utf8')).version !== version) throw fail('tool_version_mismatch');
          }
          directory = await mkdtemp(join(scratch, 'ios-'));
          env.APPIUM_HOME = directory; env.TMPDIR = directory;
          await mkdir(join(directory, 'node_modules'));
          for (const name of ['appium', 'appium-xcuitest-driver']) await symlink(join(tools, 'node_modules', name), join(directory, 'node_modules', name), 'dir');
          await writeFile(join(directory, 'package.json'), JSON.stringify({private: true, dependencies: {appium: iosVersions.appium, 'appium-xcuitest-driver': iosVersions.xcuitest}}));
          app = join(directory, 'App.app'); await mkdir(app);
          const source = resolve(options.app);
          for (const file of appFiles(source, () => check(c))) {
            const dest = join(app, file); await mkdir(join(dest, '..'), {recursive: true});
            await copyFile(join(source, file), dest); await chmod(dest, lstatSync(join(source, file)).mode & 0o777);
          }
          if (iosAppDigest(app) !== options.appSHA256) throw new TargetSetupError({classification: 'mismatch', reason: 'build-digest', stage: 'open'});
          const plist = (key: string) => command('/usr/libexec/PlistBuddy', ['-c', `Print :${key}`, join(app, 'Info.plist')], c, env);
          if (await plist('CFBundleIdentifier') !== options.bundleId || await plist('DTPlatformName') !== 'iphonesimulator') throw fail('app_platform_or_bundle_mismatch');
          const version = await plist('CFBundleShortVersionString'), build = await plist('CFBundleVersion');
          const xcode = /^Xcode ([0-9.]+)\n/.exec(await command('/usr/bin/xcodebuild', ['-version'], c, env))?.[1] ?? '';
          const runtimes = JSON.parse(await simctl(['list', 'runtimes', '-j'], c)).runtimes;
          const runtime = Array.isArray(runtimes) ? runtimes.filter(r => r.identifier === options.runtime && r.isAvailable === true) : [];
          if (runtime.length !== 1 || typeof runtime[0].version !== 'string') throw fail('runtime_unavailable');
          creationStarted = true;
          udid = await simctl(['create', owner, options.deviceType, options.runtime], c);
          if (!uuid.test(udid)) throw fail('invalid_device');
          await simctl(['boot', udid], c);
          await simctl(['bootstatus', udid, '-b'], c);
          const architecture = await simctl(['spawn', udid, 'uname', '-m'], c);
          await install(c);
          const appiumPort = await freePort(), wdaPort = await freePort(), mjpegPort = await freePort();
          check(c);
          base = `http://127.0.0.1:${appiumPort}/${owner}`;
          server = spawn(process.execPath, [join(tools, 'node_modules/appium/index.js'), '--address', '127.0.0.1', '--port', String(appiumPort),
            '--base-path', `/${owner}`, '--use-drivers', 'xcuitest', '--log-level', 'error', '--log-no-colors'], {env, cwd: directory, stdio: 'ignore', detached: true});
          server.once('error', () => { serverFailed = true; });
          for (;;) {
            check(c);
            try { if ((await request('/status', c))?.ready === true) break; } catch { /* readiness reads only */ }
            await delay(100, undefined, {signal: c.signal});
          }
          const value = await request('/session', c, 'POST', {capabilities: {alwaysMatch: {
            platformName: 'iOS', 'appium:automationName': 'XCUITest', 'appium:udid': udid, 'appium:bundleId': options.bundleId,
            'appium:noReset': true, 'appium:fullReset': false, 'appium:skipLogCapture': true, 'appium:showXcodeLog': false,
            'appium:printPageSourceOnFindFailure': false, 'appium:derivedDataPath': join(directory, 'wda'),
            'appium:wdaLocalPort': wdaPort, 'appium:mjpegServerPort': mjpegPort, 'appium:useNewWDA': true,
            'appium:wdaLaunchTimeout': Math.min(90_000, milliseconds(c)), 'appium:wdaStartupRetries': 1,
            'appium:settings[screenshotQuality]': 2, 'appium:disableAutomaticScreenshots': true,
          }, firstMatch: [{}]}});
          if (!value || typeof value.sessionId !== 'string' || !/^[A-Za-z0-9-]{1,128}$/.test(value.sessionId) ||
              value.capabilities?.udid !== udid || String(value.capabilities?.platformName).toLowerCase() !== 'ios') throw fail('foreign_session');
          session = value.sessionId;
          await endpoint('/timeouts', c, 'POST', {implicit: 0});
          const viewport = await endpoint('/window/size', c);
          evidence = {bundleId: options.bundleId, appSHA256: options.appSHA256, version, build, runtime: options.runtime, deviceType: options.deviceType,
            os: runtime[0].version, architecture, xcode, appium: iosVersions.appium, xcuitest: iosVersions.xcuitest,
            worker: createHash('sha256').update(owner).digest('hex'), viewportWidth: String(viewport?.width), viewportHeight: String(viewport?.height)};
          if (!validIOSRuntime(evidence)) throw fail('invalid_runtime_metadata');
          check(c);
        } catch (error) {
          if (error instanceof TargetSetupError) throw error;
          throw new TargetSetupError({classification: 'unknown', reason: 'device-state-unknown', stage: 'open'});
        }
      })();
      return opening;
    },
    runtime: () => evidence,
    async reset(c) {
      await appCommand('terminateApp', c);
      await simctl(['uninstall', udid, options.bundleId], c);
      await install(c);
      await appCommand('activateApp', c);
    },
    async observeTestId(id, c) {
      const matches = await find(id, c);
      return matches.map((): TargetHandle => ({
        // Re-query before each operation: a new duplicate cannot inherit an old handle.
        async tap(c) { await endpoint(`${await element(id, c)}/click`, c, 'POST', {}); },
        async fill(value, c) {
          if (typeof value !== 'string' || Buffer.byteLength(value) > 4096) throw fail('invalid_value');
          await endpoint(`${await element(id, c)}/clear`, c, 'POST', {});
          await endpoint(`${await element(id, c)}/value`, c, 'POST', {text: value});
        },
        async visible(c) { return bool(await endpoint(`${await element(id, c)}/displayed`, c)); },
        async text(c) {
          const text = await endpoint(`${await element(id, c)}/text`, c);
          if (typeof text !== 'string' || Buffer.byteLength(text) > 4096) throw fail('invalid_property');
          return text;
        },
        async state(name, c) {
          if (name !== 'enabled') throw fail('unsupported_state');
          return bool(await endpoint(`${await element(id, c)}/enabled`, c));
        },
      }));
    },
    async close() {
      if (!closing) {
        closed = true; controller.abort();
        closing = (async () => { await opening?.catch(() => {}); await cleanup(); })();
      }
      return closing;
    },
  };
  return driver;
}
