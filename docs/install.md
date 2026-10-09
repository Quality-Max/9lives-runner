# Install the CLI and Playwright SDK

The published npm package is [@9l/playwright 0.1.1](https://www.npmjs.com/package/@9l/playwright).
It supplies TypeScript fixtures and the reporter. Install the Go `9l` CLI
separately; the SDK does not bundle the runner or a browser.

## SDK

From your Playwright project, with Node 24 (Node 22 minimum):

```sh
npm install --save-dev @9l/playwright@0.1.1 @playwright/test@1.64.0
npm exec playwright install chromium
```

On Linux use `npm exec playwright install --with-deps chromium`.
The SDK accepts `@playwright/test` `>=1.61.1 <2`. Qualified versions:
every published release from 1.61.1 through 1.64.0 (1.61.1, 1.62.0, 1.62.1, 1.63.0 and 1.64.0), with Chromium. Later versions in that range install but are unqualified,
as are other engines. Preserve the existing project configuration and assertions.
The [SDK contract](sdk-bridge.md) describes supported settings and limits.

## CLI from source

With Go 1.25.13 or newer:

```sh
go install github.com/Quality-Max/9lives-runner/cmd/9l@latest
export PATH="$(go env GOPATH)/bin:$PATH"
9l --version
```

If you have set a custom `GOBIN`, add that directory to `PATH` instead.
For an exact reviewed source revision, replace `@latest` with its commit or
published CLI version. The public Go install path has been exercised.

## Prebuilt CLI releases

The [GitHub Releases page](https://github.com/Quality-Max/9lives-runner/releases)
is the download location. The first binary release is prepared by the release
workflow; until its assets appear, install from source above. Download a
specific version and the matching `SHA256SUMS` file. Go is not needed to run
these executables.

| System | Archive |
| --- | --- |
| macOS, Apple Silicon | `9l-darwin-arm64.tar.gz` |
| macOS, Intel | `9l-darwin-amd64.tar.gz` |
| Linux, x86-64 | `9l-linux-amd64.tar.gz` |
| Linux, ARM64 | `9l-linux-arm64.tar.gz` |

For the published `v0.1.1`, using GitHub CLI, this example installs on Apple
Silicon. Choose your archive from the table:

```sh
mkdir -p /tmp/9l-download
cd /tmp/9l-download
BUNDLE=9l-darwin-arm64
gh release download v0.1.1 --repo Quality-Max/9lives-runner --pattern "$BUNDLE.tar.gz" --pattern SHA256SUMS
awk -v archive="$BUNDLE.tar.gz" '$2 == archive' SHA256SUMS > selected.sha256
test -s selected.sha256
shasum -a 256 -c selected.sha256
tar -xzf "$BUNDLE.tar.gz"
mkdir -p "$HOME/.local/bin"
cp "$BUNDLE/9l" "$HOME/.local/bin/9l"
export PATH="$HOME/.local/bin:$PATH"
9l --version
```

On Linux use `sha256sum -c selected.sha256` for checksum verification. Keep the
included `LICENSE` and `NOTICE`. macOS archives are not Apple notarized; the
checksums verify downloaded bytes against the release's checksum file.

Windows is not a declared release target. macOS/Linux amd64 and arm64 are the
runner targets; owned process-tree cancellation is qualified on Unix.
If Python `9lives` is also installed, it has its own `9l` command. Use an
explicit path such as `$HOME/.local/bin/9l` to select this Go runner.

## First run

Use the [local demo](../demo/README.md) without an application or model account,
or follow the [existing-project guide](run-existing-project.md) against your
approved application. Run selected SDK specs with `9l run <spec> --sdk`.
