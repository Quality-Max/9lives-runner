// Real resource-ownership control: a stand-in preexisting native session must survive.
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const os = require('node:os');
const {createHash} = require('node:crypto');
const {spawnSync,execFile} = require('node:child_process');
const {androidTargetDriver} = require('../packages/playwright/dist/android');
const root = path.resolve(__dirname, '..');
const work = path.join(root, '.context/android-qualification', `preservation-${Date.now()}`);
const avd = process.env.NINELIVES_ANDROID_AVD ?? 'QualityMax_API_35';
const sdk = process.env.ANDROID_HOME ?? '/usr/local/share/android-commandlinetools';
const context = () => ({signal: new AbortController().signal, deadlineMs: Date.now() + 180000});
function sync(file,args,options={}) {
  const r = spawnSync(file,args,{cwd:root,encoding:'utf8',timeout:330000,maxBuffer:4<<20,...options});
  assert.ifError(r.error);return r;
}
(async () => {
  await fs.mkdir(work,{recursive:true});
  const scratch = await fs.mkdtemp(path.join(os.tmpdir(),'9l-preserve-'));
  process.env.NINELIVES_TARGET_SCRATCH_DIR = scratch;
  const fixture = JSON.parse(await fs.readFile(path.join(root,'.context/android-fixture/healthy/build.json'),'utf8'));
  const driver = androidTargetDriver({appId:'finance',sdkRoot:sdk,toolsRoot:path.join(root,'tools/android'),avd,...fixture});
  // Hash the existing AVD config in memory; no config values enter evidence.
  const avdConfig = path.join(os.homedir(),'.android/avd',`${avd}.avd/config.ini`);
  const digest = async () => createHash('sha256').update(await fs.readFile(avdConfig)).digest('hex');
  const before = await digest();
  try {
    await driver.open(context()); await driver.reset(context());
    const initial = await driver.observeTestId('recipient',context()); assert.equal(initial.length,1);
    await initial[0].fill('preserved synthetic marker',context());
    // Freeze creation evidence before starting a second owned emulator.
    const engine = path.join(work,'9l');
    assert.equal(sync('go',['build','-o',engine,'./cmd/9l']).status,0);
    const spec = path.join(root,'testdata/sdk/android/native.spec.ts');
    const creation = sync(engine,['provenance',spec,'--agent','codex']);assert.equal(creation.status,0);
    const provenance = path.join(work,'creation.json');await fs.writeFile(provenance,creation.stdout,{mode:0o600});
    const probe = path.join(work,'probe.json');
    let preexistingAlive = true;
    const keepAlive = setInterval(() => {
      void driver.observeTestId('recipient',{signal:new AbortController().signal,deadlineMs:Date.now()+5000}).catch(() => {preexistingAlive=false;});
    },20000);
    let result;
    try { result = await new Promise(resolve => execFile(engine,['run',spec,'--sdk','--config',path.join(root,'testdata/sdk/android.config.ts'),
      '--target','android:finance','--agent-provenance',provenance,'--timeout','4m','--deadline','5m',
      '--attempts','3','--receipt-dir',path.join(work,'receipts'),'--format','json',
      '--pass-env','NINELIVES_ANDROID_CASE,NINELIVES_ANDROID_PROBE,NINELIVES_ANDROID_AVD,ANDROID_HOME,JAVA_HOME'],
      {cwd:root,encoding:'utf8',timeout:330000,maxBuffer:4<<20,env:{...process.env,NINELIVES_ANDROID_CASE:'healthy',NINELIVES_ANDROID_PROBE:probe}},
      (error,stdout) => resolve({status:error?.code ?? 0,stdout})));
    } finally {clearInterval(keepAlive);}
    assert.equal(preexistingAlive,true);
    await fs.writeFile(path.join(work,'run.json'),result.stdout,{mode:0o600});
    const run=JSON.parse(result.stdout), receipt=run.receipts[0];
    assert.equal(result.status,0);assert.equal(run.complete,true);assert.equal(run.outcome,'passed');
    assert.equal(receipt.agentProvenance.status,'matched');assert.equal(receipt.attempt,1);assert.equal(receipt.targetAssertions,5);
    const facts=JSON.parse(await fs.readFile(probe,'utf8'));assert.equal(facts.deviceStopped,true);assert.equal(facts.clickDispatches,1);
    // A fresh read through the first native session proves it was not reset,
    // disconnected or terminated by cleanup of the second worker.
    const remaining=await driver.observeTestId('recipient',context());assert.equal(remaining.length,1);
    assert.equal(await remaining[0].text(context()),'preserved synthetic marker');
    assert.equal(await digest(),before);
    const evidence={version:1,preexistingSessionPreserved:true,preexistingAppStatePreserved:true,avdConfigurationPreserved:true,ownedWorkerStopped:true,targetAssertions:5};
    await fs.writeFile(path.join(work,'results.json'),JSON.stringify(evidence,null,2));
    console.log(JSON.stringify(evidence));console.log(`Bounded preservation evidence: ${path.relative(root,work)}`);
  } finally {await driver.close();await fs.rm(scratch,{recursive:true,force:true});}
})().catch(() => {console.error('Android preservation control failed; inspect bounded receipts.');process.exitCode=1;});
