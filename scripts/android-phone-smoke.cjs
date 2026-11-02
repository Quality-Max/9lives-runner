// Opt-in physical-phone qualification of attached-device mode. Requires one
// authorized phone selected with NINELIVES_ANDROID_SERIAL. It installs the
// synthetic fixture app with installInPlace, then runs the native spec in
// attached mode. The serial stays in the environment: receipts hold only its
// hash, and nothing here prints it. Screen-off runs last; on a phone with a
// lock screen it also yields the locked classification, after which the
// phone must be unlocked by hand.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const {spawnSync} = require('node:child_process');
const root = path.resolve(__dirname, '..');
const serial = process.env.NINELIVES_ANDROID_SERIAL;
if (!serial) throw Error('set NINELIVES_ANDROID_SERIAL to the authorized phone');
// A selection is checked before anything touches the phone: a typo must not
// qualify a matrix that never ran the requested case.
const caseNames = ['phone-healthy', 'phone-operations', 'phone-diagnostics', 'phone-dirty', 'phone-dirty-recovered', 'phone-posture', 'phone-width', 'phone-screen-off', 'phone-locked'];
const selected = process.argv.slice(2);
const unknown = selected.filter(name => !caseNames.includes(name));
if (unknown.length) throw Error(`unknown cases: ${unknown.join(', ')}`);
const sdk = process.env.ANDROID_HOME ?? '/usr/local/share/android-commandlinetools';
const adbPath = path.join(sdk, 'platform-tools/adb');
const work = path.join(root, '.context', 'android-phone-qualification', `smoke-${Date.now()}`);
fs.mkdirSync(work, {recursive: true});
const engine = path.join(work, '9l');
const spec = path.join(root, 'testdata/sdk/android/native.spec.ts');
const config = path.join(root, 'testdata/sdk/android.config.ts');
function sync(command, args, options = {}) {
  const r = spawnSync(command, args, {cwd: root, encoding: 'utf8', timeout: 330000, maxBuffer: 4 << 20, ...options});
  assert.ifError(r.error); return r;
}
const adb = (...args) => sync(adbPath, ['-s', serial, ...args]);
const shell = script => adb('shell', 'sh', '-c', `'${script}'`).stdout.trim();
assert.equal(adb('get-state').stdout.trim(), 'device', 'the selected phone is not an authorized device');
assert.notEqual(shell('getprop ro.kernel.qemu'), '1', 'the selected device is an emulator');
assert.equal(sync('go', ['build', '-o', engine, './cmd/9l']).status, 0);

// Two builds of the same app: each build signs with a fresh key, so the
// second one is a foreign signer for the first.
const fixtures = {};
for (const name of ['phone-a', 'phone-b']) {
  assert.equal(sync(process.execPath, ['scripts/build-android-fixture.cjs', 'healthy']).status, 0, `build ${name}`);
  const dest = path.join(root, '.context/android-fixture', name);
  fs.rmSync(dest, {recursive: true, force: true});
  fs.cpSync(path.join(root, '.context/android-fixture/healthy'), dest, {recursive: true});
  const build = JSON.parse(fs.readFileSync(path.join(dest, 'build.json'), 'utf8'));
  build.apk = path.join(dest, 'fixture.apk');
  fs.writeFileSync(path.join(dest, 'build.json'), JSON.stringify(build, null, 2) + '\n');
  fixtures[name] = build;
}
const results = [];
const record = result => { results.push(result); fs.writeFileSync(path.join(work, 'results.json'), JSON.stringify({version: 1, device: 'physical', cases: results}, null, 2)); console.log(JSON.stringify(result)); };

// installInPlace: first installation or update, the same signer again, then
// a foreign signer refused before any change.
const {installInPlace} = require(path.join(root, 'packages/playwright/dist/android'));
const installed = () => shell('pm path dev.ninelives.fixture 2>/dev/null || true');
(async () => {
  // The run owns the fixture app only if it installs it: an app already on
  // the phone (another signer, kept data) is never replaced or removed.
  if (installed() !== '') throw new assert.AssertionError({message: 'dev.ninelives.fixture is already installed; uninstall it or use another phone'});
  try {
    const first = await installInPlace({sdkRoot: sdk, deviceSerial: serial, apk: fixtures['phone-a'].apk});
    assert.equal(first.apkSHA256, fixtures['phone-a'].apkSHA256);
    assert.deepEqual([first.versionCode, first.versionName], ['7', '1.4.0']);
    assert.match(first.signer, /^[a-f0-9]{64}$/);
    fixtures['phone-a'].signer = first.signer;
    const again = await installInPlace({sdkRoot: sdk, deviceSerial: serial, apk: fixtures['phone-a'].apk});
    assert.equal(again.signer, first.signer);
    const before = installed();
    await assert.rejects(installInPlace({sdkRoot: sdk, deviceSerial: serial, apk: fixtures['phone-b'].apk}), /signer_mismatch/);
    assert.equal(installed(), before, 'a refused install changed nothing');
    record({mode: 'install', qualified: true, versionCode: first.versionCode, versionName: first.versionName});
  } catch (error) {
    record({mode: 'install', qualified: false, reason: error instanceof assert.AssertionError ? String(error.message).split('\n')[0].slice(0, 200) : String(error?.message ?? 'unexpected').slice(0, 120)});
    // Without the run's own installation nothing else is meaningful, and
    // nothing on the phone is changed further.
    throw new assert.AssertionError({message: 'installation failed; the matrix did not run'});
  }
  const creation = sync(engine, ['provenance', spec, '--agent', 'codex']);
  assert.equal(creation.status, 0);
  const provenance = path.join(work, 'creation.json'); fs.writeFileSync(provenance, creation.stdout, {mode: 0o600});

  // A dialog left open before the test: the app has two windows.
  const leaveDialogOpen = () => {
    // A fresh instance through the launcher intent, as a user would open it.
    adb('shell', 'am', 'force-stop', 'dev.ninelives.fixture');
    adb('shell', 'am', 'start', '-W', '-a', 'android.intent.action.MAIN', '-c', 'android.intent.category.LAUNCHER', '-n', 'dev.ninelives.fixture/.MainActivity');
    adb('shell', 'uiautomator', 'dump', '/data/local/tmp/9l-phone.xml');
    const xml = adb('exec-out', 'cat', '/data/local/tmp/9l-phone.xml').stdout;
    adb('shell', 'rm', '-f', '/data/local/tmp/9l-phone.xml');
    const bounds = /resource-id="dev\.ninelives\.fixture:id\/open"[^>]*bounds="\[(\d+),(\d+)\]\[(\d+),(\d+)\]"/.exec(xml);
    assert(bounds, 'open button not found');
    adb('shell', 'input', 'tap', String((+bounds[1] + +bounds[3]) >> 1), String((+bounds[2] + +bounds[4]) >> 1));
    sync('sleep', ['2']);
  };
  const cases = [
    {mode: 'phone-healthy', grep: 'native transfer'},
    {mode: 'phone-operations', grep: 'native operations'},
    {mode: 'phone-diagnostics', grep: 'native operations'},
    {mode: 'phone-dirty', grep: 'native transfer', before: leaveDialogOpen},
    {mode: 'phone-dirty-recovered', grep: 'native transfer', before: leaveDialogOpen},
    {mode: 'phone-posture', grep: 'native transfer'},
    {mode: 'phone-width', grep: 'native transfer'},
    {mode: 'phone-screen-off', grep: 'native transfer', before: () => { adb('shell', 'input', 'keyevent', 'KEYCODE_SLEEP'); sync('sleep', ['3']); }},
    {mode: 'phone-locked', grep: 'native transfer', before: () => { adb('shell', 'input', 'keyevent', 'KEYCODE_WAKEUP'); sync('sleep', ['3']); }},
  ];
  assert.deepEqual(cases.map(c => c.mode), caseNames);
  for (const {mode, grep, before} of cases.filter(c => !selected.length || selected.includes(c.mode))) {
    try {
      if (mode === 'phone-locked') {
        // Only meaningful when waking leaves the keyguard showing.
        before();
        // The driver's own reader: only an observed unlocked state may skip;
        // an unreadable, asleep or locked reading runs the case.
        const {deviceStatePattern, parseDeviceState} = require(path.join(root, 'packages/playwright/dist/android-device'));
        const reading = adb('shell', 'sh', '-c', `'(dumpsys window; dumpsys power) 2>/dev/null | grep -E "${deviceStatePattern}" | head -c 4096'`);
        assert.equal(reading.status, 0, 'phone-locked: the device state could not be read');
        if (parseDeviceState(reading.stdout) === 'unlocked') { record({mode, qualified: true, skipped: 'no lock screen after wake'}); continue; }
      } else before?.();
      const probe = path.join(work, `${mode}-probe.json`);
      const r = sync(engine, ['run', spec, '--sdk', '--config', config, '--target', 'android:finance', '--agent-provenance', provenance,
        '--grep', grep, '--timeout', '4m', '--deadline', '5m', '--attempts', '3', '--receipt-dir', path.join(work, mode), '--format', 'json',
        '--pass-env', 'NINELIVES_ANDROID_CASE,NINELIVES_ANDROID_PROBE,NINELIVES_ANDROID_SERIAL,NINELIVES_ANDROID_SIGNER,ANDROID_HOME,JAVA_HOME'],
      {env: {...process.env, NINELIVES_ANDROID_CASE: mode, NINELIVES_ANDROID_PROBE: probe, NINELIVES_ANDROID_SIGNER: fixtures['phone-a'].signer ?? ''}});
      fs.writeFileSync(path.join(work, `${mode}.json`), r.stdout, {mode: 0o600});
      const run = JSON.parse(r.stdout), receipt = run.receipts[0];
      assert.equal(receipt.attempt, 1, `${mode}: one attempt`);
      assert.equal(receipt.agentProvenance.status, 'matched', mode);
      const facts = fs.existsSync(probe) ? JSON.parse(fs.readFileSync(probe, 'utf8')) : {};
      const setup = (receipt.targetSetup ?? []).map(({testId, ...rest}) => rest);
      const raw = receipt.evidence.stdoutPath ? fs.readFileSync(receipt.evidence.stdoutPath, 'utf8') : '';
      assert(!raw.includes(serial) && !JSON.stringify(run).includes(serial), `${mode}: the serial left the phone`);
      const result = {mode, outcome: run.outcome, targetAssertions: receipt.targetAssertions ?? 0, setup};
      if (mode === 'phone-healthy' || mode === 'phone-operations' || mode === 'phone-dirty-recovered') {
        assert.equal(r.status, 0, `${mode}: passes`); assert.equal(receipt.validated, true);
        const rt = receipt.androidRuntimes[0];
        assert.equal(rt.device, 'physical'); assert.match(rt.deviceHash, /^[a-f0-9]{64}$/);
        assert.match(rt.display, /^[0-9]{1,24}$/); assert(['opened', 'half-opened', 'closed', 'flat', 'unknown'].includes(rt.posture));
        assert.deepEqual([rt.versionCode, rt.versionName, rt.apkSHA256], ['7', '1.4.0', fixtures['phone-a'].apkSHA256]);
        if (mode === 'phone-healthy') assert.equal(rt.signer, fixtures['phone-a'].signer, 'phone-healthy: declared signer recorded');
        Object.assign(result, {api: rt.api, posture: rt.posture, displayWidth: rt.displayWidth, displayHeight: rt.displayHeight, density: rt.density});
      }
      if (mode === 'phone-dirty-recovered') assert.deepEqual(setup, [{classification: 'dirty', reason: 'extra-window', stage: 'open', recovery: {method: 'back', presses: 1, restored: true}}]);
      if (mode === 'phone-dirty') assert.deepEqual(setup, [{classification: 'dirty', reason: 'extra-window', stage: 'open'}]);
      if (mode === 'phone-posture') assert.deepEqual(setup, [{classification: 'mismatch', reason: 'posture', stage: 'open'}]);
      if (mode === 'phone-width') assert.deepEqual(setup, [{classification: 'mismatch', reason: 'display-width', stage: 'open'}]);
      if (mode === 'phone-screen-off') assert.deepEqual(setup, [{classification: 'blocked', reason: 'screen-off', stage: 'open'}]);
      if (mode === 'phone-locked') assert.deepEqual(setup, [{classification: 'blocked', reason: 'device-locked', stage: 'open'}]);
      if (['phone-dirty', 'phone-posture', 'phone-width', 'phone-screen-off', 'phone-locked'].includes(mode)) {
        assert.notEqual(r.status, 0, `${mode}: fails`); assert.equal(facts.stopReason, `setup_${setup[0].classification}`);
      }
      if (mode === 'phone-diagnostics') {
        assert.notEqual(r.status, 0); assert.equal(facts.stopReason, 'assertion_failed'); assert.deepEqual(setup, []);
        const d = receipt.targetDiagnostics?.[0];
        assert(d && d.operation === 'assert-hidden' && d.windowsBefore >= 1 && d.windowsAfter > d.windowsBefore, `phone-diagnostics: window counts ${d?.windowsBefore}->${d?.windowsAfter}`);
        assert.equal(d.focusBefore, 'app');
        result.diagnostic = {windowsBefore: d.windowsBefore, windowsAfter: d.windowsAfter, focusBefore: d.focusBefore, focusAfter: d.focusAfter};
      }
      record({...result, qualified: true});
    } catch (error) {
      record({mode, qualified: false, reason: error instanceof assert.AssertionError ? String(error.message).split('\n')[0].slice(0, 200) : 'unexpected'});
    }
  }
  if (process.env.NINELIVES_ANDROID_KEEP_FIXTURE !== '1') adb('uninstall', 'dev.ninelives.fixture');
  assert(results.every(result => result.qualified !== false), 'Physical phone matrix has unqualified cases');
  console.log(`Bounded physical evidence: ${path.relative(root, work)} (${crypto.createHash('sha256').update(serial).digest('hex').slice(0, 12)})`);
})().catch(error => { console.error(error instanceof assert.AssertionError ? error.message : 'phone smoke failed'); process.exitCode = 1; });
