# First-run diagnostics

`9l doctor` is available from CLI v0.4.8. Run it from your application
project:

```sh
9l doctor
9l doctor --sdk --format json
```

The command checks Node (22 minimum, 24 qualified), `package.json` presence,
locally resolved `@playwright/test`, the optional SDK, and the exact
Chromium and headless-shell files needed by each installed Playwright runtime.
Scan/inspect and test runs can resolve different runtimes; both are checked.
It prints package versions and fixed diagnostic messages, never environment
values, config contents, browser-cache paths or authentication state.

Exit 0 means no required prerequisite is missing; warnings and unverified
checks still apply. Exit 2 means setup is missing or diagnostics could not
complete. `--sdk` makes the otherwise optional SDK required.

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
it is a missing prerequisite. An SDK released before 0.4.10 declares nothing,
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
non-disclosure, installed and absent Chromium/headless-shell builds, and
shared CLI/MCP unreachable-target guidance. CI requires real browser checks
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
