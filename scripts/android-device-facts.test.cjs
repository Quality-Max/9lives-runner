const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');
const {parseAppLog, readAppLog, frontWindow} = require('../testdata/sdk/android/device-facts.cjs');

test('the fixture grammar is kept in order', () => {
  const log = 'create\ndrawn\nclick send 1 3\n';
  assert.deepEqual(parseAppLog(log), {log: 'read', events: ['create', 'drawn', 'click send 1 3']});
  assert.deepEqual(parseAppLog('create\ndrawn out\nclick sign-in\ncreate\ndrawn in\n').events,
    ['create', 'drawn out', 'click sign-in', 'create', 'drawn in']);
  assert.deepEqual(parseAppLog('create\ndrawn\nclick validate invalid 3\nclick validate valid 16\nclick login 16\n').events,
    ['create', 'drawn', 'click validate invalid 3', 'click validate valid 16', 'click login 16']);
});

test('anything outside the grammar is counted, never kept', () => {
  const facts = parseAppLog('create\nSent $5 to Ada\nclick send 1 3 Ada\nclick validate maybe 3\ndrawn sideways\n\n');
  assert.deepEqual(facts, {log: 'read', events: ['create'], other: 4});
  assert.doesNotMatch(JSON.stringify(facts), /Sent|Ada|maybe|sideways/);
});

test('at most sixteen events are kept and truncation is explicit', () => {
  const facts = parseAppLog('create\n'.repeat(20));
  assert.equal(facts.events.length, 16);
  assert.equal(facts.truncated, true);
  assert.deepEqual(parseAppLog(''), {log: 'read', events: []});
});

// A stand-in for adb: its answers come from the script's own behaviour.
function fakeAdb(t, body) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), '9l-fake-adb-'));
  t.after(() => fs.rmSync(dir, {recursive: true, force: true}));
  const file = path.join(dir, 'adb');
  fs.writeFileSync(file, `#!/bin/sh\n${body}\n`, {mode: 0o700});
  return file;
}
const posix = {skip: process.platform === 'win32'};

test('a log read names the serial, package and file, and parses the answer', posix, async t => {
  const adb = fakeAdb(t, 'test "$*" = "-s emulator-5554 shell run-as dev.ninelives.fixture cat files/handled" || exit 9\nprintf "create\\ndrawn\\nclick send 1 3\\n"');
  assert.deepEqual(await readAppLog(adb, 'emulator-5554', 'dev.ninelives.fixture'), {log: 'read', events: ['create', 'drawn', 'click send 1 3']});
});

test('a missing log, a stalled read and an unreachable device are facts, not errors', posix, async t => {
  assert.deepEqual(await readAppLog(fakeAdb(t, 'echo "cat: files/handled: No such file or directory" >&2; exit 1'), 's', 'p'), {log: 'missing'});
  assert.deepEqual(await readAppLog(fakeAdb(t, 'exec sleep 5'), 's', 'p', 200), {log: 'timeout'});
  assert.deepEqual(await readAppLog(fakeAdb(t, "echo \"error: device 's' not found\" >&2; exit 1"), 's', 'p'), {log: 'unreachable'});
  assert.deepEqual(await readAppLog(fakeAdb(t, 'head -c 5000 /dev/zero | tr "\\0" "x"'), 's', 'p'), {log: 'oversized'});
  assert.deepEqual(await readAppLog(path.join(os.tmpdir(), '9l-no-such-adb'), 's', 'p'), {log: 'unreachable'});
});

test('the focused window is the app, another one or unknown, never its name', posix, async t => {
  const focus = owner => fakeAdb(t, `printf "  mCurrentFocus=Window{1a2b3c u0 ${owner}/dev.ninelives.fixture.MainActivity}\\n"`);
  assert.equal(await frontWindow(focus('dev.ninelives.fixture'), 's', 'dev.ninelives.fixture'), 'app');
  assert.equal(await frontWindow(focus('com.android.systemui'), 's', 'dev.ninelives.fixture'), 'other');
  assert.equal(await frontWindow(fakeAdb(t, 'printf "  mCurrentFocus=null\\n"'), 's', 'dev.ninelives.fixture'), 'unknown');
  assert.equal(await frontWindow(fakeAdb(t, 'exit 1'), 's', 'dev.ninelives.fixture'), 'unknown');
});
