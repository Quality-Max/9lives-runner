// Real Simulator qualification, never a synthetic transport benchmark.
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const {spawnSync}=require('node:child_process');
const root=path.resolve(__dirname,'..');
const modes=['healthy','defect','missing','ambiguous','label-only','digest','unavailable','cancel','blocked'];
if(process.argv[2]==='--list') {console.log(modes.join('\n'));process.exit(0);}
const selected=process.argv.slice(2).length?process.argv.slice(2):modes;
assert(selected.every(mode=>modes.includes(mode)),'unknown iOS case');
assert.equal(process.platform,'darwin','iOS smoke requires Xcode and iOS 18.6 Simulator');
const work=path.join(root,'.context/ios-qualification',`smoke-${Date.now()}`);
fs.mkdirSync(work,{recursive:true});
function command(file,args,options={}) {
  const result=spawnSync(file,args,{cwd:root,encoding:'utf8',timeout:600000,maxBuffer:4<<20,...options});
  assert.ifError(result.error);return result;
}
const engine=path.join(work,'9l');
assert.equal(command('go',['build','-o',engine,'./cmd/9l']).status,0,'build engine');
assert.equal(command(process.execPath,['scripts/build-ios-fixture.cjs']).status,0,'build fixtures');
const spec=path.join(root,'testdata/sdk/ios/native.spec.ts');
const creation=command(engine,['provenance',spec,'--agent','codex']);assert.equal(creation.status,0);
const provenance=path.join(work,'creation.json');fs.writeFileSync(provenance,creation.stdout,{mode:0o600});
function devices() {
  const result=command('/usr/bin/xcrun',['simctl','list','devices','-j']);assert.equal(result.status,0);
  return Object.values(JSON.parse(result.stdout).devices).flat();
}
const deviceSet=()=>devices().map(d=>`${d.udid}:${d.state}`).sort().join(',');
// An unrelated stopped simulator must survive every success/failure/cancel.
const sentinel=command('/usr/bin/xcrun',['simctl','create',`9l-sentinel-${Date.now()}`,'com.apple.CoreSimulator.SimDeviceType.iPhone-16','com.apple.CoreSimulator.SimRuntime.iOS-18-6']);
assert.equal(sentinel.status,0,'create unrelated sentinel');
const sentinelId=sentinel.stdout.trim();assert.match(sentinelId,/^[A-Fa-f0-9-]{36}$/);
const results=[];
try {
  for(const mode of selected) {
    const before=deviceSet(),probe=path.join(work,`${mode}-probe.json`);
    const run=command(engine,['run',spec,'--sdk','--config',path.join(root,'testdata/sdk/ios.config.ts'),'--target','ios:finance',
      '--agent-provenance',provenance,'--timeout','8m','--deadline','9m','--attempts','3','--receipt-dir',path.join(work,mode),'--format','json',
      '--pass-env','NINELIVES_IOS_CASE,NINELIVES_IOS_PROBE,DEVELOPER_DIR'],
      {env:{...process.env,NINELIVES_IOS_CASE:mode,NINELIVES_IOS_PROBE:probe}});
    // Only sanitized engine receipts/protocol and bounded probe facts persist.
    fs.writeFileSync(path.join(work,`${mode}.json`),run.stdout,{mode:0o600});
    const data=JSON.parse(run.stdout),receipt=data.receipts[0];
    assert.equal(data.receipts.length,1,mode);assert.equal(receipt.attempt,1,`${mode}: native mutations cannot retry`);
    assert.equal(receipt.agentProvenance.status,'matched',mode);
    assert.equal(receipt.validated,true,`${mode}: complete terminal evidence`);
    const facts=JSON.parse(fs.readFileSync(probe,'utf8'));
    assert.equal(deviceSet(),before,`${mode}: simulator set/state changed`);
    assert(devices().some(d=>d.udid===sentinelId),`${mode}: unrelated simulator lost`);
    const raw=receipt.evidence.stdoutPath?fs.readFileSync(receipt.evidence.stdoutPath,'utf8'):'';
    assert(!['Sent $','Pending','hierarchy','Documents/started'].some(value=>raw.includes(value)),`${mode}: content leaked`);
    const frames=raw.trim().split('\n').filter(Boolean).map(JSON.parse);
    if(mode==='healthy') {
      assert.equal(run.status,0,`healthy run failed; setup phase: ${facts.setupPhase||'unknown'}`);assert.equal(data.outcome,'passed');assert.equal(data.complete,true);
      assert.equal(receipt.targetAssertions,3);assert.equal(receipt.verifiedAssertions,0);
      assert.equal(receipt.iosRuntimes.length,1);assert.equal(facts.clickDispatches,1);
      const rt=receipt.iosRuntimes[0];
      const fixture=JSON.parse(fs.readFileSync(path.join(root,'.context/ios-fixture/healthy/build.json'),'utf8'));
      assert.equal(rt.appSHA256,fixture.appSHA256);assert.equal(rt.bundleId,fixture.bundleId);
      assert.equal(rt.os,'18.6');assert.equal(rt.xcode,'16.4');assert.equal(rt.deviceType,'com.apple.CoreSimulator.SimDeviceType.iPhone-16');
    } else {assert.notEqual(run.status,0,mode);assert.notEqual(data.outcome,'passed',mode);}
    if(!['digest','unavailable'].includes(mode)) assert.equal(facts.deviceStopped,true,`${mode}: owned device survived`);
    if(mode==='defect') {
      assert(frames.some(f=>f.operation==='assert-text'&&f.status==='failed'),'defect must fail relevant assertion');
      assert.equal(facts.stopReason,'assertion_failed');assert.equal(facts.clickDispatches,1);
    }
    if(['missing','ambiguous','label-only'].includes(mode)) {
      assert.equal(facts.clickDispatches,0);assert.equal(facts.stopReason,mode==='ambiguous'?'ambiguous_test_id':'missing_test_id');
    }
    if(mode==='digest'||mode==='unavailable') {
      assert.equal(facts.clickDispatches,0);
      assert.deepEqual(receipt.targetSetup.map(({testId,...rest})=>rest),[{classification:mode==='digest'?'mismatch':'unknown',reason:mode==='digest'?'build-digest':'device-state-unknown',stage:'open'}]);
    }
    if(mode==='cancel'||mode==='blocked') {
      assert.equal(facts.started,true,'real native mutation must start');assert.equal(facts.clickDispatches,1);
      assert.equal(facts.stopReason,mode==='cancel'?'canceled':'operation_timeout');
      assert(frames.some(f=>f.operation==='tap'&&f.status==='failed'));
    }
    const result={mode,qualified:true,outcome:data.outcome,validated:receipt.validated,simulatorSetPreserved:true,...facts};
    results.push(result);fs.writeFileSync(path.join(work,'results.json'),JSON.stringify({version:1,cases:results},null,2));
    console.log(JSON.stringify(result));
  }
} finally {
  assert.equal(command('/usr/bin/xcrun',['simctl','delete',sentinelId]).status,0,'remove owned sentinel');
  fs.rmSync(engine,{force:true});
}
