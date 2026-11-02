const assert = require('node:assert/strict');
const test = require('node:test');
const path = require('node:path');
const {androidDefaults, androidSdkCandidates, androidToolPaths, androidToolingPaths, androidToolsInstall, androidVersions} = require('../dist/android-env');
const android = require('../dist/android');

const home = '/home/fixture';
const none = () => false;
const only = (...dirs) => dir => dirs.includes(dir);

test('ANDROID_HOME wins, then ANDROID_SDK_ROOT, used as given even when missing', () => {
  assert.deepEqual(androidDefaults({env: {ANDROID_HOME: '/sdk/home', ANDROID_SDK_ROOT: '/sdk/home'}, home, isDirectory: none}),
    {sdkRoot: '/sdk/home', sdkSource: 'ANDROID_HOME', toolsRoot: path.join(home, '.9l/android-tools'), toolsSource: 'default'});
  const both = androidDefaults({env: {ANDROID_HOME: '/sdk/home', ANDROID_SDK_ROOT: '/sdk/root'}, home, isDirectory: none});
  assert.equal(both.sdkRoot, '/sdk/home');
  assert.equal(both.sdkRootIgnored, true, 'a conflicting ANDROID_SDK_ROOT is reported, not used');
  const root = androidDefaults({env: {ANDROID_HOME: ' ', ANDROID_SDK_ROOT: '/sdk/root'}, home, isDirectory: only('/opt/homebrew/share/android-commandlinetools')});
  assert.deepEqual([root.sdkRoot, root.sdkSource], ['/sdk/root', 'ANDROID_SDK_ROOT'], 'a blank variable counts as unset; a set one beats common locations');
  assert.equal(androidDefaults({env: {ANDROID_HOME: 'relative/sdk'}, home, isDirectory: none}).sdkRoot, path.resolve('relative/sdk'));
});

test('without variables the first existing common location is used, Homebrew before Android Studio', () => {
  const brew = '/opt/homebrew/share/android-commandlinetools', intel = '/usr/local/share/android-commandlinetools';
  const studio = path.join(home, 'Library/Android/sdk');
  assert.deepEqual(androidSdkCandidates({env: {}, home, platform: 'darwin'}).map(c => c.path),
    [brew, intel, '/home/linuxbrew/.linuxbrew/share/android-commandlinetools', studio]);
  assert.deepEqual(androidSdkCandidates({env: {HOMEBREW_PREFIX: '/custom/brew'}, home, platform: 'darwin'})[0],
    {path: '/custom/brew/share/android-commandlinetools', source: 'homebrew'}, 'HOMEBREW_PREFIX is tried first');
  assert.equal(androidSdkCandidates({env: {HOMEBREW_PREFIX: '/opt/homebrew'}, home, platform: 'darwin'}).filter(c => c.path === brew).length, 1);
  assert.deepEqual(androidSdkCandidates({env: {}, home, platform: 'linux'}).at(-1), {path: path.join(home, 'Android/Sdk'), source: 'android-studio'});
  assert.deepEqual(androidSdkCandidates({env: {LOCALAPPDATA: 'C:/Users/f/AppData/Local'}, home, platform: 'win32'}).map(c => c.source), ['android-studio']);
  const pick = (...dirs) => androidDefaults({env: {}, home, platform: 'darwin', isDirectory: only(...dirs)});
  assert.deepEqual([pick(intel, studio).sdkRoot, pick(intel, studio).sdkSource], [intel, 'homebrew']);
  assert.deepEqual([pick(studio).sdkRoot, pick(studio).sdkSource], [studio, 'android-studio']);
  const nothing = pick();
  assert.equal(nothing.sdkRoot, undefined);
  assert.equal(nothing.sdkSource, undefined);
});

test('the tooling directory is NINELIVES_ANDROID_TOOLS, else under the home directory, and is never created', () => {
  assert.deepEqual(androidDefaults({env: {NINELIVES_ANDROID_TOOLS: '/opt/9l-tools'}, home, isDirectory: none}),
    {toolsRoot: '/opt/9l-tools', toolsSource: 'NINELIVES_ANDROID_TOOLS'});
  assert.equal(androidDefaults({env: {}, home, isDirectory: none}).toolsRoot, path.join(home, '.9l/android-tools'));
  assert.deepEqual(androidToolingPaths('/t'), {
    appium: path.join('/t', 'node_modules/appium/package.json'), appiumEntry: path.join('/t', 'node_modules/appium/index.js'),
    uiautomator2: path.join('/t', 'node_modules/appium-uiautomator2-driver/package.json')});
  assert.equal(androidToolsInstall('~/.9l/android-tools'),
    `npm install --prefix ~/.9l/android-tools --save-exact --ignore-scripts appium@${androidVersions.appium} appium-uiautomator2-driver@${androidVersions.uiautomator2}`);
});

test('SDK tool paths are fixed under the root and the pinned build-tools release', () => {
  assert.deepEqual(androidToolPaths('/sdk', 'linux'), {
    adb: path.join('/sdk', 'platform-tools/adb'), emulator: path.join('/sdk', 'emulator/emulator'), buildTools: path.join('/sdk', 'build-tools/35.0.0'),
    aapt2: path.join('/sdk', 'build-tools/35.0.0/aapt2'), apksigner: path.join('/sdk', 'build-tools/35.0.0/apksigner')});
  const windows = androidToolPaths('/sdk', 'win32');
  assert.deepEqual([path.basename(windows.adb), path.basename(windows.emulator), path.basename(windows.apksigner)], ['adb.exe', 'emulator.exe', 'apksigner.bat']);
});

test('the Android entry point exports the discovery with the same pins', () => {
  for (const name of ['androidDefaults', 'androidSdkCandidates', 'androidToolPaths', 'androidToolingPaths', 'androidToolsInstall']) assert.equal(typeof android[name], 'function', name);
  assert.equal(android.androidBuildTools, '35.0.0');
  assert.equal(android.androidVersions, androidVersions, 'one pin for the runtime receipt and the discovery');
});
