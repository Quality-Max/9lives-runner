"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.browserInstallCommand = browserInstallCommand;
exports.proveProject = proveProject;
exports.specChecks = specChecks;
exports.diagnose = diagnose;
const node_module_1 = require("node:module");
const node_fs_1 = require("node:fs");
const node_path_1 = __importDefault(require("node:path"));
const source_text_1 = require("./source-text");
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
    const self = typeof found.self === 'string' ? ` This executable: ${found.self}.` : '';
    const other = typeof found.other === 'string' ? ` First on PATH: ${found.other}.` : '';
    if (found.selfUnknown)
        return { name: 'executable', status: 'warning', message: "this executable's own path is unknown, so it was not compared with the 9l on PATH." };
    if (!found.otherOnPath) {
        return { name: 'executable', status: 'warning',
            message: `no 9l on PATH; terminals, scripts and MCP hosts configured with plain 9l cannot start it.${self}`,
            fix: "add this executable's directory to PATH, or configure the host with this executable's full path" };
    }
    if (found.relative) {
        return { name: 'executable', status: 'warning',
            message: `the 9l first on PATH ${found.samePath ? 'is' : 'is not'} this executable, found through a relative PATH entry that resolves against the current directory; a host started elsewhere finds another 9l or none.${self}${other}`,
            fix: "replace the relative PATH entry with an absolute directory, or configure the host with this executable's full path" };
    }
    if (found.samePath)
        return { name: 'executable', status: 'ok', message: `this executable is the 9l first on PATH.${self}${other}` };
    return { name: 'executable', status: 'warning',
        message: `the 9l first on PATH is not this executable; terminals, scripts and MCP hosts start that one unless configured with a full path.${self}${other}`,
        fix: "put this build's directory first on PATH, or configure the host with this executable's full path" };
}
const specLimit = 1024 * 1024;
const configLimit = 256 * 1024;
const configExtensions = ['ts', 'js', 'mts', 'mjs', 'cts', 'cjs'];
const sdkModule = '@9l/playwright';
const testModule = '@playwright/test';
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
function nearestConfigIn(directory) {
    for (const extension of configExtensions) {
        const candidate = node_path_1.default.join(directory, `playwright.config.${extension}`);
        try {
            if ((0, node_fs_1.statSync)(candidate).isFile())
                return candidate;
        }
        catch { /* Try the next name. */ }
    }
    return undefined;
}
function declaresPlaywright(manifest) {
    const read = readBounded(manifest, configLimit);
    try {
        const data = JSON.parse(read.text ?? '');
        return Boolean(data?.dependencies?.[testModule] || data?.devDependencies?.[testModule]);
    }
    catch {
        return false;
    }
}
// The config and project prove uses for a spec, by the SDK adapter's walk:
// the nearest playwright.config.* above the spec, in the adapter's extension
// order, and the directory of that config, or else the nearest package.json
// that declares @playwright/test. Files are read as text, never executed.
function proveProject(spec) {
    let config;
    let project;
    for (let directory = node_path_1.default.dirname(spec);; directory = node_path_1.default.dirname(directory)) {
        config ??= nearestConfigIn(directory);
        if (!project && declaresPlaywright(node_path_1.default.join(directory, 'package.json')))
            project = directory;
        if (node_path_1.default.dirname(directory) === directory)
            break;
    }
    return { config, project: config ? node_path_1.default.dirname(config) : project };
}
function importCheck(name, code, bindings, paths) {
    const local = (module) => paths ? module : 'another module';
    const test = bindings.find(binding => binding.local === 'test');
    // Tests registered through a binding from @playwright/test are not
    // instrumented, even when the SDK is imported too, say for its expect.
    const plain = bindings.filter(binding => binding.module === testModule);
    const plainRegisters = plain.some(binding => binding.imported === 'test' ? (0, source_text_1.calls)(code, binding.local) : ['*', 'default'].includes(binding.imported) && (0, source_text_1.readsTest)(code, binding.local));
    const mixed = { name: 'spec/import', status: 'warning', message: `${name} takes test from @9l/playwright but also registers tests with test from @playwright/test, which prove does not instrument; which tests use which is not verified.` };
    if (test?.module === sdkModule)
        return plainRegisters ? mixed : { name: 'spec/import', status: 'ok', message: `${name} imports test from @9l/playwright.` };
    // `import { test as base }` then `base.extend`, or `n9l.test` through a namespace.
    if (bindings.some(binding => binding.module === sdkModule && ['test', '*', 'default'].includes(binding.imported))) {
        return plainRegisters ? mixed : { name: 'spec/import', status: 'ok', message: `${name} takes test from @9l/playwright under another name.` };
    }
    if (test && test.module === testModule && test.imported === 'test') {
        // Another module could still provide the test the spec runs: a `test`
        // export, a local fixtures module, or a namespace or default import the
        // spec reads `.test` from or calls.
        const other = bindings.find(binding => binding.module !== testModule && (binding.imported === 'test'
            || (['*', 'default'].includes(binding.imported) && (/^[./]/.test(binding.module) || (0, source_text_1.readsTest)(code, binding.local) || (binding.imported === 'default' && (0, source_text_1.calls)(code, binding.local, false))))));
        if (other) {
            return { name: 'spec/import', status: 'warning', message: `${name} imports test from @playwright/test and also imports from ${local(other.module)}, which is not followed; whether its tests come from @9l/playwright is not verified.` };
        }
        return { name: 'spec/import', status: 'missing',
            message: `${name} imports test from @playwright/test; prove would run its baseline uninstrumented and exit 1 because no browser context was instrumented.`,
            fix: 'import test from @9l/playwright; prove instruments only its page and context fixtures' };
    }
    if (test) {
        return { name: 'spec/import', status: 'warning', message: `${name} imports test from ${local(test.module)}; fixture modules are not followed, so whether that test comes from @9l/playwright is not verified.` };
    }
    return { name: 'spec/import', status: 'warning', message: `${name} has no test import doctor can read from @9l/playwright, @playwright/test or a fixtures module; not verified.` };
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
        const scanned = (0, source_text_1.scan)(source.text ?? '');
        const bindings = (0, source_text_1.importBindings)(scanned);
        checks.push(importCheck(name, scanned.code, bindings, paths));
        const fixtures = (0, source_text_1.callbackFixtures)(scanned, bindings);
        checks.push(fixtures.has('page') || fixtures.has('context')
            ? { name: 'spec/fixtures', status: 'ok', message: `${name} takes the page or context fixture in a test or hook callback.` }
            : { name: 'spec/fixtures', status: 'warning', message: `${name}: no destructured page or context parameter found in a test or hook callback; prove faults reach only fetch and XHR made through those fixtures.` });
    }
    const { config } = proveProject(spec);
    if (!config) {
        checks.push({ name: 'spec/config', status: 'warning', message: 'no playwright.config.* above the spec; prove runs it with Playwright defaults.', fix: 'add a playwright.config.ts above the spec, or pass prove --config' });
        return checks;
    }
    const found = `config found: ${node_path_1.default.basename(config)}${paths ? ` in ${node_path_1.default.dirname(config)}` : ''}.`;
    const contents = readBounded(config, configLimit);
    if (contents.reason !== 'ok') {
        checks.push({ name: 'spec/config', status: 'ok', message: `${found} testDir not verified: the config is too large to read.` });
        return checks;
    }
    const { code, literals } = (0, source_text_1.scan)(contents.text ?? '');
    // `projects: […]`, shorthand `{ projects }` or `config.projects = …`.
    if (/(?<![\w$])projects(?![\w$])/.test(code)) {
        // A project without its own testDir inherits the config's, so with
        // projects only executing the config tells which one selects the spec.
        checks.push({ name: 'spec/config', status: 'ok', message: `${found} testDir not verified: the config defines projects, so which one selects the spec is only known by executing it.` });
        return checks;
    }
    if ((0, source_text_1.mergesValues)(code)) {
        checks.push({ name: 'spec/config', status: 'ok', message: `${found} testDir not verified: the config spreads or merges values that can set testDir or projects, so only executing it tells which applies.` });
        return checks;
    }
    // A whole string literal: the next token after it ends the property, so
    // `'./tests' + suffix` or a template is not read as `./tests`.
    const values = [];
    let computed = false;
    for (const match of code.matchAll(/(?:^|[^.\w$])testDir\s*:\s*/g)) {
        const literal = literals.get((match.index ?? 0) + match[0].length);
        if (literal && literal.text.length <= 512 && /^\s*(?:[,}]|$)/.test(code.slice(literal.end, literal.end + 64)))
            values.push(literal.text);
        else
            computed = true;
    }
    if (values.length !== 1 || computed) {
        const why = values.length + (computed ? 1 : 0) > 1 ? 'the config sets testDir more than once, so which applies is only known by executing it' : 'only a literal testDir can be read without executing the config';
        checks.push({ name: 'spec/config', status: 'ok', message: `${found} testDir not verified: ${why}.` });
        return checks;
    }
    const literal = values[0];
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
    const spec = typeof args.spec === 'string' && node_path_1.default.isAbsolute(args.spec) ? args.spec : undefined;
    let load;
    if (spec) {
        // Judge the project prove would use for this spec, not the current
        // directory: the adapter resolves the config, project and installed
        // packages by walking up from the spec.
        const { project } = proveProject(spec);
        load = (0, node_module_1.createRequire)(node_path_1.default.join(node_path_1.default.dirname(spec), 'package.json'));
        checks.push(project
            ? { name: 'workspace', status: 'ok', message: `prove runs the spec from its project${args.paths === true ? ` ${project}` : ''}; project config has not been executed.` }
            : { name: 'workspace', status: 'missing', message: 'No playwright.config.* or package.json declaring @playwright/test above the spec, so prove cannot run it.',
                fix: 'add @playwright/test to the package.json of the project that holds the spec' });
    }
    else {
        load = (0, node_module_1.createRequire)(node_path_1.default.join(root, 'package.json'));
        let manifest = false;
        try {
            manifest = (0, node_fs_1.statSync)(node_path_1.default.join(root, 'package.json')).isFile();
        }
        catch { /* Missing project. */ }
        checks.push({ name: 'workspace', status: manifest ? 'ok' : 'missing', message: manifest ? 'package.json exists; project config has not been executed.' : 'No package.json in the current directory.',
            ...(!manifest ? { fix: 'Run 9l doctor from your application project directory.' } : {}) });
    }
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
    if (spec)
        checks.push(...specChecks(spec, args.paths === true));
    return { version: 1, cliVersion: args.cliVersion, platform: `${process.platform}/${process.arch}`,
        ready: !checks.some(check => check.status === 'missing' || check.name.endsWith('/browser')), checks,
        unverified: ['Browser launch and Linux system libraries', 'Project config, target reachability and authentication', 'MCP host binary selection and agent/provider login',
            ...(spec ? ['testDir and testMatch unless literal'] : [])],
        next: 'Start your app, then run: 9l scan http://localhost:<port> --allow-private-network. No account is required.' };
}
