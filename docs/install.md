# Install the CLI and Playwright SDK

The published npm package is [@9l/playwright 0.3.0](https://www.npmjs.com/package/@9l/playwright).
It supplies TypeScript fixtures and the reporter. Install the Go `9l` CLI
separately; the SDK does not bundle the runner or a browser.

## SDK

From your Playwright project, with Node 24 (Node 22 minimum):

```sh
npm install --save-dev @9l/playwright@0.3.0 @playwright/test@1.64.0
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
is the download location. CLI v0.3.0 is published for macOS, Linux and
Windows; releases before v0.2.0 have no Windows assets. Download a
specific version and the matching `SHA256SUMS` file. Go is not needed to run
these executables.

| System | Archive |
| --- | --- |
| macOS, Apple Silicon | `9l-darwin-arm64.tar.gz` |
| macOS, Intel | `9l-darwin-amd64.tar.gz` |
| Linux, x86-64 | `9l-linux-amd64.tar.gz` |
| Linux, ARM64 | `9l-linux-arm64.tar.gz` |
| Windows, x86-64 | `9l-windows-amd64.zip` |
| Windows, ARM64 | `9l-windows-arm64.zip` |

For the published `v0.3.0`, using GitHub CLI, this example installs on Apple
Silicon. Choose your archive from the table:

```sh
mkdir -p /tmp/9l-download
cd /tmp/9l-download
BUNDLE=9l-darwin-arm64
gh release download v0.3.0 --repo Quality-Max/9lives-runner --pattern "$BUNDLE.tar.gz" --pattern SHA256SUMS
grep -Ex "[0-9a-f]{64}  $BUNDLE\.tar\.gz" SHA256SUMS > selected.sha256
test "$(grep -c '' selected.sha256)" = 1
shasum -a 256 -c selected.sha256
tar -xzf "$BUNDLE.tar.gz"
mkdir -p "$HOME/.local/bin"
cp "$BUNDLE/9l" "$HOME/.local/bin/9l"
export PATH="$HOME/.local/bin:$PATH"
9l --version
```

On Linux use `sha256sum -c selected.sha256` for checksum verification. The
selection accepts exactly one line that is a lowercase SHA-256, two spaces and
the archive name, so a line with trailing content, a duplicate entry or a CRLF
line ending stops the install instead of being trimmed. Keep the
included `LICENSE` and `NOTICE`. macOS archives are not Apple notarized; the
checksums verify downloaded bytes against the release's checksum file.

### Windows

Choose the Windows ZIP for your CPU, download it with that release's
`SHA256SUMS`, then verify and extract it in PowerShell. Windows assets start
with v0.3.0:

```powershell
$Version = 'v0.3.0'
$Bundle = '9l-windows-amd64' # use arm64 for Windows on ARM
New-Item -ItemType Directory -Force 9l-download | Out-Null
Set-Location 9l-download
gh release download $Version --repo Quality-Max/9lives-runner --pattern "$Bundle.zip" --pattern SHA256SUMS
$Lines = @((Get-Content -Raw SHA256SUMS) -split "`n" | Where-Object { $_ -cmatch "^[0-9a-f]{64}  $([regex]::Escape("$Bundle.zip"))$" })
if ($Lines.Count -ne 1) { throw 'Missing or duplicate archive checksum' }
$Expected = ($Lines[0] -split '  ')[0]
if ((Get-FileHash "$Bundle.zip" -Algorithm SHA256).Hash -ne $Expected) { throw 'Archive checksum mismatch' }
Expand-Archive "$Bundle.zip" -DestinationPath . -Force
& ".\$Bundle\9l.exe" --version
```

Keep `LICENSE`, `NOTICE` and `THIRD-PARTY-NOTICES.txt` with the binary. Add its directory to your user
`PATH`, or invoke `9l.exe` by its full path. Install Node 24, project dependencies
and Chromium as above. Native Windows CI covers both architectures with the
workspace Playwright pin (1.61.1); later Playwright releases are separately
qualified on Linux. Go's race detector runs on Windows amd64; Windows arm64
runs the same Go tests without the race detector. Provider CLI shell
integrations remain unqualified on Windows. Receipt and evidence files inherit
their directory's Windows ACLs; keep the project and receipt directory under
your user account. POSIX mode bits do not set Windows ACLs.

If Python `9lives` is also installed, it has its own `9l` command. Use an
explicit path such as `$HOME/.local/bin/9l` to select this Go runner.

## First run

Use the [local demo](../demo/README.md) without an application or model account,
or follow the [existing-project guide](run-existing-project.md) against your
approved application. Run selected SDK specs with `9l run <spec> --sdk`.
