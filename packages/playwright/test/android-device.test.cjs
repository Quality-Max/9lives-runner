const {test} = require('node:test');
const assert = require('node:assert/strict');
const {parseDeviceState, parseWindows, parseIme} = require('../dist/android-device');

// Excerpts shaped like the filtered dumpsys lines; the driver never keeps them.
test('keyguard and wakefulness markers classify the device without guessing', () => {
  assert.equal(parseDeviceState('    KeyguardServiceDelegate\n      showing=true occluded=false\n    mInputRestricted=true\nmWakefulness=Awake'), 'locked');
  assert.equal(parseDeviceState('      showing=false aodShowing=false\n    mInputRestricted=false\nmWakefulness=Awake'), 'unlocked');
  assert.equal(parseDeviceState('mShowingLockscreen=true mDreamingLockscreen=false'), 'locked');
  assert.equal(parseDeviceState('isStatusBarKeyguard=false mKeyguardShowing=false'), 'unlocked');
  // aodShowing alone is not a keyguard marker.
  assert.equal(parseDeviceState('aodShowing=true'), 'unknown');
  assert.equal(parseDeviceState('      showing=false aodShowing=true\nmWakefulness=Dozing'), 'screen-off');
  assert.equal(parseDeviceState('mWakefulness=Asleep'), 'screen-off');
  assert.equal(parseDeviceState(''), 'unknown');
  assert.equal(parseDeviceState('mWakefulness=Awake'), 'unknown');
});

// The driver filters dumpsys on the device with deviceStatePattern; the
// parser must see the standalone keyguard line that filter keeps.
test('the on-device filter keeps every line the keyguard parser needs', () => {
  const {deviceStatePattern} = require('../dist/android-device');
  const filter = text => text.split('\n').filter(line => new RegExp(deviceStatePattern).test(line)).join('\n');
  const locked = ['WINDOW MANAGER POLICY STATE (dumpsys window policy)', '    KeyguardServiceDelegate', '      showing=true', '      occluded=false', '    mAodShowing=false', 'POWER MANAGER (dumpsys power)', '  mWakefulness=Awake'].join('\n');
  assert.equal(parseDeviceState(filter(locked)), 'locked');
  assert.equal(parseDeviceState(filter(locked.replace('showing=true', 'showing=false'))), 'unlocked');
  assert.equal(parseDeviceState(filter('    KeyguardServiceDelegate\n      aodShowing=true\n  mWakefulness=Awake')), 'unknown');
});

test('app windows are counted by package and focus is classified', () => {
  const text = [
    '  Window #0 Window{1a2b3c u0 NavigationBar0}:',
    '  Window #1 Window{4d5e6f u0 com.example.app/com.example.app.MainActivity}:',
    '  Window #2 Window{7a8b9c u0 com.example.app/com.example.app.MainActivity}:',
    '  Window #3 Window{0d1e2f u0 com.example.apple/com.example.apple.Main}:',
    '  Window #4 Window{3a4b5c u0 InputMethod}:',
    '  mCurrentFocus=Window{7a8b9c u0 com.example.app/com.example.app.MainActivity}',
  ].join('\n');
  assert.deepEqual(parseWindows(text, 'com.example.app'), {windows: 2, focus: 'app'});
  assert.deepEqual(parseWindows(text.replace(/mCurrentFocus=.*/, 'mCurrentFocus=Window{aa u0 com.android.systemui/.Dialog}'), 'com.example.app'), {windows: 2, focus: 'foreign'});
  assert.deepEqual(parseWindows(text.replace(/mCurrentFocus=.*/, 'mCurrentFocus=null'), 'com.example.app'), {windows: 2, focus: 'none'});
  assert.deepEqual(parseWindows('  Window #1 Window{4d5e6f u0 com.example.app/com.example.app.MainActivity}:', 'com.example.app'), {windows: 1, focus: 'unknown'});
  assert.deepEqual(parseWindows('', 'com.example.app'), {windows: 0, focus: 'unknown'});
});

test('keyboard visibility reads mInputShown only', () => {
  assert.equal(parseIme('  mInputShown=true mImeWindowVis=0x2'), 'shown');
  assert.equal(parseIme('  mInputShown=false'), 'hidden');
  assert.equal(parseIme('mImeWindowVis=0x0'), 'unknown');
});
