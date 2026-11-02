const assert = require('node:assert/strict');
const test = require('node:test');
const fs = require('node:fs');
const net = require('node:net');
const path = require('node:path');
const os = require('node:os');
const { androidChecks, adbDevices, javaMajor } = require('../dist/android.js');
const { diagnose } = require('../dist/doctor.js');

const posix = process.platform !== 'win32';
const pinned = { appium: '3.8.0', 'appium-uiautomator2-driver': '8.7.0' };

function script(file, body) {
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, `#!/bin/sh\n${body}\n`, { mode: 0o755 });
}

// A fake SDK, JDK and tooling directory. The tools print what the real ones
// print, plus a path and a serial-like value that must never be reported.
function fixture({ java = '21.0.5', avds = 'INFO    | Storing crashdata in: /private/fixture-path\nFixture_API_35\nsecond.avd', tools = pinned, build = ['aapt2', 'apksigner'], emulator = true } = {}) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), '9l-doctor-android-'));
  const sdk = path.join(root, 'sdk'), jdk = path.join(root, 'jdk'), tooling = path.join(root, 'tools');
  script(path.join(sdk, 'platform-tools/adb'), 'echo "Android Debug Bridge version 1.0.41"; echo "Version 36.0.0-fixture"; echo "Installed as /private/fixture-path/adb"');
  if (emulator) script(path.join(sdk, 'emulator/emulator'), `[ "$1" = "-list-avds" ] && [ "$ANDROID_HOME" = "${sdk}" ] && printf '${avds.replace(/\n/g, '\\n')}\\n'`);
  for (const name of build) script(path.join(sdk, 'build-tools/35.0.0', name), 'exit 0');
  if (java) script(path.join(jdk, 'bin/java'), `echo 'openjdk version "${java}" 2024-10-15' >&2`);
  for (const [name, version] of Object.entries(tools)) {
    fs.mkdirSync(path.join(tooling, 'node_modules', name), { recursive: true });
    fs.writeFileSync(path.join(tooling, 'node_modules', name, 'package.json'), JSON.stringify({ name, version }));
  }
  if (tools.appium) fs.writeFileSync(path.join(tooling, 'node_modules/appium/index.js'), '');
  return { root, sdk, jdk, tooling, env: { PATH: '/usr/bin:/bin', HOME: path.join(root, 'home'), ANDROID_HOME: sdk, JAVA_HOME: jdk, NINELIVES_ANDROID_TOOLS: tooling, ANDROID_ADB_SERVER_PORT: '5037' } };
}

const none = () => false;
const byName = report => Object.fromEntries(report.checks.map(check => [check.name, check]));
const attached = { running: true, devices: [{ emulator: true, state: 'device' }, { emulator: false, state: 'device' }, { emulator: false, state: 'device' }] };

test('a complete fake SDK passes every Android check and names neither paths nor serials', { skip: !posix }, async () => {
  const f = fixture();
  try {
    const report = await androidChecks({ env: f.env, platform: 'darwin', isDirectory: none, devices: async () => attached });
    const checks = byName(report);
    for (const name of ['android/java', 'android/sdk', 'android/adb', 'android/emulator', 'android/build-tools', 'android/avds', 'android/devices', 'android/appium', 'android/uiautomator2']) {
      assert.equal(checks[name]?.status, 'ok', `${name}: ${checks[name]?.message}`);
    }
    assert.match(checks['android/java'].message, /^Java 21\.0\.5 from JAVA_HOME; 9l run passes it to tests only with --pass-env JAVA_HOME\.$/);
    assert.match(checks['android/sdk'].message, /^SDK root from ANDROID_HOME; 9l run passes ANDROID_HOME to tests only with --pass-env ANDROID_HOME, otherwise a driver without sdkRoot finds no SDK\.$/);
    assert.equal(checks['android/adb'].message, 'adb 36.0.0-fixture.');
    assert.equal(checks['android/avds'].message, '2 AVDs: Fixture_API_35, second.avd. The driver boots the one named by its avd option.');
    assert.equal(checks['android/devices'].message, '1 emulator and 2 physical devices attached and ready. Serials are not shown.');
    assert.match(checks['android/appium'].message, /^Appium 3\.8\.0 \(pinned\) in NINELIVES_ANDROID_TOOLS; 9l run passes it/);
    assert.doesNotMatch(JSON.stringify(report), new RegExp(`${f.root}|fixture-path|Installed as`));
    const located = byName(await androidChecks({ env: f.env, platform: 'darwin', paths: true, devices: async () => attached }));
    assert(located['android/sdk'].message.includes(f.sdk) && located['android/appium'].message.includes(f.tooling), '--paths names the roots');
  } finally { fs.rmSync(f.root, { recursive: true, force: true }); }
});

test('missing prerequisites are missing with a concrete fix; optional emulator pieces only warn', { skip: !posix }, async () => {
  const f = fixture({ java: '11.0.2', build: ['aapt2'], emulator: false, tools: { appium: '3.9.0' } });
  try {
    const checks = byName(await androidChecks({ env: f.env, platform: 'darwin', devices: async () => ({ running: false }) }));
    assert.equal(checks['android/java'].status, 'missing');
    assert.match(checks['android/java'].fix, /^brew install openjdk@21 && export JAVA_HOME=/);
    assert.deepEqual([checks['android/build-tools'].status, checks['android/build-tools'].fix], ['missing', 'sdkmanager "build-tools;35.0.0"']);
    assert.match(checks['android/build-tools'].message, /lacks apksigner/);
    assert.deepEqual([checks['android/emulator'].status, checks['android/emulator'].fix], ['warning', 'sdkmanager "emulator"']);
    assert.equal(checks['android/avds'], undefined, 'AVDs are listed only through an emulator');
    assert.equal(checks['android/devices'].status, 'ok');
    assert.match(checks['android/devices'].message, /not running.*doctor does not start it/);
    assert.equal(checks['android/appium'].status, 'missing');
    assert.match(checks['android/appium'].message, /is 3\.9\.0, not the pinned 3\.8\.0/);
    const install = 'npm install --prefix "$NINELIVES_ANDROID_TOOLS" --save-exact --ignore-scripts appium@3.8.0 appium-uiautomator2-driver@8.7.0';
    assert.equal(checks['android/appium'].fix, install);
    assert.match(checks['android/uiautomator2'].message, /is not installed/);
    fs.rmSync(path.join(f.jdk, 'bin/java'));
    assert.match(byName(await androidChecks({ env: f.env, platform: 'linux', devices: async () => ({ running: false }) }))['android/java'].message, /JAVA_HOME is set but has no bin\/java\.$/);
    fs.rmSync(path.join(f.sdk, 'platform-tools'), { recursive: true });
    assert.deepEqual(byName(await androidChecks({ env: f.env, devices: async () => ({ running: false }) }))['android/adb'].fix, 'sdkmanager "platform-tools"');
  } finally { fs.rmSync(f.root, { recursive: true, force: true }); }
});

test('no SDK, no AVD, unauthorized devices and the default tooling directory', { skip: !posix }, async () => {
  const f = fixture({ avds: 'INFO    | no avds here' });
  try {
    const noAvd = byName(await androidChecks({ env: f.env, arch: 'x64', devices: async () => ({ running: true, devices: [{ emulator: false, state: 'unauthorized' }, { emulator: true, state: 'offline' }] }) }));
    assert.equal(noAvd['android/avds'].status, 'warning');
    assert.match(noAvd['android/avds'].fix, /avdmanager create avd -n 9l_api35 -k "system-images;android-35;google_apis;x86_64"/);
    assert.equal(noAvd['android/devices'].status, 'warning');
    assert.equal(noAvd['android/devices'].message, '0 emulators and 0 physical devices attached and ready; 1 unauthorized; 1 offline. Serials are not shown.');
    const { ANDROID_HOME, NINELIVES_ANDROID_TOOLS, ...bare } = f.env;
    const missing = await androidChecks({ env: { ...bare, ANDROID_HOME: path.join(f.root, 'absent') }, devices: async () => ({ running: false }) });
    const checks = byName(missing);
    assert.equal(checks['android/sdk'].status, 'missing');
    assert.match(checks['android/sdk'].message, /ANDROID_HOME is set to a directory that does not exist\.$/);
    assert.equal(checks['android/adb'], undefined, 'no tool checks without an SDK root');
    assert.match(checks['android/appium'].message, /not installed in ~\/\.9l\/android-tools\.$/);
    assert.match(checks['android/appium'].fix, /^npm install --prefix ~\/\.9l\/android-tools /);
    assert.equal(diagnose({ android: true }, f.root, missing).ready, false, 'a missing Android prerequisite makes the report not ready');
    const sdkRoot = byName(await androidChecks({ env: { ...f.env, ANDROID_SDK_ROOT: path.join(f.root, 'other') }, devices: async () => ({ running: false }) }));
    assert.equal(sdkRoot['android/sdk'].status, 'warning');
    assert.equal(byName(await androidChecks({ env: { ...f.env, ANDROID_ADB_SERVER_PORT: 'x' }, devices: async () => assert.fail('no query') }))['android/devices'].status, 'warning');
  } finally { fs.rmSync(f.root, { recursive: true, force: true }); }
});

// A test worker gets HOME but no SDK variable unless it is forwarded, so
// doctor says which variable a driver without sdkRoot would need.
test('android/sdk says whether tests without sdkRoot find the same root, and which variable to forward', { skip: !posix }, async () => {
  const f = fixture();
  try {
    const sdk = async (env, isDirectory) => byName(await androidChecks({ env, platform: 'linux', isDirectory, devices: async () => ({ running: false }) }))['android/sdk'];
    const { ANDROID_HOME, ...bare } = f.env;
    const brew = path.join(f.root, 'brew'), brewSdk = path.join(brew, 'share/android-commandlinetools');
    fs.mkdirSync(brewSdk, { recursive: true });
    const custom = await sdk({ ...bare, HOMEBREW_PREFIX: brew }, dir => dir === brewSdk);
    assert.equal(custom.status, 'ok');
    assert.equal(custom.message, 'SDK root from the Homebrew command-line tools location; 9l run passes HOMEBREW_PREFIX to tests only with --pass-env HOMEBREW_PREFIX, otherwise a driver without sdkRoot finds no SDK.');
    const fixed = await sdk(bare, dir => dir === '/opt/homebrew/share/android-commandlinetools');
    assert.match(fixed.message, /^SDK root from the Homebrew command-line tools location; tests find the same root when sdkRoot is omitted\.$/);
    const studio = path.join(bare.HOME, 'Android/Sdk');
    fs.mkdirSync(studio, { recursive: true });
    const same = await sdk({ ...bare, ANDROID_HOME: studio }, dir => dir === studio);
    assert.equal(same.message, 'SDK root from ANDROID_HOME; tests find the same root when sdkRoot is omitted.');
    const other = await sdk({ ...bare, ANDROID_SDK_ROOT: f.sdk }, dir => dir === studio);
    assert.equal(other.message, 'SDK root from ANDROID_SDK_ROOT; 9l run passes ANDROID_SDK_ROOT to tests only with --pass-env ANDROID_SDK_ROOT, otherwise a driver without sdkRoot finds another SDK.');
  } finally { fs.rmSync(f.root, { recursive: true, force: true }); }
});

test('Node 22 before 22.12 cannot run the pinned Appium', async () => {
  const checks = byName(await androidChecks({ env: {}, nodeVersion: '22.11.0', run: () => ({ status: null, output: '', absent: true }), devices: async () => ({ running: false }) }));
  assert.equal(checks['android/node'].status, 'missing');
  assert.equal(byName(await androidChecks({ env: {}, nodeVersion: '22.12.0', run: () => ({ status: null, output: '', absent: true }), devices: async () => ({ running: false }) }))['android/node'], undefined);
  assert.match(checks['android/java'].message, /^No java on PATH and JAVA_HOME is unset/);
});

test('java -version output is read to a major release', () => {
  assert.deepEqual(javaMajor('openjdk version "21.0.13.0.3" 2026-10-24'), { version: '21.0.13.0.3', major: 21 });
  assert.deepEqual(javaMajor('java version "1.8.0_402"'), { version: '1.8.0_402', major: 8 });
  assert.deepEqual(javaMajor('openjdk version "17" 2021-09-14'), { version: '17', major: 17 });
  assert.equal(javaMajor('The operation could not be completed. Unable to locate a Java Runtime.'), undefined);
});

test('the adb device list is read from a running server by counts only, and a closed port is not running', async () => {
  const requests = [];
  const server = net.createServer(socket => socket.once('data', data => {
    requests.push(data.toString());
    const body = 'emulator-5554\tdevice\nAAAA-private-serial\tdevice\nBBBB-private-serial\tunauthorized\n';
    socket.end(`OKAY${body.length.toString(16).padStart(4, '0')}${body}`);
  }));
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const { port } = server.address();
  try {
    const list = await adbDevices(port);
    assert.deepEqual(requests, ['000chost:devices']);
    assert.deepEqual(list, { running: true, devices: [{ emulator: true, state: 'device' }, { emulator: false, state: 'device' }, { emulator: false, state: 'unauthorized' }] });
    assert.doesNotMatch(JSON.stringify(list), /private-serial|emulator-5554/);
  } finally { await new Promise(resolve => server.close(resolve)); }
  assert.deepEqual(await adbDevices(port), { running: false });
  const silent = net.createServer(() => {});
  await new Promise(resolve => silent.listen(0, '127.0.0.1', resolve));
  try { assert.deepEqual(await adbDevices(silent.address().port, 100), { running: true }); }
  finally { silent.close(); }
});

test('doctor reuses the SDK driver discovery byte for byte', () => {
  const read = file => fs.readFileSync(path.join(__dirname, '../..', file), 'utf8');
  assert.equal(read('browser-checks/src/android-env.ts'), read('playwright/src/android-env.ts'),
    'copy packages/playwright/src/android-env.ts to packages/browser-checks/src/android-env.ts');
});

test('a spec importing the Android entry point points to --android', () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), '9l-doctor-android-spec-'));
  try {
    const spec = path.join(root, 'native.spec.ts');
    fs.writeFileSync(spec, "import { test } from '@9l/playwright';\nimport { androidTargetDriver } from '@9l/playwright/android';\ntest('x', async ({ page }) => {});\n");
    const hint = diagnose({ sdk: true, spec }, root).checks.find(check => check.name === 'spec/android');
    assert.deepEqual([hint.status, hint.fix], ['warning', '9l doctor --android']);
    assert.equal(diagnose({ sdk: true, spec, android: true }, root, { checks: [], unverified: [] }).checks.find(check => check.name === 'spec/android'), undefined);
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});
