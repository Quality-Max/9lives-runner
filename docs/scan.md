# Page checks: scan and inspect

Before a coding agent calls a web change done, give it evidence from the page
itself. `9l scan` loads the page once in Chromium with the project's own
Playwright and reports graded findings with reproduction steps and suggested
fixes:

```sh
9l scan https://your-app.dev --format markdown
9l scan http://localhost:3000/ --allow-private-network --format json --out scan.json
9l scan http://localhost:3000/ --allow-private-network --baseline scan.json --fail-on-new
9l inspect http://localhost:3000/login --allow-private-network
```

| Check | What it reports |
| --- | --- |
| `console` | JavaScript errors, warnings and failed requests |
| `links` | Broken and redirecting links, up to `--max-links` |
| `accessibility` | Missing alt text, unlabelled controls, nameless interactive elements, heading structure |
| `performance` | Core Web Vitals LCP and CLS, plus TTFB and FCP load timings (INP, the third Core Web Vital, needs real interaction and is not measured) |
| `seo` | Title and meta description |
| `security_headers` | CSP, HSTS, `X-Content-Type-Options`, `Referrer-Policy` |
| `cookies` | Missing `Secure`/`HttpOnly`/`SameSite`, third-party cookies, known trackers, tracking before consent |
| `mixed_content` | HTTP subresources and form actions on an HTTPS page |
| `weight` | Transfer bytes, request count, render-blocking resources, oversized and uncompressed assets |

The Markdown report opens with the grade and the categories to fix first:

> **Grade: 🔴 F  (0 / 100)**   ·   15 issues found
>
> | Category | Issues | Worst |
> |----------|--------|:-----:|
> | Console errors | `██████████` 4 | 🔴 high |
> | Accessibility | `████████░░` 3 | 🔴 high |
> | Security headers | `██████████` 4 | 🟠 medium |

`--baseline` compares against an earlier JSON result and reports new, fixed
and unchanged findings, so a pipeline can fail only on regressions.
`9l inspect` returns the page's ranked locators (test id, role and label
before text and position), each control's stability and a testability
verdict. A page behind a login is checked with a Playwright storage-state
file and `--acknowledge-private-content`. Loopback targets need
`--allow-private-network`; other private addresses stay blocked. Scans are
observations from one cold page load, not test results.

Coding agents get the same checks from `9l mcp` as `scan_url` and
`inspect_page`, plus `generate_playwright_repro`, which writes a spec for a
finding that `run_test` executes and `confirm_finding` checks across a fix.
These checks were previously the separate qmax-mcp npm server.

See the [CLI reference](cli.md) for every flag and the
[MCP server](mcp.md) for the agent tools.
