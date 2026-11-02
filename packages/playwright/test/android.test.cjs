const {test} = require('node:test');
const assert = require('node:assert/strict');
const {validAndroidRuntime, androidVersions} = require('../dist/android-runtime');

test('native metadata rejects unknown fields, content, missing identity and unsupported versions', () => {
  const runtime = {appPackage:'dev.ninelives.fixture',apkSHA256:'a'.repeat(64),api:'35',release:'15',abi:'arm64-v8a',emulator:'36.4.9',...androidVersions,worker:'b'.repeat(64)};
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
});

test('physical metadata is distinct from emulator identity and excludes raw serials', () => {
  const runtime = {appPackage:'dev.ninelives.fixture',apkSHA256:'a'.repeat(64),api:'37',release:'17',abi:'arm64-v8a',device:'physical',deviceHash:'c'.repeat(64),...androidVersions,worker:'b'.repeat(64)};
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
    for (const [hash,qemu] of [['b'.repeat(64),''],['a'.repeat(64),'1']]) {
      installedHash=hash; physicalQemu=qemu;
      const before=requests.filter(r=>r.url.endsWith('/session')).length;
      const bad=androidTargetDriver(options);
      await assert.rejects(bad.open(context()),/open_failed_device_attach/);
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
  let commands = [], sessions = 0, backPresses = 0, onBack = () => {}, failReads = false;
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
      else if (script.includes('dumpsys window windows')) result = device.windows;
      else if (script.includes('dumpsys input_method')) result = device.ime;
      else if (script.includes('pm path io.appium.settings')) result = 'absent';
      else if (script.includes('echo present')) result = 'absent';
      else if (command[1] === 'input' && command[3] === 'KEYCODE_BACK') { backPresses++; onBack(); }
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
    onBack = () => {};
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
  let ime = 'mInputShown=false', backs = 0, hideOnBack = true;
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
      else if (script.includes('dumpsys window windows')) result = '  Window #1 Window{4d5e6f u0 dev.ninelives.fixture/dev.ninelives.fixture.MainActivity}:\n  mCurrentFocus=Window{4d5e6f u0 dev.ninelives.fixture/dev.ninelives.fixture.MainActivity}';
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
      else if (url.endsWith('/rect')) value = {x: 12.4, y: 160, width: 1080, height: 600};
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
