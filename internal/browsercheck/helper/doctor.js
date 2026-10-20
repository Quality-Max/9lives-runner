"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.browserInstallCommand = browserInstallCommand;
exports.diagnose = diagnose;
const node_module_1 = require("node:module");
const node_fs_1 = require("node:fs");
const node_path_1 = __importDefault(require("node:path"));
const qualified = new Set(['1.61.1', '1.62.0', '1.62.1', '1.63.0', '1.64.0']);
const safeVersion = /^\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?$/;
// Read only package metadata, never project config or environment values.
function metadata(load, name) {
    try {
        let directory = node_path_1.default.dirname(load.resolve(name));
        for (;;) {
            const file = node_path_1.default.join(directory, 'package.json');
            try {
                if ((0, node_fs_1.statSync)(file).size <= 256 * 1024) {
                    const data = JSON.parse((0, node_fs_1.readFileSync)(file, 'utf8'));
                    if (data.name === name && typeof data.version === 'string' && data.version.length <= 80 && safeVersion.test(data.version)) {
                        return { version: data.version, directory };
                    }
                }
            }
            catch { /* Continue through package entry subdirectories. */ }
            const parent = node_path_1.default.dirname(directory);
            if (parent === directory)
                return undefined;
            directory = parent;
        }
    }
    catch {
        return undefined;
    }
}
function browserInstallCommand(browser, root = process.cwd()) {
    const load = (0, node_module_1.createRequire)(node_path_1.default.join(root, 'package.json'));
    const installed = metadata(load, 'playwright') ?? metadata(load, '@playwright/test');
    return installed ? `npx --yes playwright@${installed.version} install ${browser}` : `npx playwright install ${browser}`;
}
function diagnose(args, root = process.cwd()) {
    const checks = [];
    const major = Number(process.versions.node.split('.')[0]);
    checks.push({ name: 'node', status: major < 22 ? 'missing' : major === 24 ? 'ok' : 'warning',
        message: `Node ${process.versions.node}; minimum 22, qualified on 24.`,
        ...(major !== 24 ? { fix: 'Install Node 24 LTS, then restart your terminal and MCP host.' } : {}) });
    const load = (0, node_module_1.createRequire)(node_path_1.default.join(root, 'package.json'));
    let manifest = false;
    try {
        manifest = (0, node_fs_1.statSync)(node_path_1.default.join(root, 'package.json')).isFile();
    }
    catch { /* Missing project. */ }
    checks.push({ name: 'workspace', status: manifest ? 'ok' : 'missing', message: manifest ? 'package.json exists; project config has not been executed.' : 'No package.json in the current directory.',
        ...(!manifest ? { fix: 'Run 9l doctor from your application project directory.' } : {}) });
    const test = metadata(load, '@playwright/test');
    const testRuntime = test ? metadata((0, node_module_1.createRequire)(node_path_1.default.join(test.directory, 'package.json')), 'playwright') ?? test : undefined;
    const runtime = metadata(load, 'playwright') ?? testRuntime;
    checks.push({ name: 'playwright-test', status: !test ? 'missing' : qualified.has(test.version) ? 'ok' : 'warning',
        message: test ? `@playwright/test ${test.version}${qualified.has(test.version) ? ' is qualified.' : ' is outside the qualified matrix.'}` : '@playwright/test is not installed locally.',
        ...(!test ? { fix: 'npm install --save-dev @playwright/test@1.64.0' } : {}) });
    const sdk = metadata(load, '@9l/playwright');
    checks.push({ name: 'sdk', status: !sdk ? (args.sdk ? 'missing' : 'warning') : sdk.version === args.cliVersion ? 'ok' : 'warning',
        message: sdk ? `@9l/playwright ${sdk.version}; CLI ${args.cliVersion}. Matching releases are qualified together.` : '@9l/playwright is absent; ordinary run, scan and inspect do not require it.',
        ...(!sdk && args.sdk && args.cliVersion && safeVersion.test(args.cliVersion) ? { fix: `npm install --save-dev @9l/playwright@${args.cliVersion}` } : {}) });
    // Check both resolution paths: scan prefers playwright, run uses @playwright/test.
    const runtimes = new Map();
    if (runtime)
        runtimes.set('scan/inspect', runtime);
    if (testRuntime)
        runtimes.set('run', testRuntime);
    for (const [mode, installed] of runtimes) {
        const fix = `npx --yes playwright@${installed.version} install chromium`;
        try {
            const core = metadata((0, node_module_1.createRequire)(node_path_1.default.join(installed.directory, 'package.json')), 'playwright-core');
            if (!core || !qualified.has(installed.version) || core.version !== installed.version)
                throw new Error('unqualified registry');
            // This is Playwright's installed registry, not application config. Restrict
            // its private API to the versions in our qualification matrix.
            const registry = require(node_path_1.default.join(core.directory, 'lib/coreBundle.js')).registry.registry;
            for (const name of ['chromium', 'chromium-headless-shell']) {
                const executable = registry.findExecutable(name)?.executablePath();
                let present = false;
                try {
                    present = typeof executable === 'string' && (0, node_fs_1.statSync)(executable).isFile();
                }
                catch { /* Missing build. */ }
                checks.push({ name: `${mode}/${name}`, status: present ? 'ok' : 'missing',
                    message: `${name} for Playwright ${installed.version}: ${present ? 'installed' : 'missing'}.`, ...(!present ? { fix } : {}) });
            }
        }
        catch {
            checks.push({ name: `${mode}/browser`, status: 'warning', message: 'Browser files could not be verified for this package version.', fix });
        }
    }
    return { version: 1, cliVersion: args.cliVersion, platform: `${process.platform}/${process.arch}`,
        ready: !checks.some(check => check.status === 'missing' || check.name.endsWith('/browser')), checks,
        unverified: ['Browser launch and Linux system libraries', 'Project config, target reachability and authentication', 'MCP host binary selection and agent/provider login'],
        next: 'Start your app, then run: 9l scan http://localhost:<port> --allow-private-network. No account is required.' };
}
