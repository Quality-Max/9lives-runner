// Build a synthetic native fixture using an existing SDK; no Gradle download.
const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const crypto = require('node:crypto');
const {spawnSync} = require('node:child_process');
const root = path.resolve(__dirname, '..');
const mode = process.argv[2] ?? 'healthy';
if (!['healthy', 'defect', 'state-defect', 'missing', 'ambiguous', 'blocked', 'install-fail'].includes(mode)) throw Error('invalid fixture mode');
const sdk = process.env.ANDROID_HOME ?? '/usr/local/share/android-commandlinetools';
const java = process.env.JAVA_HOME ?? '/Library/Java/JavaVirtualMachines/jdk-23.jdk/Contents/Home';
const build = path.join(sdk, 'build-tools/35.0.0');
const androidJar = path.join(sdk, 'platforms/android-35/android.jar');
const temp = fs.mkdtempSync(path.join(os.tmpdir(), '9l-apk-'));
const dest = path.join(root, '.context/android-fixture', mode);
const childEnv = {PATH: process.env.PATH, HOME: process.env.HOME, JAVA_HOME: java,
  NINELIVES_FIXTURE_SIGNING_PASSWORD: crypto.randomBytes(32).toString('hex')};
function run(file, args, cwd = temp) {
  const result = spawnSync(file, args, {cwd, env: childEnv, stdio: ['ignore', 'ignore', 'pipe'], encoding: 'utf8', timeout: 60000});
  if (result.status !== 0) throw Error(`fixture tool failed: ${path.basename(file)}`);
}
try {
  fs.mkdirSync(dest, {recursive: true});
  fs.mkdirSync(path.join(temp, 'gen'));
  fs.mkdirSync(path.join(temp, 'classes'));
  run(path.join(build, 'aapt2'), ['compile', '--dir', path.join(root, 'testdata/android/res'), '-o', path.join(temp, 'res.zip')]);
  const manifest = path.join(temp, 'AndroidManifest.xml');
  fs.writeFileSync(manifest, fs.readFileSync(path.join(root, 'testdata/android/AndroidManifest.xml'), 'utf8').replace('minSdkVersion="26"', mode === 'install-fail' ? 'minSdkVersion="99"' : 'minSdkVersion="26"'));
  run(path.join(build, 'aapt2'), ['link', '-I', androidJar, '--manifest', manifest, '--java', path.join(temp, 'gen'), '-o', path.join(temp, 'base.apk'), path.join(temp, 'res.zip')]);
  fs.writeFileSync(path.join(temp, 'FixtureMode.java'), `package dev.ninelives.fixture; final class FixtureMode { static final String VALUE = "${mode}"; }`);
  run(path.join(java, 'bin/javac'), ['-source', '8', '-target', '8', '-bootclasspath', androidJar, '-d', path.join(temp, 'classes'), path.join(root, 'testdata/android/MainActivity.java'), path.join(temp, 'gen/dev/ninelives/fixture/R.java'), path.join(temp, 'FixtureMode.java')]);
  const classes = fs.readdirSync(path.join(temp, 'classes/dev/ninelives/fixture')).filter(name => name.endsWith('.class')).map(name => path.join(temp, 'classes/dev/ninelives/fixture', name));
  run(path.join(build, 'd8'), ['--lib', androidJar, '--output', temp, ...classes]);
  run('/usr/bin/zip', ['-q', path.join(temp, 'base.apk'), 'classes.dex']);
  run(path.join(build, 'zipalign'), ['-f', '4', path.join(temp, 'base.apk'), path.join(temp, 'aligned.apk')]);
  run(path.join(java, 'bin/keytool'), ['-genkeypair', '-keystore', path.join(temp, 'fixture.p12'), '-alias', 'fixture', '-storepass:env', 'NINELIVES_FIXTURE_SIGNING_PASSWORD', '-keypass:env', 'NINELIVES_FIXTURE_SIGNING_PASSWORD', '-dname', 'CN=9l Synthetic Fixture', '-keyalg', 'RSA', '-validity', '30']);
  const apk = path.join(dest, 'fixture.apk');
  run(path.join(build, 'apksigner'), ['sign', '--ks', path.join(temp, 'fixture.p12'), '--ks-pass', 'env:NINELIVES_FIXTURE_SIGNING_PASSWORD', '--key-pass', 'env:NINELIVES_FIXTURE_SIGNING_PASSWORD', '--out', apk, path.join(temp, 'aligned.apk')]);
  const metadata = {apk, apkSHA256: crypto.createHash('sha256').update(fs.readFileSync(apk)).digest('hex'), mode, appPackage: 'dev.ninelives.fixture', activity: '.MainActivity', buildTools: '35.0.0', platform: 'android-35'};
  fs.writeFileSync(path.join(dest, 'build.json'), JSON.stringify(metadata, null, 2) + '\n');
  process.stdout.write(JSON.stringify(metadata) + '\n');
} finally {
  fs.rmSync(temp, {recursive: true, force: true});
}
