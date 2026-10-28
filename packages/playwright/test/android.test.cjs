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
  const original = {execFile: cp.execFile, spawn: cp.spawn, fetch: global.fetch, scratch:process.env.NINELIVES_TARGET_SCRATCH_DIR};
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
    await fs.rm(dir,{recursive:true,force:true});
  }
});
