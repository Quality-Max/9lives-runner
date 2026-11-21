const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const {validIOSRuntime, iosVersions} = require('../dist/ios-runtime');
const runtime = {bundleId:'dev.ninelives.fixture',appSHA256:'a'.repeat(64),version:'1.0',build:'1',runtime:'com.apple.CoreSimulator.SimRuntime.iOS-18-5',deviceType:'com.apple.CoreSimulator.SimDeviceType.iPhone-16',os:'18.5',architecture:'arm64',xcode:'16.4',...iosVersions,worker:'b'.repeat(64),viewportWidth:'390',viewportHeight:'844'};

test('iOS metadata is closed, pinned, content-free and runtime-bound', () => {
  assert.equal(validIOSRuntime(runtime), true);
  for (const key of Object.keys(runtime)) {
    const missing = {...runtime}; delete missing[key];
    assert.equal(validIOSRuntime(missing), false, key);
    assert.equal(validIOSRuntime({...runtime,[key]:'private screen content'}), false, key);
  }
  for (const changes of [{os:'18.4'}, {screen:'private'}, {appium:'3.8.1'}, {xcuitest:'10.43.2'}, {viewportWidth:'0'}]) assert.equal(validIOSRuntime({...runtime,...changes}), false);
});

test('iOS metadata accepts real app version and build numbers', () => {
  for (const value of ['1234', '20261121', '2026.11.0', '1.2.3.4', '4294967295'])
    assert.equal(validIOSRuntime({...runtime, version:value, build:value}), true, value);
  for (const value of ['', '1.2.3.4.5', '12345678901', '1a', '1..2', '1.0 (42)'])
    assert.equal(validIOSRuntime({...runtime, build:value}), false, value);
  assert.equal(validIOSRuntime({...runtime, os:'1800.5', runtime:'com.apple.CoreSimulator.SimRuntime.iOS-1800-5'}), false);
});

test('iOS tree digest binds names and contents', () => {
  const {iosAppDigest} = require('../dist/ios');
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), '9l-ios-digest-'));
  try {
    fs.writeFileSync(path.join(dir,'Info.plist'),'fixture');
    fs.writeFileSync(path.join(dir,'app'),'one');
    const original = iosAppDigest(dir);
    fs.writeFileSync(path.join(dir,'app'),'two'); assert.notEqual(iosAppDigest(dir),original);
    fs.writeFileSync(path.join(dir,'app'),'one'); assert.equal(iosAppDigest(dir),original);
    fs.renameSync(path.join(dir,'app'),path.join(dir,'renamed')); assert.notEqual(iosAppDigest(dir),original);
  } finally { fs.rmSync(dir,{recursive:true,force:true}); }
});

// Windows chmod cannot set executable bits. Qualify the real Simulator app
// filesystem contract on Unix, while keeping names/content coverage everywhere.
test('iOS tree digest binds Unix executable modes and refuses links', {skip: process.platform === 'win32' ? 'requires Unix filesystem permissions' : false}, () => {
  const {iosAppDigest} = require('../dist/ios');
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), '9l-ios-mode-'));
  try {
    fs.writeFileSync(path.join(dir,'Info.plist'),'fixture');
    fs.writeFileSync(path.join(dir,'app'),'one',{mode:0o644});
    const original = iosAppDigest(dir);
    fs.chmodSync(path.join(dir,'app'),0o755); assert.notEqual(iosAppDigest(dir),original);
    fs.symlinkSync(path.join(dir,'app'),path.join(dir,'alias')); assert.throws(()=>iosAppDigest(dir),/unsupported_app_tree/);
  } finally { fs.rmSync(dir,{recursive:true,force:true}); }
});

// Synthetic transport coverage only. Real XCTest behavior is qualified by
// scripts/ios-smoke.cjs on macOS, including a deliberately broken native app.
test('owned iOS transport checks build/session identity, rejects ambiguous actions and cleans cancellation', async () => {
  const cp = require('node:child_process');
  const {EventEmitter} = require('node:events');
  const dir = fs.mkdtempSync(path.join(os.tmpdir(),'9l-ios-transport-'));
  const original = {execFile:cp.execFile,spawn:cp.spawn,fetch:global.fetch,kill:process.kill,platform:Object.getOwnPropertyDescriptor(process,'platform'),scratch:process.env.NINELIVES_TARGET_SCRATCH_DIR};
  const commands=[], requests=[], kills=[];
  let owned, staged, elements=1, hold=false, clicks=0, foreign=false, architectures='arm64 x86_64', shutdownFails=false, deleteFails=false, inventoryFails=false, createOutput, wdaBundle='com.facebook.WebDriverAgentRunner.xctrunner';
  const udid='12345678-1234-1234-1234-123456789abc';
  const c=()=>({signal:new AbortController().signal,deadlineMs:Date.now()+10000});
  try {
    Object.defineProperty(process,'platform',{value:'darwin'});
    process.env.NINELIVES_TARGET_SCRATCH_DIR=dir;
    for (const [name,version] of [['appium',iosVersions.appium],['appium-xcuitest-driver',iosVersions.xcuitest]]) {
      fs.mkdirSync(path.join(dir,'node_modules',name),{recursive:true});
      fs.writeFileSync(path.join(dir,'node_modules',name,'package.json'),JSON.stringify({version}));
    }
    const source=path.join(dir,'Fixture.app');fs.mkdirSync(source);fs.writeFileSync(path.join(source,'Info.plist'),'fixture');
    const wdaProducts=path.join(dir,'Products'),wdaRunner='Debug-iphonesimulator/WebDriverAgentRunner-Runner.app';
    fs.mkdirSync(path.join(wdaProducts,wdaRunner),{recursive:true});fs.writeFileSync(path.join(wdaProducts,wdaRunner,'Info.plist'),'runner');
    fs.writeFileSync(path.join(wdaProducts,'WebDriverAgentRunner.xctestrun'),'run');
    cp.execFile=(file,args,opts,callback)=>{
      commands.push(args);
      let output='';
      if (file.endsWith('PlistBuddy')) output=({CFBundleIdentifier:args[2]===path.join(wdaProducts,wdaRunner,'Info.plist')?wdaBundle:runtime.bundleId,DTPlatformName:'iphonesimulator',CFBundleShortVersionString:'1.0',CFBundleVersion:'1'})[args[1].slice(7)];
      else if (file.endsWith('xcodebuild')) output='Xcode 16.4\nBuild version synthetic';
      else if (args[1]==='list'&&args[2]==='runtimes') output=JSON.stringify({runtimes:[{identifier:runtime.runtime,isAvailable:true,version:'18.5'}]});
      else if (args[1]==='create') { owned={name:args[2],udid,deviceTypeIdentifier:runtime.deviceType,state:'Shutdown'};output=createOutput??udid; }
      else if (args[1]==='list'&&args[2]==='devices') {
        assert.ok(opts.timeout<=30000);
        if(inventoryFails) {queueMicrotask(()=>callback(Error('synthetic inventory failure')));return new EventEmitter();}
        output=JSON.stringify({devices:{[runtime.runtime]:owned?[owned]:[]}});
      }
      else if (args[1]==='shutdown' && shutdownFails) {
        assert.equal(args[2],udid);assert.ok(opts.timeout<=15000);
        queueMicrotask(()=>callback(Error('synthetic shutdown failure')));return new EventEmitter();
      }
      else if (args[1]==='delete') {
        assert.equal(args[2],udid);assert.ok(opts.timeout<=30000);
        if(deleteFails) {queueMicrotask(()=>callback(Error('synthetic deletion failure')));return new EventEmitter();}
        owned=undefined;
      }
      else if (args[1]==='boot') owned.state='Booted';
      else if (args[1]==='getenv') {assert.deepEqual(args,['simctl','getenv',udid,'SIMULATOR_ARCHS']);output=architectures;}
      else if (args[1]==='spawn') throw Error('Simulator runtime has no uname executable');
      else if (args[1]==='install') staged=args[3];
      else if (args[1]==='get_app_container') output=staged;
      queueMicrotask(()=>callback(null,output));
      return new EventEmitter();
    };
    cp.spawn=()=>Object.assign(new EventEmitter(),{pid:2147483647,exitCode:null,signalCode:null});
    process.kill=(pid,signal)=>{assert.equal(pid,-2147483647);kills.push(signal);return true;};
    global.fetch=async (url,init={})=>{
      const pathname=new URL(url).pathname; const body=init.body?JSON.parse(init.body):undefined;
      requests.push({pathname,body}); let value=null;
      if(pathname.endsWith('/status')) value={ready:true};
      else if(pathname.endsWith('/session')) value={sessionId:'session-1',capabilities:{udid:foreign?'foreign':udid,platformName:'iOS'}};
      else if(pathname.endsWith('/window/rect')) value={x:0,y:0,width:390,height:844};
      else if(pathname.endsWith('/window/size')) return new Response(JSON.stringify({value:{error:'unknown command'}}),{status:404});
      else if(pathname.endsWith('/elements')) {
        assert.match(body.value,/^name == "[a-z]+" AND label != "[a-z]+"$/);
        value=Array.from({length:elements},(_,i)=>({'element-6066-11e4-a52e-4f735466cecf':`element-${i}`}));
      } else if(pathname.endsWith('/click')) {
        clicks++;
        if(hold) return new Promise((_,reject)=>init.signal.addEventListener('abort',()=>reject(Error('private driver payload')),{once:true}));
      } else if(pathname.endsWith('/displayed')||pathname.endsWith('/enabled')) value=true;
      else if(pathname.endsWith('/text')) value='Sent $5';
      return new Response(JSON.stringify({value}),{status:200});
    };
    delete require.cache[require.resolve('../dist/ios')];
    const {iosTargetDriver,iosAppDigest}=require('../dist/ios');
    const opts={appId:'finance',app:source,appSHA256:iosAppDigest(source),bundleId:runtime.bundleId,runtime:runtime.runtime,deviceType:runtime.deviceType,toolsRoot:dir};
    // The engine removes the device from this record if the worker is stopped
    // before close. It exists exactly while the device may exist.
    const records=()=>fs.readdirSync(dir).filter(name=>name.endsWith('.owned.json'));
    let driver=iosTargetDriver(opts);
    await driver.open(c());assert.equal(validIOSRuntime(driver.runtime()),true);assert.equal(driver.runtime().architecture,'arm64');
    assert.equal(records().length,1);assert.match(records()[0],/^9l-ios-[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}\.owned\.json$/);
    assert.deepEqual(JSON.parse(fs.readFileSync(path.join(dir,records()[0]),'utf8')),
      {version:1,name:records()[0].replace('.owned.json',''),runtime:runtime.runtime,deviceType:runtime.deviceType});
    assert.equal(owned.name,records()[0].replace('.owned.json',''));
    // The test presents itself as macOS; the real host decides whether modes exist.
    if(original.platform.value!=='win32') assert.equal(fs.statSync(path.join(dir,records()[0])).mode&0o777,0o600);
    await driver.reset(c());
    const [handle]=await driver.observeTestId('send',c());
    elements=2;await assert.rejects(handle.tap(c()),/ambiguous_control/);assert.equal(clicks,0);
    elements=0;await assert.rejects(handle.tap(c()),/missing_control/);assert.equal(clicks,0);
    elements=1;assert.equal(await handle.text(c()),'Sent $5');
    hold=true;
    const pending=assert.rejects(handle.tap(c()),/transport_or_driver_failed/);
    while(!clicks) await new Promise(resolve=>setImmediate(resolve));
    await driver.close();await pending;
    assert.equal(clicks,1);assert.equal(owned,undefined);assert.deepEqual(kills,['SIGTERM','SIGKILL']);
    assert.deepEqual(records(),[]);
    assert.equal(commands.some(args=>args.includes('all')),false);
    assert.equal(fs.existsSync(path.dirname(staged)),false);
    driver=iosTargetDriver({...opts,appSHA256:'0'.repeat(64)});
    await assert.rejects(driver.open(c()),e=>e.record?.classification==='mismatch'&&e.record.reason==='build-digest');
    assert.deepEqual(records(),[]);await driver.close();
    architectures='x86_64';driver=iosTargetDriver(opts);
    await driver.open(c());assert.equal(driver.runtime().architecture,'x86_64');await driver.close();
    for (architectures of ['', 'unknown', 'arm64 unknown']) {
      const sessions=requests.filter(r=>r.pathname.endsWith('/session')).length;
      driver=iosTargetDriver(opts);
      await assert.rejects(driver.open(c()),e=>e.record?.classification==='unknown'&&e.cause?.message==='9lives iOS: open_architecture');
      await driver.close();assert.equal(owned,undefined);
      assert.equal(requests.filter(r=>r.pathname.endsWith('/session')).length,sessions);
    }
    architectures='arm64';
    foreign=true;driver=iosTargetDriver(opts);
    await assert.rejects(driver.open(c()),e=>e.record?.classification==='unknown'&&e.cause?.message==='9lives iOS: open_session');await driver.close();assert.equal(owned,undefined);
    foreign=false;shutdownFails=true;driver=iosTargetDriver(opts);
    await driver.open(c());await driver.close();assert.equal(owned,undefined);
    deleteFails=true;driver=iosTargetDriver(opts);await driver.open(c());
    await assert.rejects(driver.close(),/cleanup_failed/);assert.equal(owned.udid,udid);
    assert.equal(fs.existsSync(path.dirname(staged)),false);
    // A device this worker could not remove stays recorded for the engine.
    assert.deepEqual(records(),[`${owned.name}.owned.json`]);fs.rmSync(path.join(dir,records()[0]));
    // An inventory that cannot answer must not cost deletion its turn: the
    // device goes, and only the unconfirmed absence fails cleanup.
    owned=undefined;deleteFails=false;shutdownFails=false;driver=iosTargetDriver(opts);await driver.open(c());
    inventoryFails=true;commands.length=0;
    await assert.rejects(driver.close(),/cleanup_failed/);assert.equal(owned,undefined);
    assert.deepEqual(commands.map(args=>args[1]),['shutdown','delete','list']);
    assert.equal(records().length,1,'unconfirmed absence stays recorded');fs.rmSync(path.join(dir,records()[0]));
    inventoryFails=false;
    // A create that registers a device but loses its output is recovered by
    // the owned name; a malformed identifier is never passed to simctl.
    createOutput='not-a-device';driver=iosTargetDriver(opts);
    await assert.rejects(driver.open(c()),e=>e.cause?.message==='9lives iOS: open_create');
    assert.equal(records().length,1,'a create with lost output is recorded');
    commands.length=0;await driver.close();assert.equal(owned,undefined);
    assert.deepEqual(commands.map(args=>args.slice(1,3)),[['list','devices'],['shutdown',udid],['delete',udid],['list','devices']]);
    assert.deepEqual(records(),[]);
    createOutput=undefined;
    // Every session is headless and loopback-bound with private derived data.
    // Without products WebDriverAgent is compiled there; with them a private
    // copy is only run, and a wrong runner is refused before any device exists.
    const sessionCaps=()=>requests.filter(r=>r.pathname.endsWith('/session')).at(-1).body.capabilities.alwaysMatch;
    const copied=()=>path.join(sessionCaps()['appium:derivedDataPath'],'Build/Products');
    driver=iosTargetDriver(opts);await driver.open(c());
    assert.equal(sessionCaps()['appium:isHeadless'],true);assert.equal(sessionCaps()['appium:wdaBindingIP'],'127.0.0.1');
    assert.equal(sessionCaps()['appium:useNewWDA'],true);assert.equal(path.basename(sessionCaps()['appium:derivedDataPath']),'wda');
    assert.equal('appium:usePrebuiltWDA' in sessionCaps(),false);assert.equal(fs.existsSync(copied()),false);await driver.close();
    driver=iosTargetDriver({...opts,wdaProducts});await driver.open(c());
    assert.equal(sessionCaps()['appium:usePrebuiltWDA'],true);assert.equal(sessionCaps()['appium:isHeadless'],true);
    assert.notEqual(copied(),wdaProducts);
    assert.equal(fs.readFileSync(path.join(copied(),wdaRunner,'Info.plist'),'utf8'),'runner');
    assert.equal(fs.readFileSync(path.join(copied(),'WebDriverAgentRunner.xctestrun'),'utf8'),'run');
    const scratch=copied();await driver.close();assert.equal(owned,undefined);
    assert.equal(fs.existsSync(scratch),false);assert.equal(fs.existsSync(path.join(wdaProducts,wdaRunner,'Info.plist')),true);
    wdaBundle='dev.example.other';commands.length=0;driver=iosTargetDriver({...opts,wdaProducts});
    await assert.rejects(driver.open(c()),e=>e.record?.classification==='unknown'&&e.cause?.message==='9lives iOS: open_wda');
    await driver.close();assert.equal(commands.some(args=>args[1]==='create'),false);
    assert.deepEqual(records(),[]);
    assert.throws(()=>iosTargetDriver({...opts,wdaProducts:'relative/Products'}),/invalid_options/);
  } finally {
    cp.execFile=original.execFile;cp.spawn=original.spawn;global.fetch=original.fetch;process.kill=original.kill;
    Object.defineProperty(process,'platform',original.platform);
    if(original.scratch===undefined) delete process.env.NINELIVES_TARGET_SCRATCH_DIR;else process.env.NINELIVES_TARGET_SCRATCH_DIR=original.scratch;
    delete require.cache[require.resolve('../dist/ios')];fs.rmSync(dir,{recursive:true,force:true});
  }
});

test('iOS reporter emits bounded metadata only for the matching target', () => {
  const keys=['NINELIVES_ENGINE_PROTOCOL','NINELIVES_RUN_ID','NINELIVES_JOB_ID','NINELIVES_ATTEMPT_ID','NINELIVES_ENGINE_EVENTS','NINELIVES_TARGET'];
  const saved=Object.fromEntries(keys.map(key=>[key,process.env[key]]));
  const dir=fs.mkdtempSync(path.join(os.tmpdir(),'9l-ios-reporter-'));
  try {
    Object.assign(process.env,{NINELIVES_ENGINE_PROTOCOL:'9l.engine/1',NINELIVES_RUN_ID:'run-1',NINELIVES_JOB_ID:'job-001',NINELIVES_ATTEMPT_ID:'job-001-attempt-001'});
    const Reporter=require('../dist/reporter').default;
    const run=(target,body,contentType='application/vnd.9l.ios-runtime+json')=>{
      const events=path.join(dir,`${Math.random()}.ndjson`);
      Object.assign(process.env,{NINELIVES_TARGET:target,NINELIVES_ENGINE_EVENTS:events});
      const reporter=new Reporter();
      reporter.onTestEnd({id:'test-1',expectedStatus:'passed'},{retry:0,status:'passed',attachments:[{name:'9l.ios-runtime',contentType,body:Buffer.from(body)}]});
      reporter.onEnd({status:'passed'});
      return fs.readFileSync(events,'utf8').trim().split('\n').map(JSON.parse);
    };
    const frames=run('ios:finance',JSON.stringify(runtime));
    assert.equal(frames[0].type,'ios_runtime');assert.deepEqual(frames[0].iosRuntime,runtime);
    for (const [target,body,type] of [
      ['android:finance',JSON.stringify(runtime)],['web:finance',JSON.stringify(runtime)],
      ['ios:finance','{'],['ios:finance',JSON.stringify({...runtime,screen:'private'})],
      ['ios:finance',JSON.stringify(runtime),'application/json'],['ios:finance','x'.repeat(2049)],
    ]) assert.equal(run(target,body,type)[0].type,'engine_error');
  } finally {
    for(const key of keys) { if(saved[key]===undefined) delete process.env[key];else process.env[key]=saved[key]; }
    fs.rmSync(dir,{recursive:true,force:true});
  }
});
