import { createRequire } from 'node:module';
import { readFileSync, statSync } from 'node:fs';
import path from 'node:path';

export type Diagnostic = { name: string; status: 'ok' | 'warning' | 'missing'; message: string; fix?: string };
// Engine is what the CLI admits (protocol and capabilities) and can request
// (prove fault kinds); an SDK declares the same three under "9l" in its
// package.json, so the pair is judged without loading either.
export type Engine = { protocol: string; capabilities: string[]; faults: string[] };
const qualified = new Set(['1.61.1', '1.62.0', '1.62.1', '1.63.0', '1.64.0']);
const safeVersion = /^\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?$/;
const safeToken = /^[a-z0-9][a-z0-9./-]{0,63}$/;

function names(value: unknown): string[] | undefined {
  return Array.isArray(value) && value.length <= 32 && value.every(item => typeof item === 'string' && safeToken.test(item)) ? value : undefined;
}

// The declared engine contract, or undefined for a package that predates it
// or declares it badly. Only bounded, well-formed tokens are kept.
function declaredEngine(data: { [key: string]: unknown }): Engine | undefined {
  const declared = data['9l'];
  if (!declared || typeof declared !== 'object') return undefined;
  const { protocol, capabilities, faults } = declared as { [key: string]: unknown };
  const engine = { protocol, capabilities: names(capabilities), faults: names(faults) };
  return typeof engine.protocol === 'string' && safeToken.test(engine.protocol) && engine.capabilities && engine.faults ? engine as Engine : undefined;
}

// Read only package metadata, never project config or environment values.
function metadata(load: NodeRequire, name: string): { version: string; directory: string; engine?: Engine } | undefined {
  try {
    let directory = path.dirname(load.resolve(name));
    for (;;) {
      const file = path.join(directory, 'package.json');
      try {
        if (statSync(file).size <= 256 * 1024) {
          const data = JSON.parse(readFileSync(file, 'utf8'));
          if (data.name === name && typeof data.version === 'string' && data.version.length <= 80 && safeVersion.test(data.version)) {
            const engine = declaredEngine(data);
            return engine ? { version: data.version, directory, engine } : { version: data.version, directory };
          }
        }
      } catch { /* Continue through package entry subdirectories. */ }
      const parent = path.dirname(directory);
      if (parent === directory) return undefined;
      directory = parent;
    }
  } catch { return undefined; }
}

export function browserInstallCommand(browser: string, root = process.cwd()): string {
  const load = createRequire(path.join(root, 'package.json'));
  const installed = metadata(load, 'playwright') ?? metadata(load, '@playwright/test');
  return installed ? `npx --yes playwright@${installed.version} install ${browser}` : `npx playwright install ${browser}`;
}

export function diagnose(args: { cliVersion?: string; sdk?: boolean; engine?: Engine }, root = process.cwd()) {
  const checks: Diagnostic[] = [];
  const major = Number(process.versions.node.split('.')[0]);
  // Below the minimum is a defect with an install step. Any other version
  // outside the qualified one is supported but untested; never suggest a
  // downgrade for it.
  checks.push({ name: 'node', status: major < 22 ? 'missing' : major === 24 ? 'ok' : 'warning',
    message: `Node ${process.versions.node}; minimum 22, qualified on 24.${major >= 22 && major !== 24 ? ' Supported but outside the qualified matrix; report problems with the output of 9l doctor --format json.' : ''}`,
    ...(major < 22 ? { fix: 'Install Node 24 LTS, then restart your terminal and MCP host.' } : {}) });
  const load = createRequire(path.join(root, 'package.json'));
  let manifest = false;
  try { manifest = statSync(path.join(root, 'package.json')).isFile(); } catch { /* Missing project. */ }
  checks.push({ name: 'workspace', status: manifest ? 'ok' : 'missing', message: manifest ? 'package.json exists; project config has not been executed.' : 'No package.json in the current directory.',
    ...(!manifest ? { fix: 'Run 9l doctor from your application project directory.' } : {}) });
  const test = metadata(load, '@playwright/test');
  const testRuntime = test ? metadata(createRequire(path.join(test.directory, 'package.json')), 'playwright') ?? test : undefined;
  const runtime = metadata(load, 'playwright') ?? testRuntime;
  checks.push({ name: 'playwright-test', status: !test ? 'missing' : qualified.has(test.version) ? 'ok' : 'warning',
    message: test ? `@playwright/test ${test.version}${qualified.has(test.version) ? ' is qualified.' : ' is outside the qualified matrix.'}` : '@playwright/test is not installed locally.',
    ...(!test ? { fix: 'npm install --save-dev @playwright/test@1.64.0' } : {}) });
  const sdk = metadata(load, '@9l/playwright');
  const align = args.cliVersion && safeVersion.test(args.cliVersion) ? { fix: `npm install --save-dev @9l/playwright@${args.cliVersion}` } : {};
  if (!sdk) {
    checks.push({ name: 'sdk', status: args.sdk ? 'missing' : 'warning', message: '@9l/playwright is absent; ordinary run, scan and inspect do not require it.', ...(args.sdk ? align : {}) });
  } else {
    const pair = `@9l/playwright ${sdk.version}; CLI ${args.cliVersion}.`;
    if (sdk.version === args.cliVersion) {
      checks.push({ name: 'sdk', status: 'ok', message: `${pair} Matching releases are qualified together.` });
    } else if (!sdk.engine || !args.engine) {
      checks.push({ name: 'sdk', status: 'warning', message: `${pair} The SDK does not declare its engine protocol, so admission is only known at run time; matching releases are qualified together.`, ...align });
    } else if (sdk.engine.protocol === args.engine.protocol && sdk.engine.capabilities.join(',') === args.engine.capabilities.join(',')) {
      // The same handshake run --sdk accepts, so the pair runs; it is not the qualified one.
      checks.push({ name: 'sdk', status: 'warning', message: `${pair} Admitted: protocol ${sdk.engine.protocol} and capabilities match, so run --sdk, prove and confirm accept this SDK. Unqualified: matching releases are qualified together.`, ...align });
    } else {
      checks.push({ name: 'sdk', status: args.sdk ? 'missing' : 'warning', message: `${pair} Incompatible: the SDK declares protocol ${sdk.engine.protocol} with ${sdk.engine.capabilities.length} capabilities; this CLI admits ${args.engine.protocol} with ${args.engine.capabilities.length}. run --sdk, prove and confirm reject this pair.`, ...align });
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
  const runtimes = new Map<string, NonNullable<typeof runtime>>();
  if (runtime) runtimes.set('scan/inspect', runtime);
  if (testRuntime) runtimes.set('run', testRuntime);
  for (const [mode, installed] of runtimes) {
    const fix = `npx --yes playwright@${installed.version} install chromium`;
    try {
      const core = metadata(createRequire(path.join(installed.directory, 'package.json')), 'playwright-core');
      if (!core || !qualified.has(installed.version) || core.version !== installed.version) throw new Error('unqualified registry');
      // This is Playwright's installed registry, not application config. Restrict
      // its private API to the versions in our qualification matrix.
      const registry = require(path.join(core.directory, 'lib/coreBundle.js')).registry.registry;
      for (const name of ['chromium', 'chromium-headless-shell']) {
        const executable = registry.findExecutable(name)?.executablePath();
        let present = false;
        try { present = typeof executable === 'string' && statSync(executable).isFile(); } catch { /* Missing build. */ }
        checks.push({ name: `${mode}/${name}`, status: present ? 'ok' : 'missing',
          message: `${name} for Playwright ${installed.version}: ${present ? 'installed' : 'missing'}.`, ...(!present ? { fix } : {}) });
      }
    } catch {
      checks.push({ name: `${mode}/browser`, status: 'warning', message: 'Browser files could not be verified for this package version.', fix });
    }
  }
  return { version: 1, cliVersion: args.cliVersion, platform: `${process.platform}/${process.arch}`,
    ready: !checks.some(check => check.status === 'missing' || check.name.endsWith('/browser')), checks,
    unverified: ['Browser launch and Linux system libraries', 'Project config, target reachability and authentication', 'MCP host binary selection and agent/provider login'],
    next: 'Start your app, then run: 9l scan http://localhost:<port> --allow-private-network. No account is required.' };
}
