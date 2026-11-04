// Prepare a dedicated AVD for biometric specs: wipe it, set a random lock
// PIN that is never printed or stored, enroll fingerprint 1 through
// Settings with the emulator's virtual sensor, and save the unlocked state
// as a named snapshot. androidTargetDriver({biometrics: {snapshot}}) then
// boots that snapshot read-only, so the driver never sees, sets or enters a
// PIN, and every test starts from the same state. The AVD's own data is
// erased: use one only for this. Its PIN is unrecoverable by design; run
// this again (it wipes again) when the snapshot stops loading.
//
// node scripts/prepare-biometric-avd.cjs --avd 9l_biometric_API_35 --wipe [--snapshot 9l-biometric]
const {spawn, execFileSync} = require('node:child_process');
const {randomInt} = require('node:crypto');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const {androidDefaults, androidToolPaths} = require('../packages/playwright/dist/android-env');

const args = process.argv.slice(2);
const option = name => { const i = args.indexOf(name); return i >= 0 ? args[i + 1] : undefined; };
const avd = option('--avd'), snapshot = option('--snapshot') ?? '9l-biometric';
// Both name directories this script deletes inside: a name starts with a
// letter or digit, so `.` and `..` (and hidden names) are refused.
const name = /^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$/;
if (!avd || !name.test(avd) || !name.test(snapshot)) {
  console.error('usage: node scripts/prepare-biometric-avd.cjs --avd <dedicated AVD> --wipe [--snapshot <name>]');
  process.exit(2);
}
if (!args.includes('--wipe')) {
  console.error(`This erases all data of the AVD "${avd}" and gives it a lock PIN no one knows. Pass --wipe to confirm it is dedicated to biometric specs.`);
  process.exit(2);
}
const sdk = androidDefaults().sdkRoot;
if (!sdk) { console.error('No Android SDK found: set ANDROID_HOME, or run 9l doctor --android'); process.exit(2); }
const {adb, emulator} = androidToolPaths(path.resolve(sdk));
// The AVD's directory, as the emulator finds it: its snapshots live there,
// and the console (through adb) does not relay what it saved.
const avdHome = process.env.ANDROID_AVD_HOME ?? path.join(process.env.ANDROID_USER_HOME ?? process.env.ANDROID_EMULATOR_HOME ?? path.join(os.homedir(), '.android'), 'avd');
const avdDir = /^path=(.+)$/m.exec(fs.readFileSync(path.join(avdHome, `${avd}.ini`), 'utf8'))?.[1]?.trim();
if (!avdDir || !fs.existsSync(avdDir)) { console.error(`AVD "${avd}" not found under ${avdHome}`); process.exit(2); }
const snapshots = path.join(avdDir, 'snapshots'), snapshotDir = path.join(snapshots, snapshot);
// Deleted only when it is a direct child of the AVD's snapshot directory.
if (path.dirname(snapshotDir) !== snapshots) { console.error('invalid snapshot name'); process.exit(2); }
// A snapshot from before the wipe would resume the old data and PIN.
fs.rmSync(snapshotDir, {recursive: true, force: true});
const sleep = ms => Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, ms);
const devices = () => execFileSync(adb, ['devices'], {encoding: 'utf8', timeout: 30000});
const port = [...Array(40).keys()].map(i => 5600 + i * 2).find(p => !devices().includes(`emulator-${p}`));
const serial = `emulator-${port}`;
// Device output stays in memory; only progress lines are printed. A failed
// command's own error names its arguments, which include the PIN, so it is
// replaced by a fixed message before anything can print it.
const run = (...a) => {
  try { return execFileSync(adb, ['-s', serial, ...a], {encoding: 'utf8', timeout: 60000, stdio: ['ignore', 'pipe', 'pipe']}); }
  catch { throw new Error('a device command failed'); }
};

async function main() {
const child = spawn(emulator, ['-avd', avd, '-wipe-data', '-no-snapshot-load', '-no-audio', '-no-window', '-gpu', 'swiftshader_indirect', '-port', String(port)], {stdio: 'ignore'});
let failed = '';
try {
  for (let i = 0; ; i++) {
    if (i > 150) throw new Error('the AVD did not finish booting in 5 minutes');
    try { if (run('shell', 'getprop', 'sys.boot_completed').trim() === '1') break; } catch {}
    sleep(2000);
  }
  console.log(`booted ${avd} wiped`);
  const pin = String(randomInt(100000, 1000000));
  run('shell', 'locksettings', 'set-pin', pin);
  run('shell', 'am', 'start', '-W', '-a', 'android.settings.FINGERPRINT_ENROLL');
  sleep(2000);
  const buttons = ['more', 'i agree', 'agree', 'next', 'start', 'continue', 'ok', 'done'];
  let added = false;
  for (let step = 0; step < 60 && !added; step++) {
    let xml;
    try { run('shell', 'uiautomator', 'dump', '/sdcard/9l-enroll.xml'); xml = run('shell', 'cat', '/sdcard/9l-enroll.xml'); } catch { sleep(1000); continue; }
    const nodes = [...xml.matchAll(/<node [^>]*>/g)].map(m => m[0]);
    const screen = nodes.map(n => /text="([^"]*)"/.exec(n)?.[1] ?? '').filter(Boolean).join(' | ');
    const target = nodes.find(n => /clickable="true"/.test(n) && buttons.includes((/text="([^"]*)"/.exec(n)?.[1] ?? '').toLowerCase()));
    if (/enter your pin|re-enter your pin|confirm your pin/i.test(screen)) {
      run('shell', 'input', 'text', pin); run('shell', 'input', 'keyevent', '66'); sleep(2500);
    } else if (target) {
      const [, x1, y1, x2, y2] = /bounds="\[(\d+),(\d+)\]\[(\d+),(\d+)\]"/.exec(target).map(Number);
      const label = /text="([^"]*)"/.exec(target)[1].toLowerCase();
      run('shell', 'input', 'tap', String((x1 + x2) >> 1), String((y1 + y2) >> 1));
      added = label === 'done' && /fingerprint added/i.test(screen);
      sleep(1500);
    } else if (/touch the sensor|lift|put your finger|touch and hold|keep lifting|again/i.test(screen)) {
      run('emu', 'finger', 'touch', '1'); sleep(800); run('emu', 'finger', 'remove', '1'); sleep(800);
    } else sleep(1500);
  }
  run('shell', 'rm', '-f', '/sdcard/9l-enroll.xml');
  const enrolled = [...run('shell', 'dumpsys', 'fingerprint').matchAll(/"count":([0-9]{1,4})/g)].reduce((sum, m) => sum + Number(m[1]), 0);
  if (!added || enrolled < 1) throw new Error('fingerprint enrollment did not complete; the Settings flow of this image may differ');
  console.log('fingerprint 1 enrolled');
  run('shell', 'input', 'keyevent', '3'); sleep(2000);
  run('emu', 'avd', 'snapshot', 'save', snapshot);
  for (let i = 0; i < 60 && !fs.existsSync(path.join(snapshotDir, 'snapshot.pb')); i++) sleep(1000);
  if (!fs.existsSync(path.join(snapshotDir, 'snapshot.pb'))) throw new Error('the snapshot was not saved');
  console.log(`saved snapshot ${snapshot}: use androidTargetDriver({avd: '${avd}', biometrics: {snapshot: '${snapshot}'}, ...})`);
} catch (error) {
  // Only this script's own messages: run() never passes a command's text.
  failed = error instanceof Error ? error.message : 'preparation failed';
} finally {
  try { run('emu', 'kill'); } catch { child.kill('SIGKILL'); }
  // Wait for the emulator to exit, so the AVD is free for the first test;
  // the exit event needs the event loop, so this wait yields to it.
  const exited = (ms) => new Promise(resolve => {
    if (child.exitCode !== null || child.signalCode !== null) return resolve(true);
    const timer = setTimeout(() => resolve(false), ms);
    child.once('exit', () => { clearTimeout(timer); resolve(true); });
  });
  if (!await exited(60000)) {
    child.kill('SIGKILL');
    if (!await exited(10000) && !failed) failed = 'the emulator did not stop';
  }
}
if (failed) { console.error(`9l biometric AVD: ${failed}`); process.exit(1); }
}
main();
