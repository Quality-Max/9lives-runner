// Synthetic, local UIKit app. Compilation/signing output is never persisted.
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const {spawnSync}=require('node:child_process');
const {iosAppDigest}=require('../packages/playwright/dist/ios');
const root=path.resolve(__dirname,'..');
const modes=['healthy','defect','missing','ambiguous','label-only'];
function command(file,args) {
  const result=spawnSync(file,args,{cwd:root,encoding:'utf8',timeout:120000,maxBuffer:1<<20});
  assert.equal(result.status,0,`${path.basename(file)} fixture command failed`);return result.stdout.trim();
}
assert.equal(process.platform,'darwin','iOS fixtures require Xcode on macOS');
const sdk=command('/usr/bin/xcrun',['--sdk','iphonesimulator','--show-sdk-path']);
const sdkVersion=command('/usr/bin/xcrun',['--sdk','iphonesimulator','--show-sdk-version']);
assert.match(sdkVersion,/^[0-9.]+$/);
const bundleId='dev.ninelives.iosfixture';
for(const mode of modes) {
  const directory=path.join(root,'.context/ios-fixture',mode),app=path.join(directory,'Finance.app');
  fs.rmSync(directory,{recursive:true,force:true});fs.mkdirSync(app,{recursive:true});
  fs.writeFileSync(path.join(app,'Info.plist'),`<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>${bundleId}</string><key>CFBundleExecutable</key><string>Finance</string><key>CFBundleName</key><string>Finance</string><key>CFBundlePackageType</key><string>APPL</string><key>CFBundleShortVersionString</key><string>1.0</string><key>CFBundleVersion</key><string>1</string><key>DTPlatformName</key><string>iphonesimulator</string><key>DTSDKName</key><string>iphonesimulator${sdkVersion}</string><key>MinimumOSVersion</key><string>18.5</string><key>UIDeviceFamily</key><array><integer>1</integer></array><key>CFBundleSupportedPlatforms</key><array><string>iPhoneSimulator</string></array><key>UILaunchScreen</key><dict/></dict></plist>`);
  command('/usr/bin/xcrun',['--sdk','iphonesimulator','clang','-arch',process.arch==='arm64'?'arm64':'x86_64','-isysroot',sdk,'-mios-simulator-version-min=18.5','-fobjc-arc','-framework','UIKit','-framework','Foundation',`-DFIXTURE_MODE=@"${mode}"`,'testdata/ios/Finance.m','-o',path.join(app,'Finance')]);
  command('/usr/bin/codesign',['--force','--sign','-',app]);
  fs.writeFileSync(path.join(directory,'build.json'),JSON.stringify({app,bundleId,appSHA256:iosAppDigest(app)}));
}
console.log('Built five private Simulator fixture variants');
