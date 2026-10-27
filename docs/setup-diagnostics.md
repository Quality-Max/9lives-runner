# First-run diagnostics

`9l doctor` is available from CLI v0.4.8. Run it from your application
project:

```sh
9l doctor
9l doctor --sdk --format json
9l doctor --sdk tests/checkout.spec.ts   # also check one spec for prove
```

The command checks which `9l` PATH selects, Node (22 minimum, 24
qualified), `package.json` presence, locally resolved `@playwright/test`, the
optional SDK, and the exact Chromium and headless-shell files needed by each
installed Playwright runtime. Scan/inspect and test runs can resolve
different runtimes; both are checked. It prints package versions and fixed
diagnostic messages, never environment values, config contents, browser-cache
paths or authentication state. File system paths appear only with `--paths`,
a flag for local reading in the spirit of `prove --paths`; nothing doctor
prints is persisted.

Exit 0 means no required prerequisite is missing; warnings and unverified
checks still apply. Exit 2 means setup is missing or diagnostics could not
complete. `--sdk` makes the otherwise optional SDK required and, with a spec
path after it, adds the [spec checks](#check-a-spec-for-prove).

The `executable` check compares the file doctor runs from with the `9l` that
PATH resolves to, without running either. Usually they are the same file.
When they differ, terminals, scripts and MCP hosts start the one on PATH
unless configured with a full path, so the check warns and names the fix:
put this build's directory first on PATH, or configure the host with this
executable's full path. It also warns when no `9l` is on PATH at all, since
hosts configured with plain `9l` cannot start it, and when a relative PATH
entry (such as `.` or an empty entry) found it: that entry resolves against
the current directory, so a host started elsewhere finds another `9l` or none.
Without `--paths` the report says only which case applies; `--paths` adds both
resolved, absolute paths. Doctor cannot see which binary a running MCP host
selected; that stays unverified.

Node below 22 is missing and gets the install step. Node 24 is qualified. Any
other release from 22 up is supported but outside the qualified matrix: a
warning with no install step, since it is not a reason to downgrade.

The SDK pair is judged by what the installed `@9l/playwright` declares under
`"9l"` in its `package.json`: engine protocol, capabilities and prove fault
kinds. That is the same contract `run --sdk` checks in the worker's handshake,
read here without loading the package. A matching release is `ok`. A
different release that declares the protocol and capabilities this CLI admits
is *admitted but unqualified*: a warning, with the install command for the
matching release as the next step. A different protocol or capability set is
incompatible: `run --sdk`, `prove` and `confirm` reject it, so under `--sdk`
it is a missing prerequisite. An SDK released before 0.5.0 declares nothing,
and its admission is only known at run time. Under `--sdk`, `sdk/faults`
lists the prove fault kinds the SDK applies and any this CLI can request that
it does not.
Browser-file checks use Playwright's registry only for qualified versions
1.61.1, 1.62.0, 1.62.1, 1.63.0 and 1.64.0. An unknown registry cannot report ready.

Doctor does not download anything, make network requests, launch a browser,
load application config, log in, or write project files. Its embedded helper
uses an owned temporary directory which is removed afterward, a ten-second
budget, and the same restricted environment as browser checks. It resolves
installed packages and uses Playwright's registry code. Use trusted local
dependencies. It does not establish that config is valid, that Linux libraries
are installed, or that a target is reachable. A real scan verifies those paths.

## Check a spec for prove

```sh
9l doctor --sdk tests/checkout.spec.ts
```

With `--sdk`, one spec path adds three checks read from the file as text, at
most 1 MiB. Nothing is executed: not the spec, not the config, and local
fixture modules the spec imports are not followed. The `workspace`,
`playwright-test` and `sdk` checks then describe the project `prove` would use
for that spec, found by the same walk up from the spec as the SDK adapter (the
directory of the nearest `playwright.config.*`, or else the nearest
`package.json` declaring `@playwright/test`), rather than the current
directory.

- `spec/import`: reads the names the spec's static imports and requires
  bind. `ok` when `test` comes from `@9l/playwright`, including
  `import { test as base }`, `require('@9l/playwright').test`, a dynamic
  `await import()` and a namespace import such as `n9l.test`. `missing` only
  when the binding `test` comes from `@playwright/test` and nothing else in
  the file could provide it: `prove` would then run its baseline
  uninstrumented and exit 1 because no browser context was instrumented, so
  the report is not ready and doctor exits 2. Anything that could provide
  the test instead makes it a warning: a `test` export from another module,
  a default or namespace import from a local module, or one from a package
  that the spec reads `.test` from or calls, such as `fx.test(…)`. So does a
  named import the spec uses the way a test function is used, such as `it`
  from a fixtures module that exports `base.extend(…)`: called with a title
  and a callback outside any test or hook body (`it('x', async ({ page }) =>
  …)`, `it('x', { tag }, fn)`), or through `describe`, a hook or `extend`
  (`it.describe(…)`, `it.beforeEach(…)`, `base.extend(…)`), or `it.only('x',
  fn)` and the like. The callback may be inline or the name of a function
  the spec declares once, `function body({ page }) {}` or `const body =
  async ({ page }) => {}`, with or without a type annotation such as
  `const body: (f: { page: Page }) => Promise<void> = …`. A named function
  expression's own name, `const body = async function body() {}`, is bound
  only inside it and declares nothing outside. Helpers called any other way, such as
  `login(page)`, `login('admin', page)`, a member call `api.it(…)` however
  spaced, a step wrapper `step('x', fn)` inside a test, or
  `expect.extend(…)`, do not count, so a spec that only adds them to the
  plain `test` is still `missing`. A local alias or extension of the plain
  `test`, such as `const t = test.extend({ … })`, is the plain `test`, so a
  step wrapper inside its callbacks does not count either, nor does one
  inside a function the spec passes by name to the plain `test`, an alias or
  a hook, such as `test('x', body)` or `test.beforeEach(seed)`; a function
  passed to `describe` registers tests and is not a test body. Type
  arguments such as `test.extend<{ get: () => string }>(…)` do not change
  either reading. Doctor reads arguments, annotations and type arguments
  only within a bound; a call it reads only in part, such as one behind a
  very long comment, is not verified and counts as a possible
  registration, never as proof that there is none. A spec that takes the SDK under another name but also
  registers tests through a binding from `@playwright/test`, such as
  `n9l.expect` with a plain `test(…)`, is a warning too, since those tests
  are uninstrumented. So are `test as pwTest` from `@playwright/test` and a
  file doctor cannot read an import from: not verified, never missing.
- `spec/fixtures`: `ok` when the first parameter of a `test` or `test.*` hook
  callback, or of such a named test function as `it`, or of a local alias or
  extension of either, destructures `page` or `context`, whether the
  callback is inline or a function the spec declares and passes by name,
  whatever follows it
  (`async ({ page }, testInfo)`, a type annotation, nested patterns).
  Otherwise a warning that no such parameter was found, because prove faults
  reach only fetch and XHR made through those fixtures. Other functions, such
  as `.map(({ page }) => page)`, do not count.
- `spec/config`: names the nearest `playwright.config.{ts,js,mts,mjs,cts,cjs}`
  above the spec, in that order, which is the config `prove` picks without
  `--config`. When the config contains exactly one literal `testDir: '…'`
  as a whole property value, the check reports whether the spec is under it
  (`ok`) or outside it (a warning, with `prove --config` as the way out). A
  computed `testDir`, several, or any config that names `projects` in any
  form, including the shorthand `{ projects }` (a project without its own
  `testDir` inherits the config's), is reported as not verified. So is a
  config that spreads or merges values doctor cannot read, such as
  `{ testDir: './tests', ...shared }`, `defineConfig(base, { … })` or
  `Object.assign`, since those can set `testDir` or `projects`; a spread
  into `use` or an array does not count. `testMatch` is never read. No
  config above the spec is a warning.

The spec and config are read as text by a lexical scan that sets aside
comments and the contents of string, template and regex literals, so a
commented-out import, import-looking text inside a string, or a `/*` or quote
inside a regex does not change a verdict. It is not a parser, so unusual
syntax can still be misread; an import it cannot read is reported as a
warning, never as missing.

Messages name basenames only; `--paths` adds the project and config
directories, the literal `testDir` and the module a `test` was imported from. "testDir and testMatch unless literal" is listed as
unverified whenever a spec was given. Passing a spec without `--sdk`, or a
path that is not a file, is a usage error.

## Apply a repair explicitly

Node below 22: install Node 24 LTS and restart the terminal and MCP host.
Missing test runner: `npm install --save-dev @playwright/test@1.64.0`.
Missing browser: use the version-pinned command doctor prints, for example:

```sh
npx --yes playwright@1.64.0 install chromium
# On Linux, if system libraries are also missing:
npx --yes playwright@1.64.0 install --with-deps chromium
```

These commands download packages/browsers; `--with-deps` can install OS
packages. Run them only as an explicit setup action. Doctor never runs them.
Use your project package manager to keep its lockfile conventions.

## Get a useful result without an account

Start your application with its existing development command. Substitute its
published port below; this command deliberately permits that loopback origin:

```sh
9l scan http://localhost:3000 --allow-private-network
9l inspect http://localhost:3000 --allow-private-network
```

For Docker Compose, publish the application port and use `localhost:<port>`.
The opt-in does not permit Compose service names or arbitrary private hosts.
If the target is unreachable, the CLI and MCP tools ask you to start the app
and check its URL/port without echoing navigation logs or query parameters.
For authenticated pages, use an explicitly selected workspace-relative
`--storage-state` with `--acknowledge-private-content`; MCP uses
`storage_state_path` and `acknowledge_private_content:true`. See
[authenticated inspection](mcp.md) before creating or sharing state.

Ordinary Playwright specs work with `9l run tests/example.spec.ts`; they do
not need an SDK import, provider or platform account. Doctor does not prove
that a spec's assertions passed. Use the execution receipt for that.

## Qualification and onboarding timing

Automated checks cover an empty workspace, missing Node, package/config
non-disclosure, installed and absent Chromium/headless-shell builds, a stale
`9l` on PATH that is reported but never run, spec checks for the import,
fixture and `testDir` forms the tests name, and shared CLI/MCP
unreachable-target guidance. CI requires real browser checks
on Linux and native Windows, and checks the registry across every qualified
Playwright version. Treat a CI configuration as planned coverage until its
run succeeds. Local execution evidence and its actual platform are recorded
in the implementing PR; this is not a clean-machine qualification of all
supported platforms.

For each consented pilot, record OS/architecture, Node/CLI/SDK/Playwright
versions and two elapsed times: (1) clean-machine setup including downloads,
and (2) first command after prerequisites to the first useful scan result.
Do not retain page content, credentials or raw config. The target median for
(2) is under five minutes. A local fixture timing does not establish a pilot
median. Clean-profile macOS/Linux/Windows onboarding and the pilot median
remain unmeasured until actual runs are recorded.
