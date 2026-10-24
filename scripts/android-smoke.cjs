// Opt-in real Android qualification. Uses installed SDK/AVD and private fixtures only.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const {spawnSync} = require('node:child_process');
const root = path.resolve(__dirname, '..');
const work = path.join(root, '.context', 'android-qualification', `smoke-${Date.now()}`);
fs.mkdirSync(work, {recursive: true});
const engine = path.join(work, '9l');
const spec = path.join(root, 'testdata/sdk/android/native.spec.ts');
const config = path.join(root, 'testdata/sdk/android.config.ts');
function sync(command, args, options = {}) {
  const r = spawnSync(command, args, {cwd: root, encoding: 'utf8', timeout: 330000, maxBuffer: 4 << 20, ...options});
  assert.ifError(r.error); return r;
}
assert.equal(sync('go', ['build', '-o', engine, './cmd/9l']).status, 0);
for (const mode of ['healthy','defect','state-defect','missing','ambiguous','blocked','install-fail']) {
  assert.equal(sync(process.execPath, ['scripts/build-android-fixture.cjs',mode]).status,0, `build ${mode}`);
}
const bad = path.join(root,'.context/android-bad-version/node_modules');
for (const name of ['appium','appium-uiautomator2-driver']) {
  fs.mkdirSync(path.join(bad,name),{recursive:true});
  fs.writeFileSync(path.join(bad,name,'package.json'), JSON.stringify({version:'0.0.0'}));
}
const creation = sync(engine,['provenance',spec,'--agent','codex']);
assert.equal(creation.status,0);
const provenance = path.join(work,'creation.json'); fs.writeFileSync(provenance,creation.stdout,{mode:0o600});
// Device identifiers stay in memory. Persist only preservation/cleanup facts.
function emulatorSet() {
  const sdk=process.env.ANDROID_HOME ?? '/usr/local/share/android-commandlinetools';
  const r=sync(path.join(sdk,'platform-tools/adb'),['devices']);assert.equal(r.status,0);
  return r.stdout.split('\n').map(line=>/^(emulator-[0-9]+)\s+device$/.exec(line)?.[1]).filter(Boolean).sort().join(',');
}
const results = [];
const modes = process.argv.slice(2).length ? process.argv.slice(2) : ['healthy','defect','state-defect','missing','ambiguous','cancel','blocked','disconnect','install-fail','unavailable','digest','version'];
for (const mode of modes) {
  try {
  assert(['healthy','defect','state-defect','missing','ambiguous','cancel','blocked','disconnect','install-fail','unavailable','digest','version'].includes(mode));
  const beforeDevices=emulatorSet();
  const probe = path.join(work, `${mode}-probe.json`);
  const r = sync(engine,['run',spec,'--sdk','--config',config,'--target','android:finance','--agent-provenance',provenance,
    '--timeout','4m','--deadline','5m','--attempts','3','--receipt-dir',path.join(work,mode),'--format','json',
    '--pass-env','NINELIVES_ANDROID_CASE,NINELIVES_ANDROID_PROBE,NINELIVES_ANDROID_AVD,ANDROID_HOME,JAVA_HOME'],
    {env:{...process.env,NINELIVES_ANDROID_CASE:mode,NINELIVES_ANDROID_PROBE:probe}});
  fs.writeFileSync(path.join(work, `${mode}.json`),r.stdout,{mode:0o600});
  const run = JSON.parse(r.stdout), receipt=run.receipts[0];
  assert.equal(run.receipts.length,1,mode);assert.equal(receipt.attempt,1,`${mode}: mutations must never retry`);
  assert.equal(receipt.agentProvenance.status,'matched',mode);
  const facts=JSON.parse(fs.readFileSync(probe,'utf8'));
  facts.emulatorSetPreserved=emulatorSet()===beforeDevices;assert.equal(facts.emulatorSetPreserved,true,`${mode}: emulator set changed`);
  if (mode==='healthy') {
    assert.equal(r.status,0);assert.equal(run.outcome,'passed');assert.equal(run.complete,true);
    assert.equal(receipt.validated,true);assert.equal(receipt.targetAssertions,5);assert.equal(receipt.verifiedAssertions,0);
    assert.equal(receipt.androidRuntimes.length,1);assert.equal(facts.clickDispatches,1);
  } else {
    assert.notEqual(r.status,0,mode);assert.notEqual(run.outcome,'passed',mode);
  }
  if (!['install-fail','unavailable','digest','version'].includes(mode)) assert.equal(facts.deviceStopped,true, `${mode}: owned emulator survived`);
  const raw=receipt.evidence.stdoutPath ? fs.readFileSync(receipt.evidence.stdoutPath,'utf8') : '';
  const frames=raw.trim().split('\n').filter(Boolean).map(JSON.parse);
  if (mode==='defect' || mode==='state-defect') {
    const op=mode==='defect'?'assert-text':'assert-state';
    assert(frames.some(f=>f.operation===op&&f.status==='failed'),`${mode}: relevant assertion must reject defect`);
    assert.equal(receipt.validated,true);assert.equal(facts.clickDispatches,1);
    assert.equal(facts.stopReason,'assertion_failed',`${mode}: setup/deadline failure is inconclusive`);
  }
  if (mode==='cancel'||mode==='blocked') {
    assert.equal(facts.started,true,`${mode}: app onClick must actually be in flight`);
    assert.equal(facts.clickDispatches,1);assert(frames.some(f=>f.operation==='tap'&&f.status==='failed'));
    assert.equal(facts.stopReason,mode==='cancel'?'canceled':'deadline');
  }
  if (mode==='missing'||mode==='ambiguous') {
    assert.equal(facts.clickDispatches,0);assert.equal(facts.stopReason,mode==='missing'?'missing_test_id':'ambiguous_test_id');
  }
  assert(!raw.includes('Sent $')&&!raw.includes('Ada')&&!raw.includes('hierarchy'), `${mode}: private content in protocol`);
  const result={mode,outcome:run.outcome,status:receipt.status,validated:receipt.validated,targetAssertions:receipt.targetAssertions??0,...facts};
  results.push(result);fs.writeFileSync(path.join(work,'results.json'),JSON.stringify({version:1,cases:results},null,2));
  console.log(JSON.stringify(result));
  } catch {
    const result={mode,qualified:false};results.push(result);
    fs.writeFileSync(path.join(work,'results.json'),JSON.stringify({version:1,cases:results},null,2));
    console.log(JSON.stringify(result));
  }
}
assert(results.every(result=>result.qualified!==false),'Real Android matrix has unqualified cases');
console.log(`Bounded real Android evidence: ${path.relative(root,work)}`);
