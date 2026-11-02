"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.adbDevices = adbDevices;
exports.javaMajor = javaMajor;
exports.androidChecks = androidChecks;
// `9l doctor --android`: the host prerequisites of the optional Android
// target. SDK and tooling roots come from the same discovery the driver uses
// for omitted options (android-env.ts). Doctor runs only `java -version`,
// `adb version` and `emulator -list-avds`, reads two package manifests and
// asks an already running adb server for its device list over loopback. It
// never starts the adb server, boots an emulator, installs or downloads
// anything. Device serials stay in memory: only counts by kind and state are
// reported. Paths appear only under --paths.
const node_child_process_1 = require("node:child_process");
const node_fs_1 = require("node:fs");
const node_net_1 = require("node:net");
const node_path_1 = require("node:path");
const android_env_1 = require("./android-env");
const commandTimeout = 8_000;
const safeVersion = /^\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?$/;
const avdName = /^[a-zA-Z0-9_.-]{1,100}$/;
const exists = (path, kind) => {
    try {
        const info = (0, node_fs_1.statSync)(path);
        return kind === 'file' ? info.isFile() : info.isDirectory();
    }
    catch {
        return false;
    }
};
// Bounded, time-limited and without a shell. stdout and stderr are combined
// because `java -version` reports on stderr.
const run = (file, args, env) => {
    const result = (0, node_child_process_1.spawnSync)(file, args, { env, encoding: 'utf8', timeout: commandTimeout, killSignal: 'SIGKILL', maxBuffer: 1 << 20, windowsHide: true });
    const code = result.error?.code;
    return { status: result.error ? null : result.status, output: `${result.stdout ?? ''}\n${result.stderr ?? ''}`, absent: code === 'ENOENT' || code === 'EACCES' };
};
/** The device list of an adb server already listening on loopback, by the
 * adb host protocol (`host:devices`). A refused connection means no server
 * is running; doctor does not start one. Serials are dropped while parsing. */
function adbDevices(port, timeoutMs = 2_000) {
    return new Promise(settle => {
        let data = Buffer.alloc(0), done = false;
        const socket = (0, node_net_1.connect)({ host: '127.0.0.1', port });
        const finish = (value) => {
            if (done)
                return;
            done = true;
            clearTimeout(timer);
            socket.destroy();
            settle(value);
        };
        const timer = setTimeout(() => finish({ running: true }), timeoutMs);
        const parse = () => {
            if (data.length < 4)
                return;
            if (data.subarray(0, 4).toString('latin1') !== 'OKAY')
                return finish({ running: true });
            if (data.length < 8)
                return;
            const length = Number.parseInt(data.subarray(4, 8).toString('latin1'), 16);
            if (!Number.isInteger(length) || length < 0)
                return finish({ running: true });
            if (data.length < 8 + length)
                return;
            const devices = data.subarray(8, 8 + length).toString('utf8').split('\n').map(line => /^(\S+)\t(\S+)$/.exec(line.trim()))
                .filter((match) => match !== null)
                .map(([, serial, state]) => ({ emulator: serial.startsWith('emulator-'), state }));
            finish({ running: true, devices });
        };
        socket.on('connect', () => socket.write('000chost:devices'));
        socket.on('data', chunk => {
            data = Buffer.concat([data, chunk]);
            if (data.length > 64 * 1024)
                finish({ running: true });
            else
                parse();
        });
        socket.on('end', () => finish({ running: true }));
        socket.on('error', error => finish(error.code === 'ECONNREFUSED' ? { running: false } : { running: true }));
    });
}
/** `java -version` output to a major release: "1.8.0_402" is 8, "21.0.5" is 21. */
function javaMajor(output) {
    const version = /version "([0-9][0-9._+a-zA-Z-]{0,40})"/.exec(output)?.[1];
    if (!version)
        return undefined;
    const parts = version.split(/[._+-]/).map(Number);
    const major = parts[0] === 1 && parts.length > 1 ? parts[1] : parts[0];
    return Number.isInteger(major) && major > 0 ? { version, major } : undefined;
}
function manifest(file, name) {
    try {
        if ((0, node_fs_1.statSync)(file).size > 256 * 1024)
            return { present: true };
        const data = JSON.parse((0, node_fs_1.readFileSync)(file, 'utf8'));
        return { present: true, version: data?.name === name && typeof data.version === 'string' && safeVersion.test(data.version) ? data.version : undefined };
    }
    catch {
        return { present: exists(file, 'file') };
    }
}
async function androidChecks(options = {}) {
    const env = options.env ?? process.env, platform = options.platform ?? process.platform, arch = options.arch ?? process.arch;
    const exec = options.run ?? run, paths = options.paths === true;
    const at = (path) => paths ? `: ${path}` : '';
    const checks = [];
    const base = {};
    for (const key of ['PATH', 'HOME', 'USERPROFILE', 'LOCALAPPDATA', 'SystemRoot', 'TMPDIR', 'TEMP', 'TMP', 'JAVA_HOME', 'ANDROID_USER_HOME', 'ANDROID_EMULATOR_HOME', 'ANDROID_AVD_HOME']) {
        if (env[key] !== undefined)
            base[key] = env[key];
    }
    // Appium 3 runs on Node 22 from 22.12; doctor's node check already requires 22.
    const [nodeMajor, nodeMinor] = (options.nodeVersion ?? process.versions.node).split('.').map(Number);
    if (nodeMajor === 22 && nodeMinor < 12) {
        checks.push({ name: 'android/node', status: 'missing', message: `Appium ${android_env_1.androidVersions.appium} needs Node 22.12 or newer.`, fix: 'Install Node 24 LTS, then restart your terminal.' });
    }
    const javaHome = env.JAVA_HOME?.trim() ? env.JAVA_HOME : undefined;
    const javaFix = platform === 'darwin'
        ? 'brew install openjdk@21 && export JAVA_HOME="$(brew --prefix openjdk@21)/libexec/openjdk.jdk/Contents/Home"'
        : platform === 'win32' ? 'install a JDK 17 or newer, then set JAVA_HOME to it' : 'install a JDK 17 or newer (for example: sudo apt-get install openjdk-21-jdk-headless), then export JAVA_HOME to it';
    const java = javaHome ? (0, node_path_1.join)(javaHome, 'bin', platform === 'win32' ? 'java.exe' : 'java') : 'java';
    if (javaHome && !exists(java, 'file')) {
        checks.push({ name: 'android/java', status: 'missing', message: `JAVA_HOME is set but has no bin/java${at(javaHome)}.`, fix: javaFix });
    }
    else {
        const result = exec(java, ['-version'], base);
        const release = result.status === 0 ? javaMajor(result.output) : undefined;
        const from = javaHome ? `JAVA_HOME${at(javaHome)}; 9l run passes it to tests only with --pass-env JAVA_HOME` : 'PATH (JAVA_HOME is unset)';
        if (!release) {
            checks.push({ name: 'android/java', status: 'missing',
                message: result.absent ? 'No java on PATH and JAVA_HOME is unset; Appium, UiAutomator2 and apksigner need Java 17 or newer.' : `java -version from ${javaHome ? 'JAVA_HOME' : 'PATH (JAVA_HOME is unset)'} did not report a Java version; Appium, UiAutomator2 and apksigner need Java 17 or newer.`, fix: javaFix });
        }
        else if (release.major < 17) {
            checks.push({ name: 'android/java', status: 'missing', message: `Java ${release.version} from ${from}; Appium, UiAutomator2 and apksigner need 17 or newer.`, fix: javaFix });
        }
        else {
            checks.push({ name: 'android/java', status: 'ok', message: `Java ${release.version} from ${from}.` });
        }
    }
    const found = (0, android_env_1.androidDefaults)({ env, home: options.home, platform });
    const sdk = found.sdkRoot;
    if (!sdk) {
        checks.push({ name: 'android/sdk', status: 'missing', message: 'No Android SDK: ANDROID_HOME and ANDROID_SDK_ROOT are unset and no common SDK location exists.',
            fix: platform === 'darwin' ? 'brew install --cask android-commandlinetools && export ANDROID_HOME="$(brew --prefix)/share/android-commandlinetools"'
                : 'install the Android command-line tools, then export ANDROID_HOME to the SDK root' });
    }
    else if (!exists(sdk, 'directory')) {
        checks.push({ name: 'android/sdk', status: 'missing', message: `${found.sdkSource} is set to a directory that does not exist${at(sdk)}.`, fix: `export ${found.sdkSource} to the SDK root, the directory that holds platform-tools` });
    }
    else {
        const origin = found.sdkSource === 'homebrew' ? 'the Homebrew command-line tools location' : found.sdkSource === 'android-studio' ? "Android Studio's default location" : found.sdkSource;
        const passing = found.sdkSource === 'ANDROID_HOME' || found.sdkSource === 'ANDROID_SDK_ROOT'
            ? `; 9l run passes it to tests only with --pass-env ${found.sdkSource}, otherwise the driver looks in the common locations`
            : '; the driver finds the same root when sdkRoot is omitted';
        checks.push(found.sdkRootIgnored
            ? { name: 'android/sdk', status: 'warning', message: `SDK root from ANDROID_HOME${at(sdk)}; ANDROID_SDK_ROOT names another directory and is ignored.`, fix: 'unset ANDROID_SDK_ROOT or set it to the same directory as ANDROID_HOME' }
            : { name: 'android/sdk', status: 'ok', message: `SDK root from ${origin}${at(sdk)}${passing}.` });
    }
    const tools = sdk && exists(sdk, 'directory') ? (0, android_env_1.androidToolPaths)(sdk, platform) : undefined;
    let emulator = false;
    if (tools) {
        const child = { ...base, ANDROID_HOME: sdk, ANDROID_SDK_ROOT: sdk };
        if (!exists(tools.adb, 'file')) {
            checks.push({ name: 'android/adb', status: 'missing', message: 'platform-tools/adb is missing.', fix: 'sdkmanager "platform-tools"' });
        }
        else {
            const result = exec(tools.adb, ['version'], child);
            const version = /^Version ([0-9][0-9A-Za-z.-]{0,40})$/m.exec(result.output)?.[1];
            checks.push(result.status === 0 && version
                ? { name: 'android/adb', status: 'ok', message: `adb ${version}.` }
                : { name: 'android/adb', status: 'missing', message: 'platform-tools/adb is present but adb version failed.', fix: 'sdkmanager "platform-tools"' });
        }
        emulator = exists(tools.emulator, 'file');
        checks.push(emulator
            ? { name: 'android/emulator', status: 'ok', message: 'emulator/emulator is present.' }
            : { name: 'android/emulator', status: 'warning', message: 'emulator/emulator is missing; owned-emulator runs (avd and apk options) need it, attached devices do not.', fix: 'sdkmanager "emulator"' });
        const absent = ['aapt2', 'apksigner'].filter(name => !exists(tools[name], 'file'));
        checks.push(absent.length
            ? { name: 'android/build-tools', status: 'missing', message: `build-tools/${android_env_1.androidBuildTools} lacks ${absent.join(' and ')}; the driver reads APK identity and signers with them.`, fix: `sdkmanager "build-tools;${android_env_1.androidBuildTools}"` }
            : { name: 'android/build-tools', status: 'ok', message: `build-tools/${android_env_1.androidBuildTools} has aapt2 and apksigner.` });
        if (emulator) {
            const result = exec(tools.emulator, ['-list-avds'], child);
            // Newer emulators print INFO lines; only bare AVD names count.
            const avds = result.output.split('\n').map(line => line.trim()).filter(line => avdName.test(line));
            const image = `system-images;android-35;google_apis;${arch === 'x64' ? 'x86_64' : 'arm64-v8a'}`;
            if (result.status !== 0) {
                checks.push({ name: 'android/avds', status: 'warning', message: 'emulator -list-avds failed, so AVDs were not listed.', fix: 'run emulator -list-avds from the SDK to see its error' });
            }
            else if (!avds.length) {
                checks.push({ name: 'android/avds', status: 'warning', message: 'No AVDs; owned-emulator runs need an existing API 35 AVD, attached devices do not.',
                    fix: `sdkmanager "${image}" && avdmanager create avd -n 9l_api35 -k "${image}"` });
            }
            else {
                const shown = avds.slice(0, 10).join(', ') + (avds.length > 10 ? `, and ${avds.length - 10} more` : '');
                checks.push({ name: 'android/avds', status: 'ok', message: `${avds.length} AVD${avds.length === 1 ? '' : 's'}: ${shown}. The driver boots the one named by its avd option.` });
            }
        }
    }
    const port = Number(env.ANDROID_ADB_SERVER_PORT ?? 5037);
    if (Number.isInteger(port) && port > 0 && port < 65536) {
        const list = await (options.devices ?? adbDevices)(port);
        if (!list.running) {
            checks.push({ name: 'android/devices', status: 'ok', message: 'The adb server is not running, so attached devices were not counted; doctor does not start it.' });
        }
        else if (!list.devices) {
            checks.push({ name: 'android/devices', status: 'warning', message: 'The adb server did not return its device list.', fix: 'run adb devices to see its state' });
        }
        else {
            const count = (state, emulators) => list.devices.filter(device => device.state === state && (emulators === undefined || device.emulator === emulators)).length;
            const unauthorized = count('unauthorized'), offline = count('offline');
            const message = `${count('device', true)} emulator${count('device', true) === 1 ? '' : 's'} and ${count('device', false)} physical device${count('device', false) === 1 ? '' : 's'} attached and ready` +
                `${unauthorized ? `; ${unauthorized} unauthorized` : ''}${offline ? `; ${offline} offline` : ''}. Serials are not shown.`;
            checks.push(unauthorized || offline
                ? { name: 'android/devices', status: 'warning', message, fix: unauthorized ? 'unlock the device and accept its USB debugging prompt, then rerun 9l doctor --android' : 'reconnect the offline device or restart its emulator' }
                : { name: 'android/devices', status: 'ok', message });
        }
    }
    else {
        checks.push({ name: 'android/devices', status: 'warning', message: 'ANDROID_ADB_SERVER_PORT is not a port number, so attached devices were not counted.', fix: 'unset ANDROID_ADB_SERVER_PORT or set it to the adb server port' });
    }
    const prefix = found.toolsSource === 'NINELIVES_ANDROID_TOOLS' ? '"$NINELIVES_ANDROID_TOOLS"' : `~/${android_env_1.androidToolsDirectory}`;
    const where = found.toolsSource === 'NINELIVES_ANDROID_TOOLS' ? 'NINELIVES_ANDROID_TOOLS' : `~/${android_env_1.androidToolsDirectory}`;
    const hint = found.toolsSource === 'NINELIVES_ANDROID_TOOLS' ? '; 9l run passes it to tests only with --pass-env NINELIVES_ANDROID_TOOLS' : '';
    if (!found.toolsRoot) {
        checks.push({ name: 'android/appium', status: 'missing', message: 'No tooling directory: NINELIVES_ANDROID_TOOLS and HOME are unset.', fix: 'export NINELIVES_ANDROID_TOOLS to a directory, then ' + (0, android_env_1.androidToolsInstall)('"$NINELIVES_ANDROID_TOOLS"') });
    }
    else {
        const files = (0, android_env_1.androidToolingPaths)(found.toolsRoot);
        for (const [name, label, pkg, pinned, file] of [
            ['android/appium', 'Appium', 'appium', android_env_1.androidVersions.appium, files.appium],
            ['android/uiautomator2', 'UiAutomator2 driver', 'appium-uiautomator2-driver', android_env_1.androidVersions.uiautomator2, files.uiautomator2],
        ]) {
            const installed = manifest(file, pkg);
            const entry = pkg !== 'appium' || exists(files.appiumEntry, 'file');
            if (installed.version === pinned && entry) {
                checks.push({ name, status: 'ok', message: `${label} ${pinned} (pinned) in ${where}${at(found.toolsRoot)}${hint}.` });
            }
            else {
                const state = !installed.present ? 'is not installed' : !installed.version ? 'has an unreadable package.json' : installed.version !== pinned ? `is ${installed.version}, not the pinned ${pinned}; the driver rejects it at open` : 'is incomplete (no index.js)';
                checks.push({ name, status: 'missing', message: `${label} ${state} in ${where}${at(found.toolsRoot)}.`, fix: (0, android_env_1.androidToolsInstall)(prefix) });
            }
        }
    }
    return { checks, unverified: ['Android emulator boot, system images and AVD configuration', 'Device authorization, lock state and installed app builds', 'Appium server start and UiAutomator2 sessions'] };
}
