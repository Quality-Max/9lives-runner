"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.iosVersions = void 0;
exports.parseWdaSource = parseWdaSource;
exports.inspectIosSource = inspectIosSource;
exports.renderIosInspectReport = renderIosInspectReport;
exports.bootedSimulators = bootedSimulators;
exports.boundedBody = boundedBody;
exports.inspectIos = inspectIos;
// `9l inspect --ios`: how testable the foreground app on a booted iOS
// Simulator is. It starts the pinned Appium/XCUITest server, opens a session
// on the already booted simulator without naming an app (so nothing is
// installed, launched, reset or terminated for the app), reads one
// WebDriverAgent page source and ends the session. WebDriverAgent itself is
// installed on the simulator and stays there, as after any Appium session.
//
// Matching mirrors the 9l iOS driver: a test ID is an accessibility
// identifier, found with `name == id AND label != id`. WebDriverAgent reports
// `name` as the identifier, or the label when there is none, so an element
// whose name equals its label is label-only, even if it carries an
// identifier equal to its label; the driver refuses that case too.
const node_child_process_1 = require("node:child_process");
const node_fs_1 = require("node:fs");
const promises_1 = require("node:fs/promises");
const node_net_1 = require("node:net");
const node_os_1 = require("node:os");
const node_path_1 = require("node:path");
const promises_2 = require("node:timers/promises");
const inspect_screen_1 = require("./inspect-screen");
/** The Appium and XCUITest driver releases the iOS driver is qualified with;
 * packages/playwright/src/ios-runtime.ts is the source, a test keeps them equal. */
exports.iosVersions = { appium: '3.8.0', xcuitest: '10.43.1' };
const udidPattern = /^[A-Fa-f0-9]{8}(-[A-Fa-f0-9]{4}){3}-[A-Fa-f0-9]{12}$/;
const testIdPattern = /^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}$/;
const maxSourceBytes = 8 << 20;
const interactiveTypes = new Set(['Button', 'Link', 'Cell', 'Switch', 'Toggle', 'TextField', 'SecureTextField', 'SearchField', 'TextView',
    'Slider', 'Stepper', 'PickerWheel', 'Tab', 'MenuItem', 'SegmentedControl', 'ColorWell', 'DatePicker'].map((type) => `XCUIElementType${type}`));
const entities = { amp: '&', lt: '<', gt: '>', quot: '"', apos: "'" };
const decode = (value) => value.replace(/&(#x[0-9a-fA-F]+|#[0-9]+|amp|lt|gt|quot|apos);/g, (_, name) => {
    if (name[0] !== '#')
        return entities[name];
    const code = name[1] === 'x' ? Number.parseInt(name.slice(2), 16) : Number.parseInt(name.slice(1), 10);
    return code <= 0x10ffff ? String.fromCodePoint(code) : '';
});
/** The elements of a WebDriverAgent XML page source, in document order. */
function parseWdaSource(xml) {
    if (!/<XCUIElementTypeApplication\b/.test(xml))
        throw new Error('the simulator returned no app hierarchy');
    const elements = [];
    const open = [];
    for (const tag of xml.matchAll(/<(XCUIElementType\w+)((?:\s+[\w:-]+="[^"]*")*)\s*(\/?)>|<\/XCUIElementType\w+\s*>/g)) {
        if (tag[1] === undefined) {
            open.pop();
            continue;
        }
        const attributes = new Map();
        for (const [, name, value] of tag[2].matchAll(/([\w:-]+)="([^"]*)"/g))
            attributes.set(name, decode(value));
        const text = (name) => attributes.get(name) ?? '';
        elements.push({
            type: tag[1], name: text('name'), label: text('label'), value: text('value'), placeholder: text('placeholderValue'),
            enabled: text('enabled') === 'true', visible: text('visible') === 'true',
            bounds: `[${text('x')},${text('y')}][${text('width')}x${text('height')}]`, parent: open.at(-1) ?? -1,
        });
        if (tag[3] !== '/')
            open.push(elements.length - 1);
    }
    return elements;
}
const identifierOf = (element) => element.name && element.name !== element.label ? element.name : undefined;
const idFix = 'Give it an accessibility identifier: Semantics(identifier:) in Flutter, .accessibilityIdentifier in SwiftUI, accessibilityIdentifier in UIKit, testID in React Native.';
/** Rank the visible controls of a page source by the handle 9l would find them with. */
function inspectIosSource(xml) {
    const all = parseWdaSource(xml);
    const apps = [...new Set(all.filter((element) => element.type === 'XCUIElementTypeApplication').map((element) => element.label || element.name).filter(Boolean))];
    const children = all.map(() => []);
    all.forEach((element, index) => { if (element.parent >= 0)
        children[element.parent].push(index); });
    // The on-screen keyboard's keys are not the app's controls.
    const inKeyboard = (index) => index >= 0 && (all[index].type === 'XCUIElementTypeKeyboard' || inKeyboard(all[index].parent));
    const innerLabel = (index) => {
        for (const child of children[index]) {
            const element = all[child];
            if (element.type === 'XCUIElementTypeStaticText' && element.label)
                return element.label;
            const nested = interactiveTypes.has(element.type) ? '' : innerLabel(child);
            if (nested)
                return nested;
        }
        return '';
    };
    const isControl = (index) => interactiveTypes.has(all[index].type) && all[index].visible && !inKeyboard(index);
    const holdsControl = (index) => children[index].some((child) => isControl(child) || holdsControl(child));
    const controls = [];
    for (const [index, element] of all.entries()) {
        if (!isControl(index))
            continue;
        // An unnamed list row around other controls: the controls inside are
        // what a test reaches, and they are ranked. Fields and buttons are
        // always ranked, even when they hold an accessory such as a clear button.
        if (element.type === 'XCUIElementTypeCell' && !element.name && !element.label && holdsControl(index))
            continue;
        // Only the label, a field's placeholder or an inner static text: a
        // field's value is what was typed and is never read.
        const label = (0, inspect_screen_1.shortLabel)(element.label || element.placeholder || innerLabel(index));
        const base = { className: element.type, label, bounds: element.bounds };
        const id = identifierOf(element);
        if (id && testIdPattern.test(id)) {
            const shared = all.filter((other) => identifierOf(other) === id).length;
            controls.push(shared === 1
                ? { ...base, identifier: id, stability: 'stable', locator: `app.getByTestId(${JSON.stringify(id)})` }
                : { ...base, identifier: id, stability: 'repeated', locator: `app.getByTestId(${JSON.stringify(id)})`,
                    note: `${shared} elements share this identifier, so getByTestId fails as ambiguous while they are all shown. Give each instance its own identifier, for example with the item's key as a suffix.` });
        }
        else if (id) {
            controls.push({ ...base, identifier: id, stability: 'none',
                note: `Its identifier is not a 9l test ID (letters, digits, _ . - only, at most 64 characters, not starting with . or -). ${idFix}` });
        }
        else if (element.label || element.placeholder) {
            controls.push({ ...base, stability: 'copy',
                note: `It has an accessibility label but no identifier (or one equal to its label). The 9l iOS driver matches identifiers only, so a 9l test cannot reach it; label-based locators in other tools break when the copy or the device language changes. ${idFix}` });
        }
        else {
            controls.push({ ...base, stability: 'none', note: `It has no identifier and no accessibility label of its own; only coordinates reach it. ${idFix}` });
        }
    }
    const warnings = [];
    if (controls.length === 0)
        warnings.push(`No visible controls in ${all.length} elements. The screen may still be loading, or its controls may not be exposed to accessibility.`);
    if (all.some((element) => element.type === 'XCUIElementTypeSecureTextField'))
        warnings.push('A secure text field is on screen; its value is never reported.');
    const testability = (0, inspect_screen_1.testabilityOf)(controls, 'have only a label, which the 9l iOS driver cannot match');
    return { platform: 'ios', apps, elementCount: all.length, ...(0, inspect_screen_1.rankControls)(controls), testability, warnings };
}
function renderIosInspectReport(result, opts = {}) {
    return (0, inspect_screen_1.renderScreenInspectReport)(result, opts);
}
const run = (file, args, timeoutMs) => {
    const result = (0, node_child_process_1.spawnSync)(file, args, { encoding: 'utf8', timeout: timeoutMs, killSignal: 'SIGKILL', maxBuffer: 8 << 20, windowsHide: true });
    return { status: result.error ? null : result.status, stdout: result.stdout ?? '' };
};
/** The UDIDs of booted iOS simulators, from `xcrun simctl list devices booted -j`. */
function bootedSimulators(json) {
    const devices = JSON.parse(json).devices ?? {};
    return Object.entries(devices).filter(([runtime]) => runtime.includes('SimRuntime.iOS-'))
        .flatMap(([, list]) => list).filter((device) => device.state === 'Booted' && typeof device.udid === 'string' && udidPattern.test(device.udid))
        .map((device) => device.udid);
}
async function freePort() {
    const server = (0, node_net_1.createServer)();
    await new Promise((yes, no) => { server.once('error', no); server.listen(0, '127.0.0.1', yes); });
    const port = server.address().port;
    await new Promise((yes, no) => server.close((error) => (error ? no(error) : yes())));
    return port;
}
/** A response body read in chunks and cancelled once it passes the size
 * limit, counted in bytes, so an oversized page source is never held whole. */
async function boundedBody(response, limit = maxSourceBytes) {
    const reader = response.body?.getReader();
    if (!reader)
        return '';
    const chunks = [];
    let size = 0;
    for (;;) {
        const { done, value } = await reader.read();
        if (done)
            break;
        size += value.byteLength;
        if (size > limit) {
            await reader.cancel();
            throw new Error(`the page source exceeds ${Math.round(limit / (1 << 20))} MiB`);
        }
        chunks.push(value);
    }
    return Buffer.concat(chunks).toString('utf8');
}
/** Read the page source of a booted simulator's foreground app through a
 * private Appium server. The server's process group is stopped and its
 * scratch directory removed on every path. */
async function appiumSource(udid, tools, wdaProducts, deadline) {
    // Beneath the directory the 9l binary owns and removes after it has stopped
    // this process tree, so a cancellation that skips `finally` leaves nothing.
    const owned = process.env.NINELIVES_HELPER_SCRATCH;
    const directory = await (0, promises_1.mkdtemp)((0, node_path_1.join)(owned && (0, node_path_1.isAbsolute)(owned) ? owned : (0, node_os_1.tmpdir)(), '9l-inspect-ios-'));
    let server;
    let base = '', session = '';
    const remaining = () => {
        const left = deadline - Date.now();
        if (left <= 0)
            throw new Error('the iOS inspection ran out of time; WebDriverAgent may still have been starting. Pass --wda-products from scripts/build-ios-wda.cjs, or raise --timeout');
        return left;
    };
    const call = async (path, method = 'GET', body) => {
        const response = await fetch(base + path, { method, redirect: 'error', signal: AbortSignal.timeout(remaining()),
            headers: { 'content-type': 'application/json' }, ...(body === undefined ? {} : { body: JSON.stringify(body) }) });
        const text = await boundedBody(response);
        const data = JSON.parse(text);
        if (!response.ok || data.value?.error)
            throw new Error(`Appium refused ${method} ${path.replace(/\/session\/[^/]+/, '/session')}: ${String(data.value?.message ?? response.status).split('\n')[0].slice(0, 300)}`);
        return data.value;
    };
    try {
        const env = { PATH: process.env.PATH, HOME: process.env.HOME, DEVELOPER_DIR: process.env.DEVELOPER_DIR, APPIUM_HOME: directory, TMPDIR: directory };
        await (0, promises_1.mkdir)((0, node_path_1.join)(directory, 'node_modules'));
        for (const name of ['appium', 'appium-xcuitest-driver'])
            await (0, promises_1.symlink)((0, node_path_1.join)(tools, 'node_modules', name), (0, node_path_1.join)(directory, 'node_modules', name), 'dir');
        await (0, promises_1.writeFile)((0, node_path_1.join)(directory, 'package.json'), JSON.stringify({ private: true, dependencies: { appium: exports.iosVersions.appium, 'appium-xcuitest-driver': exports.iosVersions.xcuitest } }));
        if (wdaProducts)
            await (0, promises_1.cp)(wdaProducts, (0, node_path_1.join)(directory, 'wda/Build/Products'), { recursive: true, verbatimSymlinks: true });
        const appiumPort = await freePort(), wdaPort = await freePort(), mjpegPort = await freePort();
        base = `http://127.0.0.1:${appiumPort}`;
        server = (0, node_child_process_1.spawn)(process.execPath, [(0, node_path_1.join)(tools, 'node_modules/appium/index.js'), '--address', '127.0.0.1', '--port', String(appiumPort),
            '--use-drivers', 'xcuitest', '--log-level', 'error', '--log-no-colors'], { env, cwd: directory, stdio: 'ignore', detached: true });
        let exited = false;
        server.once('exit', () => { exited = true; });
        server.once('error', () => { exited = true; });
        for (;;) {
            remaining();
            if (exited)
                throw new Error('the Appium server exited while starting');
            try {
                if ((await call('/status'))?.ready === true)
                    break;
            }
            catch { /* not listening yet */ }
            await (0, promises_2.setTimeout)(150);
        }
        const created = await call('/session', 'POST', { capabilities: { alwaysMatch: {
                    platformName: 'iOS', 'appium:automationName': 'XCUITest', 'appium:udid': udid,
                    // No app or bundle: WebDriverAgent reads whatever app is in front, and
                    // nothing is installed, launched, reset or terminated for it.
                    'appium:noReset': true, 'appium:fullReset': false, 'appium:isHeadless': true,
                    'appium:skipLogCapture': true, 'appium:showXcodeLog': false, 'appium:printPageSourceOnFindFailure': false,
                    'appium:derivedDataPath': (0, node_path_1.join)(directory, 'wda'), 'appium:useNewWDA': true, ...(wdaProducts ? { 'appium:usePrebuiltWDA': true } : {}),
                    'appium:wdaLocalPort': wdaPort, 'appium:mjpegServerPort': mjpegPort, 'appium:wdaBindingIP': '127.0.0.1',
                    'appium:wdaLaunchTimeout': Math.min(240_000, remaining()), 'appium:wdaStartupRetries': 1,
                    'appium:disableAutomaticScreenshots': true, 'appium:newCommandTimeout': 60,
                }, firstMatch: [{}] } });
        if (typeof created?.sessionId !== 'string' || !/^[A-Za-z0-9-]{1,128}$/.test(created.sessionId) || created.capabilities?.udid !== udid)
            throw new Error('Appium opened a session on another device');
        session = created.sessionId;
        const source = await call(`/session/${session}/source`);
        if (typeof source !== 'string')
            throw new Error('the simulator returned no app hierarchy');
        return source;
    }
    finally {
        // Bounded well inside the margin the 9l binary leaves after the deadline.
        if (session) {
            try {
                await fetch(`${base}/session/${session}`, { method: 'DELETE', signal: AbortSignal.timeout(2_000) });
            }
            catch { /* the server is stopped next */ }
        }
        if (server?.pid) {
            for (const signal of ['SIGTERM', 'SIGKILL']) {
                try {
                    process.kill(-server.pid, signal);
                }
                catch { /* already gone */ }
                if (signal === 'SIGTERM')
                    await (0, promises_2.setTimeout)(300);
            }
        }
        await (0, promises_1.rm)(directory, { recursive: true, force: true });
    }
}
/** Inspect the foreground app of the booted simulator. */
async function inspectIos(args, deps = {}) {
    if ((deps.platform ?? process.platform) !== 'darwin')
        throw new Error('iOS inspection needs macOS with Xcode');
    if (args.udid !== undefined && !udidPattern.test(args.udid))
        throw new Error('udid is not a simulator UDID');
    const wdaProducts = args.wdaProducts ?? (process.env.NINELIVES_IOS_WDA_PRODUCTS?.trim() || undefined);
    if (wdaProducts !== undefined && (!(0, node_path_1.isAbsolute)(wdaProducts) || !(0, node_fs_1.existsSync)((0, node_path_1.join)(wdaProducts, 'Debug-iphonesimulator/WebDriverAgentRunner-Runner.app')))) {
        throw new Error('wda products must be the absolute Build/Products directory that scripts/build-ios-wda.cjs produced');
    }
    const timeoutMs = Math.min(Math.max(args.timeoutMs ?? 300_000, 5_000), 600_000);
    const deadline = Date.now() + timeoutMs;
    const exec = deps.run ?? run;
    const listed = exec('/usr/bin/xcrun', ['simctl', 'list', 'devices', 'booted', '-j'], 30_000);
    if (listed.status !== 0)
        throw new Error('xcrun simctl failed; install Xcode and set DEVELOPER_DIR to it');
    const booted = bootedSimulators(listed.stdout);
    let udid = args.udid;
    if (udid && !booted.includes(udid))
        throw new Error('that simulator is not booted');
    if (!udid) {
        if (booted.length !== 1)
            throw new Error(booted.length ? `${booted.length} simulators are booted; choose one with --udid (xcrun simctl list devices booted lists them)` : 'no iOS simulator is booted; boot one with xcrun simctl boot');
        udid = booted[0];
    }
    const tools = deps.toolsRoot ?? (process.env.NINELIVES_IOS_TOOLS?.trim() || (0, node_path_1.join)((0, node_os_1.homedir)(), '.9l', 'ios-tools'));
    for (const [name, version] of [['appium', exports.iosVersions.appium], ['appium-xcuitest-driver', exports.iosVersions.xcuitest]]) {
        let installed;
        try {
            installed = JSON.parse((0, node_fs_1.readFileSync)((0, node_path_1.join)(tools, 'node_modules', name, 'package.json'), 'utf8')).version;
        }
        catch {
            installed = undefined;
        }
        if (installed !== version) {
            throw new Error(`${name} ${version} is not installed in ${tools}; install the pinned iOS tools there (npm ci --prefix tools/ios --ignore-scripts in a 9lives checkout) and set NINELIVES_IOS_TOOLS`);
        }
    }
    const source = await (deps.readSource ?? appiumSource)(udid, tools, wdaProducts, deadline);
    return inspectIosSource(source);
}
