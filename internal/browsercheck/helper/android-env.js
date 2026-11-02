"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.androidToolsInstall = exports.androidToolsDirectory = exports.androidBuildTools = exports.androidVersions = void 0;
exports.androidSdkCandidates = androidSdkCandidates;
exports.androidDefaults = androidDefaults;
exports.androidToolPaths = androidToolPaths;
exports.androidToolingPaths = androidToolingPaths;
// Android SDK and pinned tooling discovery. One implementation serves the
// driver's defaults (@9l/playwright/android) and `9l doctor --android`:
// packages/playwright/src/android-env.ts is the source and
// packages/browser-checks/src/android-env.ts is a byte-identical copy that a
// test keeps in step. Discovery only reads the environment and checks that
// directories exist; it never runs a tool, downloads or writes anything.
const node_fs_1 = require("node:fs");
const node_os_1 = require("node:os");
const node_path_1 = require("node:path");
// Paths follow the platform being described, so a Windows SDK root keeps its
// separators and drive and a macOS or Linux candidate stays POSIX wherever
// the code runs; by default that is the host's own platform.
const paths = (platform) => platform === 'win32' ? node_path_1.win32 : node_path_1.posix;
/** The Appium server and UiAutomator2 driver releases the adapter is qualified with. */
exports.androidVersions = { appium: '3.8.0', uiautomator2: '8.7.0' };
/** The build-tools release whose aapt2 and apksigner the adapter runs. */
exports.androidBuildTools = '35.0.0';
/** Where the pinned tooling is looked for when NINELIVES_ANDROID_TOOLS is unset, under the home directory. */
exports.androidToolsDirectory = '.9l/android-tools';
/** The install command for the pinned tooling into a prefix directory. */
const androidToolsInstall = (prefix) => `npm install --prefix ${prefix} --save-exact --ignore-scripts appium@${exports.androidVersions.appium} appium-uiautomator2-driver@${exports.androidVersions.uiautomator2}`;
exports.androidToolsInstall = androidToolsInstall;
const directory = (path) => { try {
    return (0, node_fs_1.statSync)(path).isDirectory();
}
catch {
    return false;
} };
const set = (value) => typeof value === 'string' && value.trim() !== '' ? value : undefined;
/** Common SDK locations in the order they are tried after ANDROID_HOME and
 * ANDROID_SDK_ROOT: the Homebrew command-line tools under HOMEBREW_PREFIX and
 * the standard Homebrew prefixes, then the Android Studio default for the
 * platform. `brew` itself is not run. */
function androidSdkCandidates(options = {}) {
    const env = options.env ?? process.env, platform = options.platform ?? process.platform, { join } = paths(platform);
    const home = options.home ?? (set(platform === 'win32' ? env.USERPROFILE : env.HOME) ?? (0, node_os_1.homedir)());
    const prefixes = [set(env.HOMEBREW_PREFIX), '/opt/homebrew', '/usr/local', '/home/linuxbrew/.linuxbrew'].filter((p) => p !== undefined);
    const candidates = platform === 'win32' ? [] :
        [...new Set(prefixes)].map(prefix => ({ path: join(prefix, 'share/android-commandlinetools'), source: 'homebrew' }));
    if (platform === 'darwin')
        candidates.push({ path: join(home, 'Library/Android/sdk'), source: 'android-studio' });
    else if (platform === 'win32') {
        const local = set(env.LOCALAPPDATA);
        if (local)
            candidates.push({ path: join(local, 'Android/Sdk'), source: 'android-studio' });
    }
    else
        candidates.push({ path: join(home, 'Android/Sdk'), source: 'android-studio' });
    return candidates;
}
/** The SDK root and pinned-tooling directory the driver uses when its options
 * omit them. ANDROID_HOME wins, then ANDROID_SDK_ROOT, then the first common
 * location that exists. A variable that is set is used as given, even if the
 * directory is missing, so a misconfiguration is reported rather than hidden.
 * The tooling directory is NINELIVES_ANDROID_TOOLS, else ~/.9l/android-tools.
 * Relative values resolve against the current directory. */
function androidDefaults(options = {}) {
    const env = options.env ?? process.env, exists = options.isDirectory ?? directory;
    const platform = options.platform ?? process.platform, { join, resolve } = paths(platform);
    const home = options.home ?? (set(platform === 'win32' ? env.USERPROFILE : env.HOME) ?? (0, node_os_1.homedir)());
    const result = {};
    const androidHome = set(env.ANDROID_HOME), sdkRoot = set(env.ANDROID_SDK_ROOT);
    if (androidHome) {
        Object.assign(result, { sdkRoot: resolve(androidHome), sdkSource: 'ANDROID_HOME' });
        if (sdkRoot && resolve(sdkRoot) !== result.sdkRoot)
            result.sdkRootIgnored = true;
    }
    else if (sdkRoot) {
        Object.assign(result, { sdkRoot: resolve(sdkRoot), sdkSource: 'ANDROID_SDK_ROOT' });
    }
    else {
        const found = androidSdkCandidates({ ...options, env, home, platform }).find(candidate => exists(candidate.path));
        if (found)
            Object.assign(result, { sdkRoot: found.path, sdkSource: found.source });
    }
    const tools = set(env.NINELIVES_ANDROID_TOOLS);
    if (tools)
        Object.assign(result, { toolsRoot: resolve(tools), toolsSource: 'NINELIVES_ANDROID_TOOLS' });
    else if (home)
        Object.assign(result, { toolsRoot: join(home, exports.androidToolsDirectory), toolsSource: 'default' });
    return result;
}
/** The SDK executables the driver runs, under one SDK root. */
function androidToolPaths(sdkRoot, platform = process.platform) {
    const windows = platform === 'win32', { join } = paths(platform);
    const buildTools = join(sdkRoot, 'build-tools', exports.androidBuildTools);
    return {
        adb: join(sdkRoot, 'platform-tools', windows ? 'adb.exe' : 'adb'),
        emulator: join(sdkRoot, 'emulator', windows ? 'emulator.exe' : 'emulator'),
        buildTools,
        aapt2: join(buildTools, windows ? 'aapt2.exe' : 'aapt2'),
        apksigner: join(buildTools, windows ? 'apksigner.bat' : 'apksigner'),
    };
}
/** The pinned packages' manifests and the Appium entry point under a tooling directory. */
function androidToolingPaths(toolsRoot, platform = process.platform) {
    const { join } = paths(platform);
    return {
        appium: join(toolsRoot, 'node_modules/appium/package.json'),
        appiumEntry: join(toolsRoot, 'node_modules/appium/index.js'),
        uiautomator2: join(toolsRoot, 'node_modules/appium-uiautomator2-driver/package.json'),
    };
}
