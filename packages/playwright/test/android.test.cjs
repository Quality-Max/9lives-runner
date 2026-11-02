const {test} = require('node:test');
const assert = require('node:assert/strict');
const {validAndroidRuntime, androidVersions} = require('../dist/android-runtime');

test('native metadata rejects unknown fields, content, missing identity and unsupported versions', () => {
  const runtime = {appPackage:'dev.ninelives.fixture',apkSHA256:'a'.repeat(64),api:'35',release:'15',abi:'arm64-v8a',emulator:'36.4.9',...androidVersions,worker:'b'.repeat(64),display:'4619827259835644672',displayWidth:'1080',displayHeight:'2340',density:'420',posture:'flat',versionCode:'28',versionName:'1.4.0',signer:'d'.repeat(64)};
  assert.equal(validAndroidRuntime(runtime), true);
  for (const key of Object.keys(runtime)) {
    assert.equal(validAndroidRuntime({...runtime,[key]:'synthetic page content'}),false,key);
    const missing = {...runtime};delete missing[key];
    assert.equal(validAndroidRuntime(missing),false,key);
  }
  assert.equal(validAndroidRuntime({...runtime,screen:'synthetic'}),false);
  assert.equal(validAndroidRuntime({...runtime,appium:'3.8.1'}),false);
  assert.equal(validAndroidRuntime({...runtime,uiautomator2:'8.7.1'}),false);
  assert.equal(validAndroidRuntime(null),false);
  assert.equal(validAndroidRuntime({...runtime,display:'unknown',displayWidth:'0',displayHeight:'0',density:'0',posture:'unknown'}),true);
  assert.equal(validAndroidRuntime({...runtime,posture:'half-opened'}),true);
  assert.equal(validAndroidRuntime({...runtime,display:'Built-in Screen'}),false);
  assert.equal(validAndroidRuntime({...runtime,posture:"DeviceState{name='OPENED'}"}),false);
  assert.equal(validAndroidRuntime({...runtime,versionCode:'unknown',versionName:'unknown',signer:'unknown'}),true);
  assert.equal(validAndroidRuntime({...runtime,versionName:'1.4.0 (debug build for user@host)'}),false);
  assert.equal(validAndroidRuntime({...runtime,signer:'CN=9l Synthetic Fixture'}),false);
});

test('physical metadata is distinct from emulator identity and excludes raw serials', () => {
  const runtime = {appPackage:'dev.ninelives.fixture',apkSHA256:'a'.repeat(64),api:'37',release:'17',abi:'arm64-v8a',device:'physical',deviceHash:'c'.repeat(64),...androidVersions,worker:'b'.repeat(64),display:'4619827259835644672',displayWidth:'1812',displayHeight:'2176',density:'374',posture:'opened',versionCode:'29',versionName:'1.4.0',signer:'unknown'};
  assert.equal(validAndroidRuntime(runtime), true);
  for (const key of Object.keys(runtime)) {
    const missing = {...runtime}; delete missing[key];
    assert.equal(validAndroidRuntime(missing), false, key);
  }
  for (const extra of [{emulator:'36.4.9'},{serial:'private'},{device:'emulator'},{deviceHash:'private'}]) {
    assert.equal(validAndroidRuntime({...runtime,...extra}), false);
  }
});

test('attached phone verifies the build, preserves app data and restores owned automation on close and cancellation', async () => {
  const cp = require('node:child_process');
  const {EventEmitter} = require('node:events');
  const fs = require('node:fs/promises');
  const path = require('node:path');
  const os = require('node:os');
  const {randomUUID} = require('node:crypto');
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), '9l-phone-test-'));
  const original = {execFile: cp.execFile, spawn: cp.spawn, fetch: global.fetch, scratch:process.env.NINELIVES_TARGET_SCRATCH_DIR, tmpdir:process.env.TMPDIR};
  const commands = [], requests = [];
  let installedHash = 'a'.repeat(64), elements = 1, holdClick = false, physicalQemu = '';
  const policies = new Map([['hidden_api_policy_pre_p_apps','null'],['hidden_api_policy_p_apps','2'],['hidden_api_policy','0']]);
  const initialPolicies = [...policies];
  let mockPermission = 'deny', mockStore = 'original synthetic helper index', backup;
  const serial = `test-${randomUUID()}`;
  let driver;
  try {
    for (const [name, version] of Object.entries({appium:androidVersions.appium,'appium-uiautomator2-driver':androidVersions.uiautomator2})) {
      await fs.mkdir(path.join(dir,'node_modules',name),{recursive:true});
      await fs.writeFile(path.join(dir,'node_modules',name,'package.json'), JSON.stringify({version}));
    }
    process.env.NINELIVES_TARGET_SCRATCH_DIR = dir;
    process.env.TMPDIR = path.join(dir,'tmp-a');
    await fs.mkdir(process.env.TMPDIR);
    cp.execFile = (file,args,options,callback) => {
      commands.push(args);
      const command = args.slice(2);
      let result = '';
      if (command[0] === 'get-state') result = 'device';
      else if (command[1] === 'getprop') result = ({'ro.kernel.qemu':physicalQemu,'ro.build.version.sdk':'37','ro.build.version.release':'17','ro.product.cpu.abi':'arm64-v8a'})[command[2]];
      else if (command[1] === 'pm' && command[2] === 'path') result = command[3] === 'io.appium.settings' ? '' : 'package:/data/app/test/base.apk';
      else if (command[1] === 'sh' && command[3].includes('pm path io.appium.settings')) result = 'present';
      else if (command[1] === 'sh' && command[3].includes('echo present')) { backup = mockStore; result = 'present'; }
      else if (command[1] === 'sh' && command[3].includes('cp /data/local/tmp/9l-')) { mockStore = backup; backup = undefined; }
      else if (command[1] === 'appops') {
        if (command[2] === 'get') result = `MOCK_LOCATION: ${mockPermission}`;
        else mockPermission = command[5];
      }
      else if (command[1] === 'sha256sum') result = `${installedHash}  /data/app/test/base.apk`;
      else if (command[1] === 'settings') {
        const [, , op, , key, value] = command;
        if (op === 'get') result = policies.get(key);
        else policies.set(key, op === 'delete' ? 'null' : value);
      }
      queueMicrotask(() => callback(null,result));
      return new EventEmitter();
    };
    cp.spawn = () => {
      const child = new EventEmitter(); child.pid = 123; child.exitCode = null; child.signalCode = null;
      child.kill = signal => {child.signalCode = signal; queueMicrotask(() => child.emit('exit')); return true;};
      return child;
    };
    global.fetch = async (input, init) => {
      const url = String(input), body = init.body ? JSON.parse(init.body) : undefined;
      requests.push({url,body});
      if (url.endsWith('/click') && holdClick) return new Promise((resolve,reject) => {
        init.signal.addEventListener('abort', () => reject(new Error('aborted')), {once:true});
      });
      let value = null;
      if (url.endsWith('/status')) value = {ready:true};
      else if (url.endsWith('/session')) {
        for (const key of policies.keys()) policies.set(key,'1');
        mockPermission = 'allow'; mockStore = 'automation helper index';
        value = {sessionId:'owned-session',capabilities:{'appium:udid':serial}};
      } else if (url.endsWith('/elements')) value = Array.from({length:elements},(_,i)=>({'element-6066-11e4-a52e-4f735466cecf':`ref-${i}`}));
      else if (url.endsWith('/displayed')) value = true;
      return new Response(JSON.stringify({value}),{status:200});
    };
    const {androidTargetDriver} = require('../dist/android');
    const options = {appId:'phone',sdkRoot:dir,toolsRoot:dir,deviceSerial:serial,apkSHA256:'a'.repeat(64),appPackage:'dev.ninelives.fixture',activity:'.MainActivity',controls:{projects:{accessibilityIdSuffix:', Projects',className:'android.view.View'}}};
    const context = () => ({signal:new AbortController().signal,deadlineMs:Date.now()+30000});
    driver = androidTargetDriver(options);
    await assert.rejects(driver.reset(context()), /reset_unsupported/);
    assert.equal(commands.length,0);
    await driver.open(context());
    assert.equal(validAndroidRuntime(driver.runtime()),true);
    assert.equal(driver.runtime().device,'physical');
    const caps = requests.find(r=>r.url.endsWith('/session')).body.capabilities.alwaysMatch;
    for (const k of ['fullReset','forceAppLaunch','shouldTerminateApp','autoGrantPermissions']) assert.equal(caps[`appium:${k}`],false,k);
    assert.equal(Object.hasOwn(caps,'appium:hideKeyboard'),false);
    for (const k of ['noReset','dontStopAppOnReset','skipUnlock','skipDeviceInitialization']) assert.equal(caps[`appium:${k}`],true,k);
    process.env.TMPDIR = path.join(dir,'tmp-b');
    await fs.mkdir(process.env.TMPDIR);
    const contender = androidTargetDriver(options);
    await assert.rejects(contender.open(context()), /open_failed_device_attach/);
    await contender.close();
    const [handle] = await driver.observeTestId('projects',context());
    assert.equal(await handle.visible(context()), true);
    assert.match(requests.find(r=>r.url.endsWith('/elements')).body.value,/packageName\("dev.ninelives.fixture"\).*descriptionMatches/);
    elements = 2;
    await assert.rejects(handle.tap(context()), /ambiguous_resource_id/);
    assert.equal(requests.filter(r=>r.url.endsWith('/click')).length,0);
    elements = 1;
    await handle.tap(context());
    holdClick = true;
    const pending = assert.rejects(handle.tap(context()),/transport_failed/);
    await new Promise(resolve=>setImmediate(resolve));
    await driver.close();
    await pending;
    // The helper is stopped, never the app. No installation/clear commands.
    assert(commands.some(a=>a.includes('force-stop')&&a.includes('io.appium.uiautomator2.server')));
    assert(!commands.some(a=>a.includes('install')||a.includes('clear')||(a.includes('force-stop')&&a.includes(options.appPackage))));
    assert.deepEqual([...policies],initialPolicies);
    assert.equal(mockPermission,'deny');
    assert.equal(mockStore,'original synthetic helper index');
    assert.equal(backup,undefined);
    for (const [hash,qemu,reason] of [['b'.repeat(64),'',/build-digest/],['a'.repeat(64),'1',/open_failed_device_attach/]]) {
      installedHash=hash; physicalQemu=qemu;
      const before=requests.filter(r=>r.url.endsWith('/session')).length;
      const bad=androidTargetDriver(options);
      await assert.rejects(bad.open(context()),reason);
      await bad.close();
      assert.equal(requests.filter(r=>r.url.endsWith('/session')).length,before);
    }
  } finally {
    await driver?.close();
    cp.execFile=original.execFile;cp.spawn=original.spawn;global.fetch=original.fetch;
    if(original.scratch===undefined) delete process.env.NINELIVES_TARGET_SCRATCH_DIR;
    else process.env.NINELIVES_TARGET_SCRATCH_DIR=original.scratch;
    if(original.tmpdir===undefined) delete process.env.TMPDIR;
    else process.env.TMPDIR=original.tmpdir;
    await fs.rm(dir,{recursive:true,force:true});
  }
});

// A locked phone, state a previous test left behind, and a declared Back
// recovery are classified before any application operation. The driver only
// reads device state; the one mutation is the declared Back press.
test('attached phone classifies a locked device and leaked state as setup, never as an application failure', async () => {
  const cp = require('node:child_process');
  const {EventEmitter} = require('node:events');
  const fs = require('node:fs/promises');
  const path = require('node:path');
  const os = require('node:os');
  const {randomUUID} = require('node:crypto');
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), '9l-phone-setup-'));
  const original = {execFile: cp.execFile, spawn: cp.spawn, fetch: global.fetch, scratch: process.env.NINELIVES_TARGET_SCRATCH_DIR, tmpdir: process.env.TMPDIR};
  const appWindow = '  Window #1 Window{4d5e6f u0 dev.ninelives.fixture/dev.ninelives.fixture.MainActivity}:';
  const focus = '  mCurrentFocus=Window{4d5e6f u0 dev.ninelives.fixture/dev.ninelives.fixture.MainActivity}';
  const device = {state: 'showing=false\nmInputRestricted=false\nmWakefulness=Awake', windows: [appWindow, focus].join('\n'), ime: 'mInputShown=false'};
  let commands = [], sessions = 0, backPresses = 0, onBack = () => {}, failReads = false, failWindows = false, failBackAfter = Infinity;
  const serial = `test-${randomUUID()}`;
  let opened = [];
  try {
    for (const [name, version] of Object.entries({appium: androidVersions.appium, 'appium-uiautomator2-driver': androidVersions.uiautomator2})) {
      await fs.mkdir(path.join(dir, 'node_modules', name), {recursive: true});
      await fs.writeFile(path.join(dir, 'node_modules', name, 'package.json'), JSON.stringify({version}));
    }
    process.env.NINELIVES_TARGET_SCRATCH_DIR = dir;
    process.env.TMPDIR = dir;
    cp.execFile = (file, args, options, callback) => {
      commands.push(args);
      const command = args.slice(2);
      const script = command[1] === 'sh' ? command[3] : '';
      let result = '';
      if (command[0] === 'get-state') result = 'device';
      else if (command[1] === 'getprop') result = ({'ro.kernel.qemu': '', 'ro.build.version.sdk': '37', 'ro.build.version.release': '17', 'ro.product.cpu.abi': 'arm64-v8a'})[command[2]];
      else if (command[1] === 'pm' && command[2] === 'path') result = 'package:/data/app/test/base.apk';
      else if (script.includes('dumpsys window; dumpsys power')) { if (failReads) { queueMicrotask(() => callback(new Error('device offline'))); return new EventEmitter(); } result = device.state; }
      else if (script.includes('dumpsys window windows')) { if (failWindows) { queueMicrotask(() => callback(new Error('device offline'))); return new EventEmitter(); } result = `${device.windows}\n9l-windows-end`; }
      else if (script.includes('dumpsys input_method')) result = device.ime;
      else if (script.includes('pm path io.appium.settings')) result = 'absent';
      else if (script.includes('echo present')) result = 'absent';
      else if (command[1] === 'input' && command[3] === 'KEYCODE_BACK') { if (backPresses >= failBackAfter) { queueMicrotask(() => callback(new Error('device offline'))); return new EventEmitter(); } backPresses++; onBack(); }
      else if (command[1] === 'sha256sum') result = `${'a'.repeat(64)}  /data/app/test/base.apk`;
      else if (command[1] === 'settings') result = command[2] === 'get' ? '0' : '';
      queueMicrotask(() => callback(null, result));
      return new EventEmitter();
    };
    cp.spawn = () => {
      const child = new EventEmitter(); child.pid = 123; child.exitCode = null; child.signalCode = null;
      child.kill = signal => { child.signalCode = signal; queueMicrotask(() => child.emit('exit')); return true; };
      return child;
    };
    global.fetch = async input => {
      const url = String(input);
      let value = null;
      if (url.endsWith('/status')) value = {ready: true};
      else if (url.endsWith('/session')) { sessions++; value = {sessionId: 'owned-session', capabilities: {'appium:udid': serial}}; }
      else if (url.endsWith('/elements')) value = [{'element-6066-11e4-a52e-4f735466cecf': 'ref-0'}];
      else if (url.endsWith('/displayed')) value = false;
      return new Response(JSON.stringify({value}), {status: 200});
    };
    const {androidTargetDriver, TargetSetupError} = require('../dist/android');
    const base = {appId: 'phone', sdkRoot: dir, toolsRoot: dir, deviceSerial: serial, apkSHA256: 'a'.repeat(64), appPackage: 'dev.ninelives.fixture', activity: '.MainActivity'};
    const context = () => ({signal: new AbortController().signal, deadlineMs: Date.now() + 30000});
    const open = async (options, expectation) => {
      commands = []; sessions = 0; backPresses = 0;
      const driver = androidTargetDriver({...base, ...options});
      opened.push(driver);
      let error;
      try { await driver.open(context()); } catch (e) { error = e; }
      if (expectation === undefined) { assert.equal(error, undefined); return driver; }
      assert(error instanceof TargetSetupError, `expected a setup classification, got ${error?.message}`);
      assert.deepEqual(error.record, expectation);
      await driver.close();
      return driver;
    };
    // Locked before start, or asleep: classified before policies, helpers or a session.
    device.state = 'KeyguardServiceDelegate\n      showing=true occluded=false\n    mInputRestricted=true\nmWakefulness=Awake';
    await open({}, {classification: 'blocked', reason: 'device-locked', stage: 'open'});
    assert.equal(sessions, 0);
    assert(!commands.some(a => a[2] === 'settings' && a[3] === 'put'), 'no policy changed on a locked device');
    device.state = 'showing=false\nmWakefulness=Asleep';
    await open({}, {classification: 'blocked', reason: 'screen-off', stage: 'open'});
    // A phone that cannot be read before the test is unknown, not an open failure.
    failReads = true;
    await open({}, {classification: 'unknown', reason: 'device-state-unknown', stage: 'open'});
    assert.equal(sessions, 0);
    failReads = false;
    // An unrecognized build is unknown, which does not block the test.
    device.state = 'mWakefulness=Awake';
    let driver = await open({});
    assert.equal(sessions, 1);
    await driver.close();
    device.state = 'showing=false\nmInputRestricted=false\nmWakefulness=Awake';
    // State a previous test left: an extra app window, foreign focus or an open keyboard.
    device.windows = [appWindow, appWindow.replace('4d5e6f', '7a8b9c').replace('#1', '#2'), focus].join('\n');
    await open({}, {classification: 'dirty', reason: 'extra-window', stage: 'open'});
    assert.equal(backPresses, 0, 'no recovery without a declaration');
    await open({baseline: {windows: 2}});
    await opened.at(-1).close();
    device.windows = [appWindow, '  mCurrentFocus=Window{aa u0 com.android.systemui/com.android.systemui.Dialog}'].join('\n');
    await open({}, {classification: 'dirty', reason: 'foreign-focus', stage: 'open'});
    device.windows = [appWindow, focus].join('\n');
    device.ime = 'mInputShown=true';
    await open({}, {classification: 'dirty', reason: 'ime-showing', stage: 'open'});
    // A declared Back recovery is bounded and recorded; the test runs only from the restored baseline.
    let remaining = 2;
    onBack = () => { if (--remaining === 0) device.ime = 'mInputShown=false'; };
    driver = await open({recovery: {back: 3}});
    assert.equal(backPresses, 2);
    assert.deepEqual(driver.setup(), {classification: 'dirty', reason: 'ime-showing', stage: 'open', recovery: {method: 'back', presses: 2, restored: true}});
    assert.equal(validAndroidRuntime(driver.runtime()), true);
    // Rechecks read the keyguard after a completed assertion failure.
    assert.equal(await driver.recheck(context()), undefined);
    device.state = 'showing=true\nmInputRestricted=true';
    assert.deepEqual(await driver.recheck(context()), {classification: 'blocked', reason: 'device-locked', stage: 'recheck'});
    device.state = '';
    assert.deepEqual(await driver.recheck(context()), {classification: 'unknown', reason: 'device-state-unknown', stage: 'recheck'});
    device.state = 'showing=false\nmWakefulness=Awake';
    await driver.close();
    device.ime = 'mInputShown=true';
    onBack = () => {};
    await open({recovery: {back: 3}}, {classification: 'dirty', reason: 'ime-showing', stage: 'open', recovery: {method: 'back', presses: 3, restored: false}});
    assert.equal(backPresses, 3);
    // A reading the driver cannot interpret after Back is not a restored baseline.
    onBack = () => { device.ime = 'mInputShown=false'; device.windows = [appWindow, '  mCurrentFocus=null'].join('\n'); };
    await open({recovery: {back: 3}}, {classification: 'dirty', reason: 'ime-showing', stage: 'open', recovery: {method: 'back', presses: 1, restored: false}});
    device.windows = [appWindow, focus].join('\n');
    // A baseline read that fails after Back keeps the recovery it performed.
    device.ime = 'mInputShown=true';
    onBack = () => { failWindows = true; };
    await open({recovery: {back: 3}}, {classification: 'dirty', reason: 'ime-showing', stage: 'open', recovery: {method: 'back', presses: 1, restored: false}});
    failWindows = false;
    onBack = () => {};
    // So does a Back command that fails after an earlier one completed.
    failBackAfter = 1;
    await open({recovery: {back: 3}}, {classification: 'dirty', reason: 'ime-showing', stage: 'open', recovery: {method: 'back', presses: 1, restored: false}});
    failBackAfter = Infinity;
    // Before any Back press, an unreadable baseline classifies nothing.
    device.ime = 'mImeWindowVis=0x0';
    driver = await open({recovery: {back: 3}});
    assert.equal(driver.setup(), undefined);
    assert.equal(backPresses, 0);
    // A phone that cannot be read at a recheck is unknown, not silently unlocked.
    failReads = true;
    assert.deepEqual(await driver.recheck(context()), {classification: 'unknown', reason: 'device-state-unknown', stage: 'recheck'});
    failReads = false;
    await driver.close();
    device.ime = 'mInputShown=true';
    assert(!commands.some(a => a.includes('clear') || a.includes('force-stop') && a.includes(base.appPackage) || a.includes('uninstall')), 'recovery never clears, stops or removes the app');
    // A clean baseline records nothing.
    device.ime = 'mInputShown=false';
    driver = await open({recovery: {back: 3}});
    assert.equal(driver.setup(), undefined);
    assert.equal(backPresses, 0);
    await driver.close();
    // Baseline and recovery are attachment-only and bounded.
    for (const options of [{baseline: {windows: 0}}, {baseline: {windows: 17}}, {baseline: {focus: 'app'}}, {recovery: {back: 0}}, {recovery: {back: 17}}, {recovery: {clear: true}}]) {
      assert.throws(() => androidTargetDriver({...base, ...options}), /invalid_configuration/);
    }
    const emulator = {appId: 'phone', sdkRoot: dir, toolsRoot: dir, avd: 'QualityMax_API_35', apk: path.join(dir, 'x.apk'), apkSHA256: 'a'.repeat(64), appPackage: 'dev.ninelives.fixture', activity: '.MainActivity'};
    assert.throws(() => androidTargetDriver({...emulator, baseline: {windows: 1}}), /invalid_configuration/);
    assert.throws(() => androidTargetDriver({...emulator, recovery: {back: 1}}), /invalid_configuration/);
  } finally {
    for (const driver of opened) await driver.close().catch(() => {});
    cp.execFile = original.execFile; cp.spawn = original.spawn; global.fetch = original.fetch;
    if (original.scratch === undefined) delete process.env.NINELIVES_TARGET_SCRATCH_DIR; else process.env.NINELIVES_TARGET_SCRATCH_DIR = original.scratch;
    if (original.tmpdir === undefined) delete process.env.TMPDIR; else process.env.TMPDIR = original.tmpdir;
    await fs.rm(dir, {recursive: true, force: true});
  }
});

// Each added operation maps to one supported Appium command with fixed
// arguments; the keyboard is read back from the device, never assumed.
test('attached phone dispatches back, keyboard, scroll, touch, geometry and lifecycle through supported commands only', async () => {
  const cp = require('node:child_process');
  const {EventEmitter} = require('node:events');
  const fs = require('node:fs/promises');
  const path = require('node:path');
  const os = require('node:os');
  const {randomUUID} = require('node:crypto');
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), '9l-phone-ops-'));
  const original = {execFile: cp.execFile, spawn: cp.spawn, fetch: global.fetch, scratch: process.env.NINELIVES_TARGET_SCRATCH_DIR, tmpdir: process.env.TMPDIR};
  const requests = [];
  let ime = 'mInputShown=false', backs = 0, hideOnBack = true, rectValue = {x: 12.4, y: 160, width: 1080, height: 600};
  const serial = `test-${randomUUID()}`;
  let driver;
  try {
    for (const [name, version] of Object.entries({appium: androidVersions.appium, 'appium-uiautomator2-driver': androidVersions.uiautomator2})) {
      await fs.mkdir(path.join(dir, 'node_modules', name), {recursive: true});
      await fs.writeFile(path.join(dir, 'node_modules', name, 'package.json'), JSON.stringify({version}));
    }
    process.env.NINELIVES_TARGET_SCRATCH_DIR = dir;
    process.env.TMPDIR = dir;
    cp.execFile = (file, args, options, callback) => {
      const command = args.slice(2);
      const script = command[1] === 'sh' ? command[3] : '';
      let result = '';
      if (command[0] === 'get-state') result = 'device';
      else if (command[1] === 'getprop') result = ({'ro.kernel.qemu': '', 'ro.build.version.sdk': '37', 'ro.build.version.release': '17', 'ro.product.cpu.abi': 'arm64-v8a'})[command[2]];
      else if (command[1] === 'pm' && command[2] === 'path') result = 'package:/data/app/test/base.apk';
      else if (script.includes('dumpsys window; dumpsys power')) result = 'showing=false\nmWakefulness=Awake';
      else if (script.includes('dumpsys window windows')) result = '  Window #1 Window{4d5e6f u0 dev.ninelives.fixture/dev.ninelives.fixture.MainActivity}:\n  mCurrentFocus=Window{4d5e6f u0 dev.ninelives.fixture/dev.ninelives.fixture.MainActivity}\n9l-windows-end';
      else if (script.includes('dumpsys input_method')) result = ime;
      else if (script.includes('pm path io.appium.settings') || script.includes('echo present')) result = 'absent';
      else if (command[1] === 'sha256sum') result = `${'a'.repeat(64)}  /data/app/test/base.apk`;
      else if (command[1] === 'settings') result = command[2] === 'get' ? '0' : '';
      queueMicrotask(() => callback(null, result));
      return new EventEmitter();
    };
    cp.spawn = () => {
      const child = new EventEmitter(); child.pid = 123; child.exitCode = null; child.signalCode = null;
      child.kill = signal => { child.signalCode = signal; queueMicrotask(() => child.emit('exit')); return true; };
      return child;
    };
    global.fetch = async (input, init) => {
      const url = String(input), body = init.body ? JSON.parse(init.body) : undefined;
      requests.push({path: url.replace(/^.*\/session\/owned-session/, ''), method: init.method ?? 'GET', body});
      let value = null;
      if (url.endsWith('/status')) value = {ready: true};
      else if (url.endsWith('/session')) value = {sessionId: 'owned-session', capabilities: {'appium:udid': serial}};
      else if (url.endsWith('/elements')) value = [{'element-6066-11e4-a52e-4f735466cecf': 'ref-0'}];
      else if (url.endsWith('/window/rect')) value = {x: 0, y: 0, width: 1080, height: 2340};
      else if (url.endsWith('/rect')) value = rectValue;
      else if (url.endsWith('/back')) { backs++; if (hideOnBack) ime = 'mInputShown=false'; }
      else if (url.endsWith('/execute/sync')) value = true;
      return new Response(JSON.stringify({value}), {status: 200});
    };
    const {androidTargetDriver} = require('../dist/android');
    const context = (ms = 30000) => ({signal: new AbortController().signal, deadlineMs: Date.now() + ms});
    driver = androidTargetDriver({appId: 'phone', sdkRoot: dir, toolsRoot: dir, deviceSerial: serial, apkSHA256: 'a'.repeat(64), appPackage: 'dev.ninelives.fixture', activity: '.MainActivity'});
    await driver.open(context());
    requests.length = 0;
    await driver.back(context());
    assert.deepEqual(requests.at(-1), {path: '/back', method: 'POST', body: {}});
    assert.equal(await driver.keyboardShown(context()), false);
    await driver.hideKeyboard(context());
    assert.equal(backs, 1, 'a hidden keyboard is not dismissed again');
    ime = 'mInputShown=true';
    assert.equal(await driver.keyboardShown(context()), true);
    await driver.hideKeyboard(context());
    assert.equal(backs, 2);
    assert.equal(await driver.keyboardShown(context()), false);
    ime = 'mImeWindowVis=0x0';
    await assert.rejects(driver.keyboardShown(context()), /unavailable_state/);
    ime = 'mInputShown=true'; hideOnBack = false;
    await assert.rejects(driver.hideKeyboard(context(400)), /keyboard_not_hidden/);
    assert(!requests.some(r => r.path.includes('hide_keyboard')), 'no Appium keyboard capability or endpoint');
    const [handle] = await driver.observeTestId('list', context());
    await handle.scroll('down', 0.5, context());
    assert.deepEqual(requests.at(-1), {path: '/execute/sync', method: 'POST', body: {script: 'mobile: scrollGesture', args: [{elementId: 'ref-0', direction: 'down', percent: 0.5}]}});
    assert.deepEqual(await handle.bounds(context()), {x: 12, y: 160, width: 1080, height: 600});
    // A malformed rectangle is never coerced to zeros; a scrolled-out origin is geometry.
    for (const bad of [{x: null, y: null, width: null, height: null}, {x: '12', y: 160, width: 1080, height: 600}, {x: true, y: 0, width: 1, height: 1}, {x: 0, y: 0, width: -1, height: 1}, null]) {
      rectValue = bad;
      await assert.rejects(handle.bounds(context()), /invalid_property/);
    }
    rectValue = {x: -40, y: -200.4, width: 300, height: 80};
    assert.deepEqual(await handle.bounds(context()), {x: -40, y: -200, width: 300, height: 80});
    rectValue = {x: 12.4, y: 160, width: 1080, height: 600};
    assert.deepEqual(await driver.viewport(context()), {width: 1080, height: 2340});
    await driver.tapAt({x: 900, y: 1200}, context());
    const actions = requests.at(-1);
    assert.equal(actions.path, '/actions');
    assert.deepEqual(actions.body.actions[0].parameters, {pointerType: 'touch'});
    assert.deepEqual(actions.body.actions[0].actions.map(a => a.type), ['pointerMove', 'pointerDown', 'pause', 'pointerUp']);
    assert.deepEqual([actions.body.actions[0].actions[0].x, actions.body.actions[0].actions[0].y], [900, 1200]);
    await assert.rejects(driver.tapAt({x: -1, y: 0}, context()), /invalid_point/);
    await driver.terminate(context());
    assert.deepEqual(requests.at(-1).body, {script: 'mobile: terminateApp', args: [{appId: 'dev.ninelives.fixture'}]});
    await driver.activate(context());
    assert.deepEqual(requests.at(-1).body, {script: 'mobile: activateApp', args: [{appId: 'dev.ninelives.fixture'}]});
    assert(!requests.some(r => r.path.includes('/appium/device/') || r.path.includes('reset')), 'no reset, clear or device endpoints');
  } finally {
    await driver?.close();
    cp.execFile = original.execFile; cp.spawn = original.spawn; global.fetch = original.fetch;
    if (original.scratch === undefined) delete process.env.NINELIVES_TARGET_SCRATCH_DIR; else process.env.NINELIVES_TARGET_SCRATCH_DIR = original.scratch;
    if (original.tmpdir === undefined) delete process.env.TMPDIR; else process.env.TMPDIR = original.tmpdir;
    await fs.rm(dir, {recursive: true, force: true});
  }
});

// Opt-in diagnostics read the app's windows before each gesture and after a
// failure, and classify a hidden assertion the window manager contradicts.
test('attached phone diagnostics are metadata only and classify observer staleness', async () => {
  const cp = require('node:child_process');
  const {EventEmitter} = require('node:events');
  const fs = require('node:fs/promises');
  const path = require('node:path');
  const os = require('node:os');
  const {randomUUID} = require('node:crypto');
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), '9l-phone-diag-'));
  const original = {execFile: cp.execFile, spawn: cp.spawn, fetch: global.fetch, scratch: process.env.NINELIVES_TARGET_SCRATCH_DIR, tmpdir: process.env.TMPDIR};
  const activity = '  Window #1 Window{4d5e6f u0 dev.ninelives.fixture/dev.ninelives.fixture.MainActivity}:';
  const dialog = '  Window #2 Window{7a8b9c u0 dev.ninelives.fixture/dev.ninelives.fixture.MainActivity}:';
  const focus = '  mCurrentFocus=Window{4d5e6f u0 dev.ninelives.fixture/dev.ninelives.fixture.MainActivity}';
  let windows = [activity, dialog, focus].join('\n'), windowReads = 0, truncated = false;
  const serial = `test-${randomUUID()}`;
  let driver, plain;
  try {
    for (const [name, version] of Object.entries({appium: androidVersions.appium, 'appium-uiautomator2-driver': androidVersions.uiautomator2})) {
      await fs.mkdir(path.join(dir, 'node_modules', name), {recursive: true});
      await fs.writeFile(path.join(dir, 'node_modules', name, 'package.json'), JSON.stringify({version}));
    }
    process.env.NINELIVES_TARGET_SCRATCH_DIR = dir;
    process.env.TMPDIR = dir;
    cp.execFile = (file, args, options, callback) => {
      const command = args.slice(2);
      const script = command[1] === 'sh' ? command[3] : '';
      let result = '';
      if (command[0] === 'get-state') result = 'device';
      else if (command[1] === 'getprop') result = ({'ro.kernel.qemu': '', 'ro.build.version.sdk': '37', 'ro.build.version.release': '17', 'ro.product.cpu.abi': 'arm64-v8a'})[command[2]];
      else if (command[1] === 'pm' && command[2] === 'path') result = 'package:/data/app/test/base.apk';
      else if (script.includes('dumpsys window; dumpsys power')) result = 'showing=false\nmWakefulness=Awake';
      else if (script.includes('dumpsys window windows')) { windowReads++; result = truncated ? windows : `${windows}\n9l-windows-end`; }
      else if (script.includes('dumpsys input_method')) result = 'mInputShown=false';
      else if (script.includes('pm path io.appium.settings') || script.includes('echo present')) result = 'absent';
      else if (command[1] === 'sha256sum') result = `${'a'.repeat(64)}  /data/app/test/base.apk`;
      else if (command[1] === 'settings') result = command[2] === 'get' ? '0' : '';
      queueMicrotask(() => callback(null, result));
      return new EventEmitter();
    };
    cp.spawn = () => {
      const child = new EventEmitter(); child.pid = 123; child.exitCode = null; child.signalCode = null;
      child.kill = signal => { child.signalCode = signal; queueMicrotask(() => child.emit('exit')); return true; };
      return child;
    };
    global.fetch = async input => {
      const url = String(input);
      let value = null;
      if (url.endsWith('/status')) value = {ready: true};
      else if (url.endsWith('/session')) value = {sessionId: 'owned-session', capabilities: {'appium:udid': serial}};
      else if (url.endsWith('/elements')) value = [{'element-6066-11e4-a52e-4f735466cecf': 'ref-0'}];
      else if (url.endsWith('/window/rect')) value = {x: 0, y: 0, width: 1080, height: 2340};
      else if (url.endsWith('/rect')) value = {x: 0, y: 0, width: 200, height: 80};
      else if (url.endsWith('/displayed')) value = true;
      else if (url.endsWith('/text')) value = '7';
      return new Response(JSON.stringify({value}), {status: 200});
    };
    const {androidTargetDriver} = require('../dist/android');
    const context = () => ({signal: new AbortController().signal, deadlineMs: Date.now() + 30000});
    const base = {appId: 'phone', sdkRoot: dir, toolsRoot: dir, deviceSerial: serial, apkSHA256: 'a'.repeat(64), appPackage: 'dev.ninelives.fixture', activity: '.MainActivity', baseline: {windows: 2}};
    assert.throws(() => androidTargetDriver({...base, diagnostics: {screenshots: true}}), /invalid_configuration/);
    driver = androidTargetDriver({...base, diagnostics: {gestures: true}});
    await driver.open(context());
    const afterOpen = windowReads;
    // The dialog is open (2 windows) when the outside touch is sent; the
    // window manager then drops it while the snapshot still shows the element.
    await driver.tapAt({x: 1000, y: 2300}, context());
    assert.equal(windowReads, afterOpen + 1, 'one window read before the gesture');
    windows = [activity, focus].join('\n');
    assert.deepEqual(await driver.recheck(context(), {operation: 'assert-hidden'}), {classification: 'observer', reason: 'window-gone-element-displayed', stage: 'recheck'});
    assert.equal(await driver.recheck(context(), {operation: 'assert-visible'}), undefined, 'only a hidden assertion can be stale this way');
    const record = await driver.diagnose({operation: 'assert-hidden', reason: 'assertion_failed', elapsedMs: 10000}, context());
    // The hidden assertion's record keeps the touch it follows.
    assert.deepEqual(record, {operation: 'assert-hidden', reason: 'assertion_failed', elapsedMs: 10000, x: 1000, y: 2300, point: true, windowsBefore: 2, windowsAfter: 1, focusBefore: 'app', focusAfter: 'app', viewportWidth: 1080, viewportHeight: 2340});
    const tap = await driver.diagnose({operation: 'tap-at', reason: 'deadline', elapsedMs: 5000, point: {x: 1000, y: 2300}}, context());
    assert.deepEqual([tap.x, tap.y, tap.point], [1000, 2300, true]);
    assert(!JSON.stringify(record).includes('MainActivity') && !JSON.stringify(record).includes('Window{'), 'no window titles or hierarchy');
    // The dialog still open: a real failure, not observer staleness.
    windows = [activity, dialog, focus].join('\n');
    await driver.tapAt({x: 1000, y: 2300}, context());
    assert.equal(await driver.recheck(context(), {operation: 'assert-hidden'}), undefined);
    // A dump cut at its byte bound is unobserved: windows pushed past the cap
    // never look like a window that disappeared.
    windows = [activity, focus].join('\n');
    truncated = true;
    assert.equal(await driver.recheck(context(), {operation: 'assert-hidden'}), undefined);
    assert.equal((await driver.diagnose({operation: 'assert-hidden', reason: 'assertion_failed', elapsedMs: 1}, context())).windowsAfter, -1);
    truncated = false;
    // An unavailable dump after the gesture is unobserved, never zero windows.
    windows = '';
    assert.equal(await driver.recheck(context(), {operation: 'assert-hidden'}), undefined);
    assert.equal((await driver.diagnose({operation: 'assert-hidden', reason: 'assertion_failed', elapsedMs: 1}, context())).windowsAfter, -1);
    // Another operation between the gesture and the hidden assertion disarms the heuristic.
    windows = [activity, dialog, focus].join('\n');
    await driver.tapAt({x: 1000, y: 2300}, context());
    const [field] = await driver.observeTestId('amount', context());
    await field.text(context());
    windows = [activity, focus].join('\n');
    assert.equal(await driver.recheck(context(), {operation: 'assert-hidden'}), undefined);
    // Disarming keeps the pre-gesture observation as diagnostic metadata.
    assert.deepEqual(await driver.diagnose({operation: 'assert-text', reason: 'assertion_failed', elapsedMs: 1}, context()),
      {operation: 'assert-text', reason: 'assertion_failed', elapsedMs: 1, x: 1000, y: 2300, point: true, windowsBefore: 2, windowsAfter: 1, focusBefore: 'app', focusAfter: 'app', viewportWidth: 1080, viewportHeight: 2340});
    // So do keyboard, geometry and viewport reads.
    for (const between of [c => driver.keyboardShown(c), c => field.bounds(c), c => driver.viewport(c)]) {
      windows = [activity, dialog, focus].join('\n');
      await driver.tapAt({x: 1000, y: 2300}, context());
      await between(context());
      windows = [activity, focus].join('\n');
      assert.equal(await driver.recheck(context(), {operation: 'assert-hidden'}), undefined);
    }
    // A visibility read alone keeps the dismissal sequence armed.
    windows = [activity, dialog, focus].join('\n');
    await driver.tapAt({x: 1000, y: 2300}, context());
    await field.visible(context());
    windows = [activity, focus].join('\n');
    assert.equal((await driver.recheck(context(), {operation: 'assert-hidden'}))?.classification, 'observer');
    await driver.close();
    assert.equal((await driver.diagnose({operation: 'tap', reason: 'deadline', elapsedMs: 1}, context())).windowsAfter, -1, 'a closed driver answers with what it knows');
    // Without the opt-in, nothing is read or recorded.
    windowReads = 0;
    plain = androidTargetDriver(base);
    await plain.open(context());
    const reads = windowReads;
    await plain.tapAt({x: 1, y: 1}, context());
    assert.equal(windowReads, reads);
    assert.equal(await plain.diagnose({operation: 'tap-at', reason: 'deadline', elapsedMs: 1}, context()), undefined);
    windows = [activity, focus].join('\n');
    assert.equal(await plain.recheck(context(), {operation: 'assert-hidden'}), undefined);
  } finally {
    await driver?.close(); await plain?.close();
    cp.execFile = original.execFile; cp.spawn = original.spawn; global.fetch = original.fetch;
    if (original.scratch === undefined) delete process.env.NINELIVES_TARGET_SCRATCH_DIR; else process.env.NINELIVES_TARGET_SCRATCH_DIR = original.scratch;
    if (original.tmpdir === undefined) delete process.env.TMPDIR; else process.env.TMPDIR = original.tmpdir;
    await fs.rm(dir, {recursive: true, force: true});
  }
});

// The runtime names the display the session targets and the fold posture, a
// declared posture or width is checked before any application operation, and
// a screen digest names that display and never keeps the image.
test('attached phone records its active display and posture, checks declared requirements and digests screenshots in memory', async () => {
  const cp = require('node:child_process');
  const {EventEmitter} = require('node:events');
  const fs = require('node:fs/promises');
  const path = require('node:path');
  const os = require('node:os');
  const {randomUUID, createHash} = require('node:crypto');
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), '9l-phone-display-'));
  const original = {execFile: cp.execFile, spawn: cp.spawn, fetch: global.fetch, scratch: process.env.NINELIVES_TARGET_SCRATCH_DIR, tmpdir: process.env.TMPDIR};
  const inner = 'DisplayDeviceInfo{"Built-in Screen": uniqueId="local:4619827259835644672", 1812 x 2176, density 374, state ON}';
  const cover = 'DisplayDeviceInfo{"Built-in Screen": uniqueId="local:4619827259835644673", 904 x 2316, density 420, state OFF}';
  const device = {displays: [inner, cover].join('\n'), posture: "  Committed state: DeviceState{identifier=2, name='OPENED'}", truncated: false, holdCapture: false};
  const png = Buffer.concat([Buffer.from('\x89PNG\r\n\x1a\n', 'latin1'), Buffer.from('synthetic image bytes')]);
  const screencaps = [];
  let sessions = 0;
  const serial = `test-${randomUUID()}`;
  const opened = [];
  try {
    for (const [name, version] of Object.entries({appium: androidVersions.appium, 'appium-uiautomator2-driver': androidVersions.uiautomator2})) {
      await fs.mkdir(path.join(dir, 'node_modules', name), {recursive: true});
      await fs.writeFile(path.join(dir, 'node_modules', name, 'package.json'), JSON.stringify({version}));
    }
    process.env.NINELIVES_TARGET_SCRATCH_DIR = dir;
    process.env.TMPDIR = dir;
    cp.execFile = (file, args, options, callback) => {
      const command = args.slice(2);
      const script = command[1] === 'sh' ? command[3] : '';
      let result = '';
      if (command[0] === 'exec-out') {
        screencaps.push(command);
        // A held capture answers only when its signal aborts it.
        if (device.holdCapture) options.signal.addEventListener('abort', () => callback(new Error('aborted')), {once: true});
        else queueMicrotask(() => callback(null, png));
        return new EventEmitter();
      }
      if (command[0] === 'get-state') result = 'device';
      else if (command[1] === 'getprop') result = ({'ro.kernel.qemu': '', 'ro.build.version.sdk': '37', 'ro.build.version.release': '17', 'ro.product.cpu.abi': 'arm64-v8a'})[command[2]];
      else if (command[1] === 'pm' && command[2] === 'path') result = 'package:/data/app/test/base.apk';
      else if (script.includes('dumpsys window; dumpsys power')) result = 'showing=false\nmWakefulness=Awake';
      else if (script.includes('dumpsys window windows')) result = '  Window #1 Window{4d5e6f u0 dev.ninelives.fixture/dev.ninelives.fixture.MainActivity}:\n  mCurrentFocus=Window{4d5e6f u0 dev.ninelives.fixture/dev.ninelives.fixture.MainActivity}\n9l-windows-end';
      else if (script.includes('dumpsys input_method')) result = 'mInputShown=false';
      else if (script.includes('dumpsys display')) result = device.truncated ? device.displays : `${device.displays}\n9l-displays-end`;
      else if (script.includes('dumpsys device_state')) result = device.posture;
      else if (script.includes('pm path io.appium.settings') || script.includes('echo present')) result = 'absent';
      else if (command[1] === 'sha256sum') result = `${'a'.repeat(64)}  /data/app/test/base.apk`;
      else if (command[1] === 'settings') result = command[2] === 'get' ? '0' : '';
      queueMicrotask(() => callback(null, result));
      return new EventEmitter();
    };
    cp.spawn = () => {
      const child = new EventEmitter(); child.pid = 123; child.exitCode = null; child.signalCode = null;
      child.kill = signal => { child.signalCode = signal; queueMicrotask(() => child.emit('exit')); return true; };
      return child;
    };
    global.fetch = async input => {
      const url = String(input);
      let value = null;
      if (url.endsWith('/status')) value = {ready: true};
      else if (url.endsWith('/session')) { sessions++; value = {sessionId: 'owned-session', capabilities: {'appium:udid': serial}}; }
      return new Response(JSON.stringify({value}), {status: 200});
    };
    const {androidTargetDriver, TargetSetupError} = require('../dist/android');
    const context = () => ({signal: new AbortController().signal, deadlineMs: Date.now() + 30000});
    const base = {appId: 'phone', sdkRoot: dir, toolsRoot: dir, deviceSerial: serial, apkSHA256: 'a'.repeat(64), appPackage: 'dev.ninelives.fixture', activity: '.MainActivity'};
    const open = async (options, expectation) => {
      sessions = 0;
      const driver = androidTargetDriver({...base, ...options});
      opened.push(driver);
      let error;
      try { await driver.open(context()); } catch (e) { error = e; }
      if (expectation === undefined) { assert.equal(error, undefined); return driver; }
      assert(error instanceof TargetSetupError, `expected a setup classification, got ${error?.message}`);
      assert.deepEqual(error.record, expectation);
      await driver.close();
    };
    let driver = await open({requires: {posture: 'opened', minWidthDp: 640}});
    const runtime = driver.runtime();
    assert.equal(validAndroidRuntime(runtime), true);
    assert.deepEqual([runtime.display, runtime.displayWidth, runtime.displayHeight, runtime.density, runtime.posture], ['4619827259835644672', '1812', '2176', '374', 'opened']);
    assert.equal(await driver.screenDigest(context()), createHash('sha256').update(png).digest('hex'));
    assert.deepEqual(screencaps.at(-1), ['exec-out', 'screencap', '-p', '-d', '4619827259835644672'], 'the active display is named explicitly');
    await driver.close();
    // 1812 px at 374 dpi is 775 dp: a wider requirement is a mismatch, not an app failure.
    await open({requires: {minWidthDp: 800}}, {classification: 'mismatch', reason: 'display-width', stage: 'open'});
    await open({requires: {posture: 'closed'}}, {classification: 'mismatch', reason: 'posture', stage: 'open'});
    // Rotated to landscape with a density override: the logical layout, not
    // the physical metrics, decides the width requirement and is recorded.
    const landscape = '    mOverrideDisplayInfo=DisplayInfo{"Built-in Screen", displayId 0", real 2176 x 1812, rotation 1, state ON, uniqueId "local:4619827259835644672", app 2176 x 1812, density 400 (400.0 x 400.0) dpi}';
    device.displays = [inner, cover, landscape].join('\n');
    driver = await open({requires: {minWidthDp: 800}});
    assert.deepEqual([driver.runtime().displayWidth, driver.runtime().displayHeight, driver.runtime().density], ['2176', '1812', '400']);
    await driver.close();
    assert.equal(sessions, 1);
    // A mismatch is found before the session that would auto-launch the app.
    await open({requires: {posture: 'closed'}}, {classification: 'mismatch', reason: 'posture', stage: 'open'});
    assert.equal(sessions, 0, 'no session, so no app launch, on a mismatch');
    // Folded: the cover display is the powered one.
    device.displays = [inner.replace('state ON', 'state OFF'), cover.replace('state OFF', 'state ON')].join('\n');
    device.posture = "  Committed state: DeviceState{identifier=0, name='CLOSED'}";
    await open({requires: {minWidthDp: 640}}, {classification: 'mismatch', reason: 'display-width', stage: 'open'});
    driver = await open({});
    assert.deepEqual([driver.runtime().display, driver.runtime().displayWidth, driver.runtime().posture], ['4619827259835644673', '904', 'closed']);
    await driver.close();
    // Unreadable display or posture is recorded as unknown, and a digest then
    // takes the device default without inventing an id.
    device.displays = ''; device.posture = '';
    driver = await open({});
    assert.deepEqual([driver.runtime().display, driver.runtime().displayWidth, driver.runtime().density, driver.runtime().posture], ['unknown', '0', '0', 'unknown']);
    await driver.screenDigest(context());
    assert.deepEqual(screencaps.at(-1), ['exec-out', 'screencap', '-p']);
    await driver.close();
    await open({requires: {minWidthDp: 1}}, {classification: 'mismatch', reason: 'display-width', stage: 'open'});
    // A dump cut at its byte bound names no display, even when what survived
    // holds a single wide one: the powered cover display may be what was cut.
    device.displays = inner.replace('state ON', 'state OFF'); device.truncated = true;
    await open({requires: {minWidthDp: 640}}, {classification: 'mismatch', reason: 'display-width', stage: 'open'});
    driver = await open({});
    assert.equal(driver.runtime().display, 'unknown');
    // Closing the driver cancels a capture still in flight.
    device.truncated = false; device.holdCapture = true;
    const pending = driver.screenDigest(context());
    pending.catch(() => {});
    await driver.close();
    await assert.rejects(Promise.race([pending, new Promise(resolve => setTimeout(resolve, 1000, 'still pending'))]), /screenshot_failed/);
    device.holdCapture = false;
    for (const requires of [{}, {posture: 'OPENED'}, {minWidthDp: 0}, {minWidthDp: 1.5}, {width: 640}]) {
      assert.throws(() => androidTargetDriver({...base, requires}), /invalid_configuration/);
    }
  } finally {
    for (const driver of opened) await driver.close().catch(() => {});
    cp.execFile = original.execFile; cp.spawn = original.spawn; global.fetch = original.fetch;
    if (original.scratch === undefined) delete process.env.NINELIVES_TARGET_SCRATCH_DIR; else process.env.NINELIVES_TARGET_SCRATCH_DIR = original.scratch;
    if (original.tmpdir === undefined) delete process.env.TMPDIR; else process.env.TMPDIR = original.tmpdir;
    await fs.rm(dir, {recursive: true, force: true});
  }
});

// A declared artifact, version or signer that differs from the installed
// build is a setup mismatch before any session; the signer is read by pulling
// the installed APK only when declared, and the in-place installer refuses a
// different signer before touching the device.
test('attached phone verifies the installed build against declared identity and installInPlace refuses a foreign signer', async () => {
  const cp = require('node:child_process');
  const {EventEmitter} = require('node:events');
  const fs = require('node:fs/promises');
  const path = require('node:path');
  const os = require('node:os');
  const {randomUUID, createHash} = require('node:crypto');
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), '9l-phone-build-'));
  const original = {execFile: cp.execFile, spawn: cp.spawn, fetch: global.fetch, scratch: process.env.NINELIVES_TARGET_SCRATCH_DIR, tmpdir: process.env.TMPDIR};
  const artifact = path.join(dir, 'build-28.apk');
  await fs.writeFile(artifact, 'synthetic artifact bytes');
  const artifactDigest = createHash('sha256').update('synthetic artifact bytes').digest('hex');
  const device = {digest: artifactDigest, version: '    versionCode=28 minSdk=24 targetSdk=35\n    versionName=1.4.0', installedSigner: 'a1'.repeat(32), installed: true};
  const commands = [];
  let sessions = 0, onInspect = () => {}, installedBytes;
  const serial = `test-${randomUUID()}`;
  const opened = [];
  try {
    for (const [name, version] of Object.entries({appium: androidVersions.appium, 'appium-uiautomator2-driver': androidVersions.uiautomator2})) {
      await fs.mkdir(path.join(dir, 'node_modules', name), {recursive: true});
      await fs.writeFile(path.join(dir, 'node_modules', name, 'package.json'), JSON.stringify({version}));
    }
    process.env.NINELIVES_TARGET_SCRATCH_DIR = dir;
    process.env.TMPDIR = dir;
    cp.execFile = (file, args, options, callback) => {
      commands.push([path.basename(file), ...args]);
      if (path.basename(file) === 'apksigner') {
        const target = args[2];
        const signer = target.endsWith('installed.apk') ? device.installedSigner : require('node:fs').readFileSync(target, 'utf8') === 'synthetic artifact bytes' ? 'a1'.repeat(32) : 'b2'.repeat(32);
        queueMicrotask(() => callback(null, `Signer #1 certificate DN: CN=9l\nSigner #1 certificate SHA-256 digest: ${signer}\n`));
        return new EventEmitter();
      }
      if (path.basename(file) === 'aapt2') { onInspect(); queueMicrotask(() => callback(null, "package: name='dev.ninelives.fixture' versionCode='28' versionName='1.4.0'")); return new EventEmitter(); }
      const command = args.slice(2);
      const script = command[1] === 'sh' ? command[3] : '';
      let result = '';
      if (command[0] === 'get-state') result = 'device';
      else if (command[0] === 'pull') { require('node:fs').writeFileSync(command[2], 'synthetic artifact bytes'); result = ''; }
      else if (command[1] === 'getprop') result = ({'ro.kernel.qemu': '', 'ro.build.version.sdk': '37', 'ro.build.version.release': '17', 'ro.product.cpu.abi': 'arm64-v8a'})[command[2]];
      else if (command[1] === 'pm' && command[2] === 'path') { if (!device.installed) { queueMicrotask(() => callback(new Error('exit 1'))); return new EventEmitter(); } result = 'package:/data/app/test/base.apk'; }
      else if (script.includes('pm path dev.ninelives.fixture')) result = device.installed ? 'package:/data/app/test/base.apk' : '';
      else if (command[0] === 'install') { device.installed = true; installedBytes = require('node:fs').readFileSync(command.at(-1), 'utf8'); result = 'Success'; }
      else if (script.includes('dumpsys window; dumpsys power')) result = 'showing=false\nmWakefulness=Awake';
      else if (script.includes('dumpsys window windows')) result = '  Window #1 Window{4d5e6f u0 dev.ninelives.fixture/dev.ninelives.fixture.MainActivity}:\n  mCurrentFocus=Window{4d5e6f u0 dev.ninelives.fixture/dev.ninelives.fixture.MainActivity}';
      else if (script.includes('dumpsys input_method')) result = 'mInputShown=false';
      else if (script.includes('dumpsys package')) result = device.version;
      else if (script.includes('pm path io.appium.settings') || script.includes('echo present')) result = 'absent';
      else if (command[1] === 'sha256sum') result = `${device.digest}  /data/app/test/base.apk`;
      else if (command[1] === 'settings') result = command[2] === 'get' ? '0' : '';
      queueMicrotask(() => callback(null, result));
      return new EventEmitter();
    };
    cp.spawn = () => {
      const child = new EventEmitter(); child.pid = 123; child.exitCode = null; child.signalCode = null;
      child.kill = signal => { child.signalCode = signal; queueMicrotask(() => child.emit('exit')); return true; };
      return child;
    };
    global.fetch = async input => {
      const url = String(input);
      let value = null;
      if (url.endsWith('/status')) value = {ready: true};
      else if (url.endsWith('/session')) { sessions++; value = {sessionId: 'owned-session', capabilities: {'appium:udid': serial}}; }
      return new Response(JSON.stringify({value}), {status: 200});
    };
    const {androidTargetDriver, TargetSetupError, installInPlace, androidLeasePath} = require('../dist/android');
    const context = () => ({signal: new AbortController().signal, deadlineMs: Date.now() + 30000});
    const base = {appId: 'phone', sdkRoot: dir, toolsRoot: dir, deviceSerial: serial, appPackage: 'dev.ninelives.fixture', activity: '.MainActivity'};
    const open = async (options, expectation) => {
      commands.length = 0; sessions = 0;
      const driver = androidTargetDriver({...base, ...options});
      opened.push(driver);
      let error;
      try { await driver.open(context()); } catch (e) { error = e; }
      if (expectation === undefined) { assert.equal(error, undefined, error?.message); return driver; }
      if (expectation instanceof RegExp) { assert.match(String(error?.message), expectation); await driver.close(); return; }
      assert(error instanceof TargetSetupError, `expected a setup classification, got ${error?.message}`);
      assert.deepEqual(error.record, expectation);
      assert.equal(sessions, 0, 'no session on a wrong build');
      await driver.close();
    };
    // The declared artifact replaces apkSHA256; versions and signer are recorded.
    let driver = await open({expected: {apk: artifact, versionCode: '28', versionName: '1.4.0', signerSHA256: 'a1'.repeat(32)}});
    assert.equal(validAndroidRuntime(driver.runtime()), true);
    assert.deepEqual([driver.runtime().apkSHA256, driver.runtime().versionCode, driver.runtime().versionName, driver.runtime().signer], [artifactDigest, '28', '1.4.0', 'a1'.repeat(32)]);
    assert(commands.some(c => c[0] === 'adb' && c[3] === 'pull'), 'the installed APK is pulled for the declared signer');
    assert.equal(await fs.access(path.join(dir, 'installed.apk')).then(() => true, () => false), false, 'the pulled APK does not stay in the scratch root');
    await driver.close();
    // Without a declared signer nothing is pulled and the signer is unknown.
    driver = await open({apkSHA256: artifactDigest});
    assert.equal(driver.runtime().signer, 'unknown');
    assert(!commands.some(c => c[3] === 'pull' || c[0] === 'apksigner'));
    await driver.close();
    // EAS auto-incremented to 29: a stale declaration is a mismatch, not an app failure.
    device.version = '    versionCode=29 minSdk=24 targetSdk=35\n    versionName=1.4.0';
    await open({apkSHA256: artifactDigest, expected: {versionCode: '28'}}, {classification: 'mismatch', reason: 'build-version', stage: 'open'});
    await open({apkSHA256: artifactDigest, expected: {versionName: '1.5.0'}}, {classification: 'mismatch', reason: 'build-version', stage: 'open'});
    device.installedSigner = 'c3'.repeat(32);
    await open({apkSHA256: artifactDigest, expected: {signerSHA256: 'a1'.repeat(32)}}, {classification: 'mismatch', reason: 'build-signer', stage: 'open'});
    device.digest = 'd4'.repeat(32);
    await open({expected: {apk: artifact}}, {classification: 'mismatch', reason: 'build-digest', stage: 'open'});
    await open({apkSHA256: 'e5'.repeat(32), expected: {apk: artifact}}, /open_failed_configuration/);
    for (const options of [{}, {expected: {}}, {apkSHA256: artifactDigest, expected: {versionCode: 'v28'}}, {apkSHA256: artifactDigest, expected: {signerSHA256: 'CN=9l'}}, {apkSHA256: artifactDigest, expected: {clearData: true}}]) {
      assert.throws(() => androidTargetDriver({...base, ...options}), /invalid_configuration/);
    }
    // installInPlace: a foreign signer refuses before install; a matching one installs with -r only.
    device.digest = artifactDigest; device.installedSigner = 'c3'.repeat(32);
    commands.length = 0;
    await assert.rejects(installInPlace({sdkRoot: dir, deviceSerial: serial, apk: artifact}), /signer_mismatch/);
    assert(!commands.some(c => c.includes('install') || c.includes('uninstall') || c.includes('clear')), 'nothing changed on a signer mismatch');
    device.installedSigner = 'a1'.repeat(32);
    commands.length = 0;
    assert.deepEqual(await installInPlace({sdkRoot: dir, deviceSerial: serial, apk: artifact}), {appPackage: 'dev.ninelives.fixture', apkSHA256: artifactDigest, signer: 'a1'.repeat(32), versionCode: '29', versionName: '1.4.0'});
    const install = commands.find(c => c.includes('install'));
    assert.deepEqual(install.slice(3, 6), ['install', '--no-streaming', '-r']);
    assert(install[6] !== artifact && path.basename(path.dirname(install[6])).startsWith('9l-install-'), 'the private snapshot is installed, not the caller\'s path');
    assert.equal(await fs.access(install[6]).then(() => true, () => false), false, 'the snapshot is removed');
    assert(!commands.some(c => c.includes('uninstall') || c.includes('clear')));
    // A build replacing the caller's file after it was read changes nothing:
    // the inspected, verified and installed bytes are the snapshot's.
    onInspect = () => { require('node:fs').writeFileSync(artifact, 'replaced by a concurrent build'); };
    assert.deepEqual(await installInPlace({sdkRoot: dir, deviceSerial: serial, apk: artifact}), {appPackage: 'dev.ninelives.fixture', apkSHA256: artifactDigest, signer: 'a1'.repeat(32), versionCode: '29', versionName: '1.4.0'});
    assert.equal(installedBytes, 'synthetic artifact bytes');
    onInspect = () => {};
    await fs.writeFile(artifact, 'synthetic artifact bytes');
    assert.equal(await fs.access(androidLeasePath(serial)).then(() => true, () => false), false, 'the installer releases its lease');
    // A first installation: no package yet, nothing to pull or compare, install proceeds.
    device.installed = false;
    commands.length = 0;
    assert.deepEqual(await installInPlace({sdkRoot: dir, deviceSerial: serial, apk: artifact}), {appPackage: 'dev.ninelives.fixture', apkSHA256: artifactDigest, signer: 'a1'.repeat(32), versionCode: '29', versionName: '1.4.0'});
    assert(!commands.some(c => c[3] === 'pull'), 'first install pulls nothing');
    assert(commands.some(c => c.includes('install')));
    // The device lease a running test holds blocks the installer before any device command.
    const held = androidTargetDriver({...base, apkSHA256: artifactDigest});
    opened.push(held);
    await held.open(context());
    commands.length = 0;
    await assert.rejects(installInPlace({sdkRoot: dir, deviceSerial: serial, apk: artifact}), /device_in_use/);
    assert.equal(commands.filter(c => c[0] === 'adb').length, 0, 'no adb command while the device is leased');
    await held.close();
    assert.equal(await fs.access(androidLeasePath(serial)).then(() => true, () => false), false, 'the installer never removes a lease it did not create');
    await assert.rejects(installInPlace({sdkRoot: dir, deviceSerial: 'emulator-5554', apk: artifact}), /invalid_configuration/);
    // Omitted roots come from the discovery doctor reports; explicit ones win.
    const discovered = path.join(dir, 'discovered-sdk'), files = [], mock = cp.execFile;
    const saved = {home: process.env.ANDROID_HOME, root: process.env.ANDROID_SDK_ROOT, tools: process.env.NINELIVES_ANDROID_TOOLS};
    cp.execFile = (file, ...rest) => { files.push(file); return mock(file, ...rest); };
    process.env.ANDROID_HOME = discovered; delete process.env.ANDROID_SDK_ROOT; process.env.NINELIVES_ANDROID_TOOLS = dir;
    try {
      const {sdkRoot, toolsRoot, ...portable} = base;
      const located = androidTargetDriver({...portable, apkSHA256: artifactDigest});
      opened.push(located);
      await located.open(context());
      await located.close();
      assert(files.length > 0 && files.every(file => file.startsWith(`${discovered}${path.sep}`)), 'the driver runs the discovered SDK tools');
      files.length = 0;
      assert.equal((await installInPlace({deviceSerial: serial, apk: artifact})).apkSHA256, artifactDigest);
      assert(files.length > 0 && files.every(file => file.startsWith(`${discovered}${path.sep}`)), 'the installer runs the discovered SDK tools');
      files.length = 0;
      await installInPlace({sdkRoot: dir, deviceSerial: serial, apk: artifact});
      assert(files.length > 0 && files.every(file => !file.startsWith(discovered)), 'an explicit sdkRoot wins over discovery');
    } finally {
      cp.execFile = mock;
      for (const [key, value] of [['ANDROID_HOME', saved.home], ['ANDROID_SDK_ROOT', saved.root], ['NINELIVES_ANDROID_TOOLS', saved.tools]]) {
        if (value === undefined) delete process.env[key]; else process.env[key] = value;
      }
    }
  } finally {
    for (const driver of opened) await driver.close().catch(() => {});
    cp.execFile = original.execFile; cp.spawn = original.spawn; global.fetch = original.fetch;
    if (original.scratch === undefined) delete process.env.NINELIVES_TARGET_SCRATCH_DIR; else process.env.NINELIVES_TARGET_SCRATCH_DIR = original.scratch;
    if (original.tmpdir === undefined) delete process.env.TMPDIR; else process.env.TMPDIR = original.tmpdir;
    await fs.rm(dir, {recursive: true, force: true});
  }
});
