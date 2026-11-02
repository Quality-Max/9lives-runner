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

const {parseDisplays, activeDisplay, parsePosture} = require('../dist/android-device');

test('physical displays are read with id, size, density and power state; the active one is the single powered display', () => {
  const inner = 'DisplayDeviceInfo{"Built-in Screen": uniqueId="local:4619827259835644672", 1812 x 2176, modeId 1, defaultModeId 1, supportedModes [...], colorMode 0, supportedColorModes [0], hdrCapabilities ..., allmSupported false, gameContentTypeSupported false, density 374, 374.0 x 374.0 dpi, appVsyncOff 1000000, presDeadline 11111111, touch INTERNAL, rotation 0, type INTERNAL, address {port=0, model=...}, deviceProductInfo ..., state ON, frameRateOverride ..., brightnessMinimum 0.0, brightnessMaximum 1.0, brightnessDefault 0.4, FLAG_DEFAULT_DISPLAY, FLAG_ROTATES_WITH_CONTENT, FLAG_SECURE, FLAG_SUPPORTS_PROTECTED_BUFFERS, FLAG_TRUSTED}';
  const cover = 'DisplayDeviceInfo{"Built-in Screen": uniqueId="local:4619827259835644673", 904 x 2316, modeId 2, density 420, state OFF, FLAG_ROTATES_WITH_CONTENT}';
  const displays = parseDisplays([inner, cover].join('\n'));
  assert.deepEqual(displays, [
    {id: '4619827259835644672', width: 1812, height: 2176, density: 374, on: true},
    {id: '4619827259835644673', width: 904, height: 2316, density: 420, on: false},
  ]);
  assert.equal(activeDisplay(displays).id, '4619827259835644672');
  assert.equal(activeDisplay(parseDisplays([inner.replace('state ON', 'state OFF'), cover.replace('state OFF', 'state ON')].join('\n'))).id, '4619827259835644673');
  assert.equal(activeDisplay(parseDisplays([inner, cover.replace('state OFF', 'state ON')].join('\n'))), undefined, 'two powered displays cannot be chosen between');
  assert.equal(activeDisplay(parseDisplays(cover)).id, '4619827259835644673', 'a single display is the target whatever its state');
  assert.deepEqual(parseDisplays('DisplayDeviceInfo{"Overlay #1": uniqueId="overlay:1", 1080 x 1920, density 320, state ON}'), [], 'only local displays');
  assert.deepEqual(parseDisplays(''), []);
  // A device's cached mInfo= record repeats its top-level one: one display, still choosable.
  const repeated = parseDisplays([inner, cover, `      mInfo=${inner}`].join('\n'));
  assert.equal(repeated.length, 2);
  assert.equal(activeDisplay(repeated).id, '4619827259835644672');
});

test('fold posture comes from the committed device state only', () => {
  assert.equal(parsePosture("  Committed state: DeviceState{identifier=2, name='OPENED', app_accessible=true}"), 'opened');
  assert.equal(parsePosture("  Committed state: DeviceState{identifier=1, name='HALF_OPENED'}"), 'half-opened');
  assert.equal(parsePosture('mCommittedState=DeviceState{identifier=0, name=CLOSED}'), 'closed');
  assert.equal(parsePosture("Committed state: DeviceState{identifier=4, name='REAR_DISPLAY'}"), 'unknown');
  assert.equal(parsePosture("Committed state: DeviceState{identifier=3, name='UNFOLDED'}"), 'unknown', 'no substring matching');
  assert.equal(parsePosture("Committed state: DeviceState{identifier=5, name='FOLDED'}"), 'unknown');
  assert.equal(parsePosture("Can't find service: device_state"), 'flat');
  assert.equal(parsePosture(''), 'unknown');
});

const {parsePackageVersion, parseSigner} = require('../dist/android-device');

test('package versions and the signer digest are read from tool output only', () => {
  assert.deepEqual(parsePackageVersion('    versionCode=29 minSdk=24 targetSdk=35\n    versionName=1.4.0\n'), {versionCode: '29', versionName: '1.4.0'});
  assert.deepEqual(parsePackageVersion('    versionName=1.4.0 (debug)\n'), {versionCode: 'unknown', versionName: 'unknown'});
  assert.deepEqual(parsePackageVersion(''), {versionCode: 'unknown', versionName: 'unknown'});
  assert.equal(parseSigner(`Signer #1 certificate DN: CN=9l Synthetic Fixture\nSigner #1 certificate SHA-256 digest: ${'f'.repeat(64)}\nSigner #1 certificate SHA-1 digest: ${'1'.repeat(40)}`), 'f'.repeat(64));
  assert.equal(parseSigner('DOES NOT VERIFY'), undefined);
  // A certificate DN quoting a digest line cannot supply the signer.
  assert.equal(parseSigner(`Signer #1 certificate DN: CN=Signer #1 certificate SHA-256 digest: ${'e'.repeat(64)}\nSigner #1 certificate SHA-256 digest: ${'f'.repeat(64)}`), 'f'.repeat(64));
  assert.equal(parseSigner(`Signer #1 certificate DN: CN=Signer #1 certificate SHA-256 digest: ${'e'.repeat(64)}`), undefined);
  // A field only counts at the start of its own line.
  assert.deepEqual(parsePackageVersion('    versionName=debug versionName=1.4.0\n'), {versionCode: 'unknown', versionName: 'unknown'});
  assert.deepEqual(parsePackageVersion('    versionName=build-versionCode=28\n'), {versionCode: 'unknown', versionName: 'unknown'});
  assert.deepEqual(parsePackageVersion('    flags=[ versionCode=28 ]\n    versionName=1.4.0\n'), {versionCode: 'unknown', versionName: '1.4.0'});
});

const {parseLogicalDisplays} = require('../dist/android-device');

test('the logical layout follows rotation and density overrides, per physical display', () => {
  const base = '    mBaseDisplayInfo=DisplayInfo{"Built-in Screen", displayId 0", FLAG_TRUSTED, real 1080 x 2400, largest app 1080 x 2400, smallest app 1080 x 2400, mode 1, rotation 0, state ON, type INTERNAL, uniqueId "local:4619827259835644672", app 1080 x 2400, density 420 (420.0 x 420.0) dpi, layerStack 0}';
  const override = '    mOverrideDisplayInfo=DisplayInfo{"Built-in Screen", displayId 0", FLAG_TRUSTED, real 2400 x 1080, largest app 2400 x 1080, smallest app 2400 x 1080, mode 1, rotation 1, state ON, type INTERNAL, uniqueId "local:4619827259835644672", app 2400 x 1080, density 440 (440.0 x 440.0) dpi, layerStack 0}';
  assert.deepEqual([...parseLogicalDisplays(base).entries()], [['4619827259835644672', {width: 1080, height: 2400, density: 420}]]);
  assert.deepEqual(parseLogicalDisplays([base, override].join('\n')).get('4619827259835644672'), {width: 2400, height: 1080, density: 440});
  assert.deepEqual(parseLogicalDisplays([override, base].join('\n')).get('4619827259835644672'), {width: 2400, height: 1080, density: 440}, 'the override wins whatever the order');
  assert.equal(parseLogicalDisplays('mBaseDisplayInfo=DisplayInfo{"Overlay", uniqueId "overlay:1", app 100 x 100, density 160}').size, 0);
  // The current app size, not the nominal largest/smallest range before it.
  const portrait = '    mOverrideDisplayInfo=DisplayInfo{"Built-in Screen", displayId 0", FLAG_TRUSTED, real 1080 x 2400, largest app 2400 x 2400, smallest app 1080 x 1080, mode 1, rotation 0, state ON, type INTERNAL, uniqueId "local:4619827259835644672", app 1080 x 2265, density 420 (420.0 x 420.0) dpi, layerStack 0}';
  assert.deepEqual(parseLogicalDisplays(portrait).get('4619827259835644672'), {width: 1080, height: 2265, density: 420});
});
