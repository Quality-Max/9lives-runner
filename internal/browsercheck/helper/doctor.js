"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.browserInstallCommand = browserInstallCommand;
exports.specChecks = specChecks;
exports.diagnose = diagnose;
const node_module_1 = require("node:module");
const node_fs_1 = require("node:fs");
const node_path_1 = __importDefault(require("node:path"));
const qualified = new Set(['1.61.1', '1.62.0', '1.62.1', '1.63.0', '1.64.0']);
const safeVersion = /^\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?$/;
const safeToken = /^[a-z0-9][a-z0-9./-]{0,63}$/;
function names(value) {
    return Array.isArray(value) && value.length <= 32 && value.every(item => typeof item === 'string' && safeToken.test(item)) ? value : undefined;
}
// The declared engine contract, or undefined for a package that predates it
// or declares it badly. Only bounded, well-formed tokens are kept.
function declaredEngine(data) {
    const declared = data['9l'];
    if (!declared || typeof declared !== 'object')
        return undefined;
    const { protocol, capabilities, faults } = declared;
    const engine = { protocol, capabilities: names(capabilities), faults: names(faults) };
    return typeof engine.protocol === 'string' && safeToken.test(engine.protocol) && engine.capabilities && engine.faults ? engine : undefined;
}
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
                        const engine = declaredEngine(data);
                        return engine ? { version: data.version, directory, engine } : { version: data.version, directory };
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
function executableCheck(found) {
    if (!found.otherOnPath)
        return { name: 'executable', status: 'ok', message: 'no other 9l on PATH.' };
    if (found.samePath)
        return { name: 'executable', status: 'ok', message: 'this executable is the 9l first on PATH.' };
    const where = typeof found.self === 'string' && typeof found.other === 'string' ? ` This executable: ${found.self}. First on PATH: ${found.other}.` : '';
    return { name: 'executable', status: 'warning',
        message: `the 9l first on PATH is not this executable; terminals, scripts and MCP hosts start that one unless configured with a full path.${where}`,
        fix: "put this build's directory first on PATH, or configure the host with this executable's full path" };
}
const specLimit = 1024 * 1024;
const configLimit = 256 * 1024;
const configExtensions = ['ts', 'js', 'mts', 'mjs', 'cts', 'cjs'];
// Static import forms only: `import { test } from '<module>'` and
// `const { test } = require('<module>')`, single or double quotes. Clauses are
// bounded so a large file cannot make the match expensive.
const importForm = /\bimport\s+(\{[^}]{0,512}\})\s+from\s+(['"])(@9l\/playwright|@playwright\/test)\2/g;
const requireForm = /\b(?:const|let|var)\s+(\{[^}]{0,512}\})\s*=\s*require\(\s*(['"])(@9l\/playwright|@playwright\/test)\2\s*\)/g;
const fixtureForm = /\(\s*\{[^}]{0,512}?\b(?:page|context)\b/;
const testDirForm = /\btestDir\s*:\s*(['"])([^'"\n]{1,512})\1/;
// The modules the spec takes `test` from, by name, without evaluating it.
function testImports(source) {
    const modules = new Set();
    for (const form of [importForm, requireForm]) {
        form.lastIndex = 0;
        for (let match = form.exec(source); match; match = form.exec(source)) {
            if (/\btest\b/.test(match[1]))
                modules.add(match[3]);
        }
    }
    return modules;
}
// Read a regular file as text when it is within the limit; otherwise say why not.
function readBounded(file, limit) {
    try {
        const info = (0, node_fs_1.statSync)(file);
        if (!info.isFile())
            return { reason: 'missing' };
        if (info.size > limit)
            return { reason: 'large' };
        return { text: (0, node_fs_1.readFileSync)(file, 'utf8'), reason: 'ok' };
    }
    catch {
        return { reason: 'missing' };
    }
}
// The nearest playwright.config.* above the spec, in the order the SDK adapter
// tries them, so doctor names the config prove picks without --config.
function nearestConfig(spec) {
    for (let directory = node_path_1.default.dirname(spec);; directory = node_path_1.default.dirname(directory)) {
        for (const extension of configExtensions) {
            const candidate = node_path_1.default.join(directory, `playwright.config.${extension}`);
            try {
                if ((0, node_fs_1.statSync)(candidate).isFile())
                    return candidate;
            }
            catch { /* Try the next name. */ }
        }
        if (node_path_1.default.dirname(directory) === directory)
            return undefined;
    }
}
// Spec readiness for prove: the import, the instrumented fixtures and the
// config that will select the spec. Everything is read as text; neither the
// spec nor any config is executed, and messages name only basenames unless
// the CLI asked for paths.
function specChecks(spec, paths = false) {
    const checks = [];
    const name = node_path_1.default.basename(spec);
    const source = readBounded(spec, specLimit);
    if (source.reason === 'missing') {
        checks.push({ name: 'spec', status: 'missing', message: `${name} is not a readable file.`, fix: 'pass the path of one Playwright spec file after --sdk' });
    }
    else if (source.reason === 'large') {
        checks.push({ name: 'spec', status: 'warning', message: `${name}: spec too large to inspect (over 1 MiB); the import and fixture checks were skipped.` });
    }
    else {
        const modules = testImports(source.text ?? '');
        if (modules.has('@9l/playwright')) {
            checks.push({ name: 'spec/import', status: 'ok', message: `${name} imports test from @9l/playwright.` });
        }
        else if (modules.has('@playwright/test')) {
            checks.push({ name: 'spec/import', status: 'missing', message: `${name} imports test from @playwright/test; prove refuses it.`, fix: 'import test from @9l/playwright; prove instruments only its page and context fixtures' });
        }
        else {
            checks.push({ name: 'spec/import', status: 'warning', message: `${name} has no test import from @9l/playwright or @playwright/test; a test imported through a local fixtures module is not followed.` });
        }
        checks.push(fixtureForm.test(source.text ?? '')
            ? { name: 'spec/fixtures', status: 'ok', message: `${name} uses the page or context fixture.` }
            : { name: 'spec/fixtures', status: 'warning', message: `prove faults reach only fetch and XHR made through page or context; this spec uses neither.` });
    }
    const config = nearestConfig(spec);
    if (!config) {
        checks.push({ name: 'spec/config', status: 'warning', message: 'no playwright.config.* above the spec; prove runs it with Playwright defaults.', fix: 'add a playwright.config.ts above the spec, or pass prove --config' });
        return checks;
    }
    const found = `config found: ${node_path_1.default.basename(config)}${paths ? ` in ${node_path_1.default.dirname(config)}` : ''}.`;
    const contents = readBounded(config, configLimit);
    const literal = contents.text?.match(testDirForm)?.[2];
    if (!literal) {
        checks.push({ name: 'spec/config', status: 'ok', message: `${found} testDir not verified: only a literal testDir can be read without executing the config.` });
        return checks;
    }
    const testDir = node_path_1.default.resolve(node_path_1.default.dirname(config), literal);
    const relative = node_path_1.default.relative(testDir, spec);
    const inside = relative !== '' && !relative.startsWith('..') && !node_path_1.default.isAbsolute(relative);
    const named = paths ? ` (${literal})` : '';
    checks.push(inside
        ? { name: 'spec/config', status: 'ok', message: `${found} The spec is under its literal testDir${named}.` }
        : { name: 'spec/config', status: 'warning', message: `${found} The spec is outside its literal testDir${named}, so prove will not select it.`, fix: 'move the spec under that testDir, or pass prove --config with a config that includes it' });
    return checks;
}
function diagnose(args, root = process.cwd()) {
    const checks = [];
    if (args.executable && typeof args.executable === 'object')
        checks.push(executableCheck(args.executable));
    const major = Number(process.versions.node.split('.')[0]);
    // Below the minimum is a defect with an install step. Any other version
    // outside the qualified one is supported but untested; never suggest a
    // downgrade for it.
    checks.push({ name: 'node', status: major < 22 ? 'missing' : major === 24 ? 'ok' : 'warning',
        message: `Node ${process.versions.node}; minimum 22, qualified on 24.${major >= 22 && major !== 24 ? ' Supported but outside the qualified matrix; report problems with the output of 9l doctor --format json.' : ''}`,
        ...(major < 22 ? { fix: 'Install Node 24 LTS, then restart your terminal and MCP host.' } : {}) });
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
    const align = args.cliVersion && safeVersion.test(args.cliVersion) ? { fix: `npm install --save-dev @9l/playwright@${args.cliVersion}` } : {};
    if (!sdk) {
        checks.push({ name: 'sdk', status: args.sdk ? 'missing' : 'warning', message: '@9l/playwright is absent; ordinary run, scan and inspect do not require it.', ...(args.sdk ? align : {}) });
    }
    else {
        const pair = `@9l/playwright ${sdk.version}; CLI ${args.cliVersion}.`;
        const declared = sdk.engine && args.engine ? { sdk: sdk.engine, cli: args.engine } : undefined;
        const admitted = declared && declared.sdk.protocol === declared.cli.protocol && declared.sdk.capabilities.join(',') === declared.cli.capabilities.join(',');
        if (declared && !admitted) {
            // A declared mismatch is decisive whatever the versions say: a linked or
            // locally rebuilt SDK can keep a release's version with another contract.
            checks.push({ name: 'sdk', status: args.sdk ? 'missing' : 'warning', message: `${pair} Incompatible: the SDK declares protocol ${declared.sdk.protocol} with ${declared.sdk.capabilities.length} capabilities; this CLI admits ${declared.cli.protocol} with ${declared.cli.capabilities.length}. run --sdk, prove and confirm reject this pair.`, ...align });
        }
        else if (sdk.version === args.cliVersion) {
            checks.push({ name: 'sdk', status: 'ok', message: `${pair} Matching releases are qualified together.` });
        }
        else if (!declared) {
            checks.push({ name: 'sdk', status: 'warning', message: `${pair} The SDK does not declare its engine protocol, so admission is only known at run time; matching releases are qualified together.`, ...align });
        }
        else {
            // The same handshake run --sdk accepts, so the pair runs; it is not the qualified one.
            checks.push({ name: 'sdk', status: 'warning', message: `${pair} Admitted: protocol ${declared.sdk.protocol} and capabilities match, so run --sdk, prove and confirm accept this SDK. Unqualified: matching releases are qualified together.`, ...align });
        }
        if (args.sdk && args.engine) {
            // prove refuses --faults kinds the SDK cannot apply; say which up front.
            const unsupported = args.engine.faults.filter(kind => !sdk.engine?.faults.includes(kind));
            checks.push({ name: 'sdk/faults', status: !sdk.engine ? 'warning' : unsupported.length ? 'warning' : 'ok',
                message: !sdk.engine ? 'The SDK does not declare its prove fault kinds; prove learns them from the baseline run.'
                    : unsupported.length ? `prove fault kinds this SDK applies: ${sdk.engine.faults.join(', ')}. Not applied by this SDK: ${unsupported.join(', ')}.`
                        : `prove fault kinds this SDK applies: ${sdk.engine.faults.join(', ')}.` });
        }
    }
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
    const spec = typeof args.spec === 'string' && node_path_1.default.isAbsolute(args.spec) ? args.spec : undefined;
    if (spec)
        checks.push(...specChecks(spec, args.paths === true));
    return { version: 1, cliVersion: args.cliVersion, platform: `${process.platform}/${process.arch}`,
        ready: !checks.some(check => check.status === 'missing' || check.name.endsWith('/browser')), checks,
        unverified: ['Browser launch and Linux system libraries', 'Project config, target reachability and authentication', 'MCP host binary selection and agent/provider login',
            ...(spec ? ['testDir and testMatch unless literal'] : [])],
        next: 'Start your app, then run: 9l scan http://localhost:<port> --allow-private-network. No account is required.' };
}
