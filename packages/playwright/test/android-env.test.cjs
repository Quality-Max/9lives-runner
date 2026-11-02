const assert = require('node:assert/strict');
const test = require('node:test');
const path = require('node:path');
const {androidDefaults, androidSdkCandidates, androidToolPaths, androidToolingPaths, androidToolsInstall, androidVersions} = require('../dist/android-env');
const android = require('../dist/android');

const home = '/home/fixture';
const none = () => false;
const only = (...dirs) => dir => dirs.includes(dir);

// POSIX expectations name a POSIX platform, so they hold on a Windows host too.
const linux = 'linux';

test('ANDROID_HOME wins, then ANDROID_SDK_ROOT, used as given even when missing', () => {
  assert.deepEqual(androidDefaults({env: {ANDROID_HOME: '/sdk/home', ANDROID_SDK_ROOT: '/sdk/home'}, home, platform: linux, isDirectory: none}),
    {sdkRoot: '/sdk/home', sdkSource: 'ANDROID_HOME', toolsRoot: '/home/fixture/.9l/android-tools', toolsSource: 'default'});
  const both = androidDefaults({env: {ANDROID_HOME: '/sdk/home', ANDROID_SDK_ROOT: '/sdk/root'}, home, platform: linux, isDirectory: none});
  assert.equal(both.sdkRoot, '/sdk/home');
  assert.equal(both.sdkRootIgnored, true, 'a conflicting ANDROID_SDK_ROOT is reported, not used');
  const root = androidDefaults({env: {ANDROID_HOME: ' ', ANDROID_SDK_ROOT: '/sdk/root'}, home, platform: linux, isDirectory: only('/opt/homebrew/share/android-commandlinetools')});
  assert.deepEqual([root.sdkRoot, root.sdkSource], ['/sdk/root', 'ANDROID_SDK_ROOT'], 'a blank variable counts as unset; a set one beats common locations');
  assert.equal(androidDefaults({env: {ANDROID_HOME: 'relative/sdk'}, home, isDirectory: none}).sdkRoot, path.resolve('relative/sdk'));
});

test('without variables the first existing common location is used, Homebrew before Android Studio', () => {
  const brew = '/opt/homebrew/share/android-commandlinetools', intel = '/usr/local/share/android-commandlinetools';
  const studio = '/home/fixture/Library/Android/sdk';
  assert.deepEqual(androidSdkCandidates({env: {}, home, platform: 'darwin'}).map(c => c.path),
    [brew, intel, '/home/linuxbrew/.linuxbrew/share/android-commandlinetools', studio]);
  assert.deepEqual(androidSdkCandidates({env: {HOMEBREW_PREFIX: '/custom/brew'}, home, platform: 'darwin'})[0],
    {path: '/custom/brew/share/android-commandlinetools', source: 'homebrew'}, 'HOMEBREW_PREFIX is tried first');
  assert.equal(androidSdkCandidates({env: {HOMEBREW_PREFIX: '/opt/homebrew'}, home, platform: 'darwin'}).filter(c => c.path === brew).length, 1);
  assert.deepEqual(androidSdkCandidates({env: {}, home, platform: 'linux'}).at(-1), {path: '/home/fixture/Android/Sdk', source: 'android-studio'});
  const pick = (...dirs) => androidDefaults({env: {}, home, platform: 'darwin', isDirectory: only(...dirs)});
  assert.deepEqual([pick(intel, studio).sdkRoot, pick(intel, studio).sdkSource], [intel, 'homebrew']);
  assert.deepEqual([pick(studio).sdkRoot, pick(studio).sdkSource], [studio, 'android-studio']);
  const nothing = pick();
  assert.equal(nothing.sdkRoot, undefined);
  assert.equal(nothing.sdkSource, undefined);
});

test('the tooling directory is NINELIVES_ANDROID_TOOLS, else under the home directory, and is never created', () => {
  assert.deepEqual(androidDefaults({env: {NINELIVES_ANDROID_TOOLS: '/opt/9l-tools'}, home, platform: linux, isDirectory: none}),
    {toolsRoot: '/opt/9l-tools', toolsSource: 'NINELIVES_ANDROID_TOOLS'});
  assert.equal(androidDefaults({env: {HOME: home}, platform: linux, isDirectory: none}).toolsRoot, '/home/fixture/.9l/android-tools');
  assert.deepEqual(androidToolingPaths('/t', linux), {
    appium: '/t/node_modules/appium/package.json', appiumEntry: '/t/node_modules/appium/index.js',
    uiautomator2: '/t/node_modules/appium-uiautomator2-driver/package.json'});
  assert.equal(androidToolsInstall('~/.9l/android-tools'),
    `npm install --prefix ~/.9l/android-tools --save-exact --ignore-scripts appium@${androidVersions.appium} appium-uiautomator2-driver@${androidVersions.uiautomator2}`);
});

test('SDK tool paths are fixed under the root and the pinned build-tools release', () => {
  assert.deepEqual(androidToolPaths('/sdk', 'linux'), {
    adb: '/sdk/platform-tools/adb', emulator: '/sdk/emulator/emulator', buildTools: '/sdk/build-tools/35.0.0',
    aapt2: '/sdk/build-tools/35.0.0/aapt2', apksigner: '/sdk/build-tools/35.0.0/apksigner'});
  assert.deepEqual(androidToolPaths('C:\\Android\\Sdk', 'win32'), {
    adb: 'C:\\Android\\Sdk\\platform-tools\\adb.exe', emulator: 'C:\\Android\\Sdk\\emulator\\emulator.exe', buildTools: 'C:\\Android\\Sdk\\build-tools\\35.0.0',
    aapt2: 'C:\\Android\\Sdk\\build-tools\\35.0.0\\aapt2.exe', apksigner: 'C:\\Android\\Sdk\\build-tools\\35.0.0\\apksigner.bat'});
});

test('Windows roots keep drive and separators, and the home directory is USERPROFILE', () => {
  const env = {USERPROFILE: 'C:\\Users\\f', HOME: '/c/Users/f', LOCALAPPDATA: 'C:\\Users\\f\\AppData\\Local'};
  assert.deepEqual(androidSdkCandidates({env, platform: 'win32'}), [{path: 'C:\\Users\\f\\AppData\\Local\\Android\\Sdk', source: 'android-studio'}],
    'no Homebrew location on Windows');
  assert.deepEqual(androidDefaults({env, platform: 'win32', isDirectory: only('C:\\Users\\f\\AppData\\Local\\Android\\Sdk')}),
    {sdkRoot: 'C:\\Users\\f\\AppData\\Local\\Android\\Sdk', sdkSource: 'android-studio', toolsRoot: 'C:\\Users\\f\\.9l\\android-tools', toolsSource: 'default'});
  assert.equal(androidDefaults({env: {...env, ANDROID_HOME: 'D:\\sdk'}, platform: 'win32', isDirectory: none}).sdkRoot, 'D:\\sdk');
});

test('the Android entry point exports the discovery with the same pins', () => {
  for (const name of ['androidDefaults', 'androidSdkCandidates', 'androidToolPaths', 'androidToolingPaths', 'androidToolsInstall']) assert.equal(typeof android[name], 'function', name);
  assert.equal(android.androidBuildTools, '35.0.0');
  assert.equal(android.androidVersions, androidVersions, 'one pin for the runtime receipt and the discovery');
});
