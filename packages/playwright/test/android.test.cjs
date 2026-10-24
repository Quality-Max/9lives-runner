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
