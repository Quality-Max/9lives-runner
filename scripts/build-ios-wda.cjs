// Compiles the pinned WebDriverAgent runner for the Simulator once, outside any
// test's open bound. Sessions install the product and start it without
// xcodebuild. Nothing is downloaded; compiler output is not persisted.
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const {spawnSync}=require('node:child_process');
const root=path.resolve(__dirname,'..');
assert.equal(process.platform,'darwin','WebDriverAgent requires Xcode on macOS');
const driver=path.join(root,'tools/ios/node_modules/appium-xcuitest-driver');
const source=[path.join(driver,'node_modules/appium-webdriveragent'),path.join(root,'tools/ios/node_modules/appium-webdriveragent')]
  .find(candidate=>fs.existsSync(path.join(candidate,'WebDriverAgent.xcodeproj')));
assert(source,'install the pinned tools first: npm ci --prefix tools/ios --ignore-scripts');
const version=name=>JSON.parse(fs.readFileSync(path.join(name,'package.json'),'utf8')).version;
const xcode=spawnSync('/usr/bin/xcodebuild',['-version'],{encoding:'utf8',timeout:60000});
assert.equal(xcode.status,0,'full Xcode is required; Command Line Tools alone are insufficient');
const identity={version:1,xcuitest:version(driver),webDriverAgent:version(source),xcode:xcode.stdout.trim().split('\n').join(' '),architecture:process.arch==='arm64'?'arm64':'x86_64'};
const directory=path.join(root,'.context/ios-wda'),derived=path.join(directory,'derived'),record=path.join(directory,'build.json');
const app=path.join(derived,'Build/Products/Debug-iphonesimulator/WebDriverAgentRunner-Runner.app');
const same=()=>{
  try {
    const {app:built,elapsedMs,...previous}=JSON.parse(fs.readFileSync(record,'utf8'));
    return built===app&&fs.existsSync(path.join(app,'Info.plist'))&&JSON.stringify(previous)===JSON.stringify(identity);
  } catch { return false; }
};
if(same()) {console.log('Reusing the WebDriverAgent runner built for these tools and this Xcode');process.exit(0);}
fs.rmSync(directory,{recursive:true,force:true});fs.mkdirSync(directory,{recursive:true});
const started=Date.now();
// A generic Simulator destination needs no booted device. The product is
// unsigned, which the Simulator accepts; it never runs on a physical iPhone.
const build=spawnSync('/usr/bin/xcodebuild',['build-for-testing','-project',path.join(source,'WebDriverAgent.xcodeproj'),'-scheme','WebDriverAgentRunner',
  '-destination','generic/platform=iOS Simulator','-derivedDataPath',derived,'CODE_SIGNING_ALLOWED=NO',`ARCHS=${identity.architecture}`],
  {cwd:directory,encoding:'utf8',timeout:1500000,maxBuffer:64<<20});
if(build.error||build.status!==0||!fs.existsSync(path.join(app,'Info.plist'))) {
  // A bounded tail of compiler output for diagnosis only, never written to disk.
  process.stderr.write(`${String(build.stdout||'').slice(-4000)}\n${String(build.stderr||'').slice(-2000)}\n`);
  assert.fail(`WebDriverAgent build failed (${build.error?.code||build.status})`);
}
fs.writeFileSync(record,JSON.stringify({...identity,app,elapsedMs:Date.now()-started}));
console.log(`Built the WebDriverAgent runner in ${Math.round((Date.now()-started)/1000)} s`);
