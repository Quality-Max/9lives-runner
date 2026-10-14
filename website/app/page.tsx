import Image from 'next/image';
import { CopyButton } from './copy-button';
import { ThemeToggle } from './theme-toggle';

const installCommand = 'curl -fsSL https://9lives.run/install.sh | sh';
const pathCommand = 'export PATH="$HOME/.local/share/9lives-runner/bin:$PATH"';
const runCommand = '9l run tests/ --timeout 5m --format json';
const scanCommand = '9l scan https://your-app.dev --format markdown';
const mcpCommand = 'claude mcp add 9lives -- 9l mcp';
const repository = 'https://github.com/Quality-Max/9lives-runner';
const documentation = 'https://quality-max.github.io/9lives-runner/';
const migrationGuide = 'https://github.com/Quality-Max/9lives/blob/main/docs/MIGRATING_TO_GO.md';

const stories = [
  { id: 'locator', number: '01', title: 'A renamed button', copy: 'Repair a broken locator while keeping the original assertions.', label: 'Illustrated walkthrough' },
  { id: 'coverage', number: '02', title: 'A missing check', copy: 'See how a green test can miss the outcome that matters.', label: 'Illustrated walkthrough' },
  { id: 'flow', number: '03', title: 'Behind the agent', copy: 'Follow observed controls, bounded actions, and execution receipts.', label: 'Algorithm walkthrough' },
  { id: 'live', number: '04', title: 'A real Codex run', copy: 'Inspect saved provider decisions and browser execution logs.', label: 'Recorded execution' },
  { id: 'proof', number: '05', title: 'Prove the fix', copy: 'Compare the same test before and after a fix. Then challenge it with faults.', label: 'Recorded execution' },
];
const workflow = [
  { number: '01', title: 'Reproduce the failure', tool: 'run_test', copy: 'Start with a reviewed Playwright spec and an assertion that fails on the broken application.' },
  { number: '02', title: 'Review the change', tool: 'Your coding agent', copy: 'Inspect the proposed application patch or locator repair. Keep the test’s intended behavior and assertions.' },
  { number: '03', title: 'Confirm the fix', tool: 'confirm_finding · experimental', copy: 'Run the same reproduction on each Git revision in isolation. Inspect the failing result before and the passing result after.' },
  { number: '04', title: 'Challenge the test', tool: '9l prove · experimental', copy: 'Inject supported fetch/XHR faults and rerun. See which faults caused an assertion failure and which survived.' },
];
const pageChecks = [
  ['console', 'JavaScript errors, warnings, and failed requests'],
  ['links', 'Broken and redirecting links'],
  ['accessibility', 'Alt text, control names, and heading structure'],
  ['performance', 'LCP, CLS, TTFB, and FCP from one cold page load'],
  ['seo', 'Page title and meta description'],
  ['security_headers', 'CSP, HSTS, and browser security headers'],
  ['cookies', 'Cookie flags, trackers, and tracking before consent'],
  ['mixed_content', 'HTTP resources and forms on HTTPS pages'],
  ['weight', 'Transfer bytes, requests, and oversized assets'],
];
const ecosystem = [
  { title: 'QualityMax Cloud', copy: 'Hosted test management, execution, and quality intelligence.', href: 'https://qualitymax.io' },
  { title: 'qmax-code', copy: 'A coding and QA agent for broader repository workflows.', href: 'https://github.com/Quality-Max/qmax-code' },
  { title: 'QualityMax Grader', copy: 'An offline A–F quality grade for Playwright specs.', href: 'https://github.com/Quality-Max/qualitymax-grader' },
  { title: 'Free QA Skills', copy: 'Open skills for test quality, accessibility, security, and more.', href: 'https://github.com/Quality-Max/free-qa-skills' },
];

function Brand() {
  return <a className="qualitymax-brand" href="https://qualitymax.io" aria-label="QualityMax home">
    <Image className="brand-dark" src="/brand/logo-wordmark-white.svg" alt="QualityMax" width={144} height={22} loading="eager" unoptimized />
    <Image className="brand-light" src="/brand/logo-wordmark-color.svg" alt="" width={144} height={22} loading="eager" unoptimized />
  </a>;
}
function Command({ label, value }: { label: string; value: string }) {
  return <div className="command-card"><span className="command-label">{label}</span><div><code>{value}</code><CopyButton value={value} /></div></div>;
}

export default function Home() {
  return <>
    <a className="skip-link" href="#main">Skip to content</a>
    <header className="site-header wrap">
      <div className="brand-lockup"><Brand /><a className="product-logo" href="#main" aria-label="9lives home">9<span>lives</span></a></div>
      <nav aria-label="Primary navigation"><a href="#demo">Demo</a><a href="#workflow">Workflow</a><a href="#start">Get started</a><a href={documentation}>Documentation</a><a href={repository}>GitHub <span aria-hidden="true">↗</span></a></nav>
      <ThemeToggle />
    </header>
    <main id="main">
      <section className="hero wrap" aria-labelledby="hero-title">
        <div className="hero-content">
          <p className="eyebrow">9lives / Open-source Playwright tools</p>
          <h1 id="hero-title">Codex fixes.<br /><span>9l checks the change.</span></h1>
          <p className="hero-copy">Your agent proposes a fix. Run the test, compare the results, and check what its assertions catch. Keep your Playwright suite and inspect the evidence locally.</p>
          <div className="actions"><a className="button primary" href="/demo.html#proof">Explore the real experiment <span aria-hidden="true">↗</span></a><a className="button secondary" href="#start">Run it yourself <span aria-hidden="true">↓</span></a></div>
          <p className="small-note">No account needed · Apache-2.0 · macOS, Linux &amp; Windows</p>
        </div>
        <div className="evidence-card" aria-label="Recorded fix confirmation">
          <div className="panel-heading"><span className="live-dot" aria-hidden="true" /> Recorded experiment <span>13 Oct 2026</span></div>
          <div className="evidence-title"><span className="badge">Same test. Two app revisions.</span><h2>The test tells the<br />revisions apart.</h2></div>
          <div className="revision-pair"><div className="verdict failed"><span>Before the fix</span><strong>Assertion failed</strong><code>0636853e13</code></div><div className="verdict passed"><span>After the fix</span><strong>Test passed</strong><code>6e5f24ccd2</code></div></div>
          <div className="terminal-summary"><p><span>9l MCP</span><code>confirm_finding</code><strong>confirmed</strong></p><p><span>9l prove</span><code>500 · 401 · malformed JSON</code><strong>3 faults caught</strong></p></div>
          <p className="evidence-footnote">Seven Chromium executions on a synthetic profile app. Saved results; opening the demo does not run Codex.</p>
          <a className="text-link" href="/demo.html#proof">Inspect the patch, logs, and receipts <span aria-hidden="true">→</span></a>
        </div>
      </section>
      <div className="principles wrap"><span>Ordinary Playwright specs</span><span>Explicit assertions</span><span>Bounded local execution</span><span>Attributable receipts</span></div>

      <section className="section wrap" id="demo" aria-labelledby="demo-heading">
        <div className="section-heading"><p className="eyebrow">Start with the evidence</p><h2 id="demo-heading">Five ways to look past a green test.</h2><p>Follow a locator repair, find a missing assertion, or inspect a real Codex run and checked application fix. Choose where to begin.</p></div>
        <div className="story-grid">{stories.map(story => <a className="story-card" key={story.id} href={`/demo.html#${story.id}`}><span className="story-number">{story.number} <span aria-hidden="true">↗</span></span><h3>{story.title}</h3><p>{story.copy}</p><span className="story-label">{story.label}</span></a>)}</div>
        <p className="section-note">The first three tabs explain the workflow. The last two show recorded execution. Full traces scroll, highlights stay easy to find, and evidence extracts are downloadable.</p>
      </section>

      <section className="section wrap" id="workflow" aria-labelledby="workflow-heading">
        <div className="section-heading"><p className="eyebrow">From a failure to a checked fix</p><h2 id="workflow-heading">Make each step earn its result.</h2><p>9lives gives a coding agent execution tools. Your reviewed tests decide which behavior matters.</p></div>
        <div className="workflow-grid">{workflow.map(step => <article key={step.number}><span className="step-number">{step.number}</span><h3>{step.title}</h3><p>{step.copy}</p><code>{step.tool}</code></article>)}</div>
        <div className="boundary"><strong>A pass has a precise meaning.</strong><p>A passing receipt means the test’s assertions passed and the execution report was validated. Confirmation shows the test distinguishes two revisions. A caught fault means an assertion failed under that fault. None establishes full requirement coverage.</p><a href={`${repository}/blob/main/docs/README.md`}>Read the contracts and limits →</a></div>
      </section>

      <section className="section wrap start-section" id="start" aria-labelledby="start-heading">
        <div className="section-heading"><p className="eyebrow">Use your own project</p><h2 id="start-heading">Keep your tests.<br />Add execution evidence.</h2><p>The Go runner uses your project’s installed Playwright and configuration. The SDK is optional. You can run ordinary specs without an AI model or QualityMax account.</p><p className="small-note">Native CLI v0.4.5 · x64 &amp; ARM64 · Chromium qualified</p></div>
        <div className="command-stack"><Command label="01 / Install the CLI on macOS or Linux" value={installCommand} /><Command label="02 / Add the binary to PATH" value={pathCommand} /><Command label="03 / Run your existing Playwright suite" value={runCommand} /><p className="small-note">Your project needs Playwright dependencies and browsers. <a href={`${repository}/blob/main/docs/install.md#windows`}>Windows ZIP installation</a> · <a href={`${repository}/blob/main/docs/run-existing-project.md`}>Project setup</a></p></div>
        <div className="agent-card"><div><p className="eyebrow">Inside your coding agent</p><h3>Connect the tools through MCP.</h3><p>Native MCP exposes run, assess, heal, confirm, page inspection, and reproduction tools. Start a page check with <code>scan_url</code>. Use the QA skill for requirements and source provenance. Codex and Claude setup live in the agent guide.</p><a className="text-link" href={`${repository}/blob/main/docs/agent-setup.md`}>Set up your agent →</a></div><div><Command label="Example / Claude Code" value={mcpCommand} /><p className="small-note">Optional SDK: named steps and bounded goals. Goals still need explicit assertions; goal completion is unverified.</p><a className="text-link" href={`${repository}/blob/main/docs/goals.md`}>Read goal contracts →</a></div></div>
      </section>

      <section className="section wrap" id="page-checks" aria-labelledby="page-checks-heading">
        <div className="section-heading"><p className="eyebrow">Before the reproduction</p><h2 id="page-checks-heading">Inspect the page. Find the next test.</h2><p>One Chromium load gives nine categories of observations. <code>inspect_page</code> ranks controls and locators; <code>generate_playwright_repro</code> writes a reviewable spec for a finding.</p></div>
        <div className="check-grid">{pageChecks.map(([name, copy]) => <article key={name}><h3>{name}</h3><p>{copy}</p></article>)}</div>
        <Command label="Scan a public page" value={scanCommand} />
        <p className="section-note">Scans are observations, not test results. Load timings are from one cold visit; INP is not measured. Authenticated pages need explicit consent, and loopback targets need <code>--allow-private-network</code>. <a href={`${repository}#check-the-page-you-changed`}>Page-check guide →</a></p>
      </section>

      <section className="section wrap" id="status" aria-labelledby="status-heading">
        <div className="section-heading"><p className="eyebrow">What you can rely on</p><h2 id="status-heading">Working tools. Stated boundaries.</h2></div>
        <div className="status-grid"><article><span className="badge">Available</span><h3>Local execution &amp; assessment</h3><p>Plans, bounded runs, cancellation, terminal receipts, advisory static assessment, page checks, and native MCP. Existing Playwright specs stay usable.</p></article><article><span className="badge">Experimental</span><h3>Healing, confirmation &amp; fault testing</h3><p>Supported locator repairs are verified in isolation. Confirmation compares revisions. Fault testing challenges fetch/XHR paths. The demo qualifies one synthetic app in Chromium.</p></article><article><span className="badge">Planned</span><h3>Broader behavioral verification</h3><p>Independent behavioral verification, verified replay, and native mobile qualification remain subsequent milestones. Other browser engines are unqualified.</p></article></div>
        <details className="legacy-note"><summary>Using the original Python package?</summary><p>Python remains in maintenance mode for existing workflows, with no removal date set. Use Go for new Playwright projects. Check <code>9l --version</code> when both distributions are installed.</p><code>uv tool install 9lives</code><p><a href={migrationGuide}>Read the migration guide</a> · <a href="https://github.com/Quality-Max/9lives">Python source</a></p></details>
      </section>

      <section className="section wrap" id="ecosystem" aria-labelledby="ecosystem-heading"><div className="section-heading"><p className="eyebrow">Built by QualityMax</p><h2 id="ecosystem-heading">Local tools, connected to a wider workflow.</h2><p>9lives is the open-source execution tool. Choose the other QualityMax tools when you need hosted management, a coding agent, or a separate quality review.</p></div><div className="ecosystem-grid">{ecosystem.map(item => <a href={item.href} key={item.title}><h3>{item.title}<span aria-hidden="true">↗</span></h3><p>{item.copy}</p></a>)}</div><p className="section-note">Featured in <a href="https://aiinqa.com/ai-in-qa-issue-18/" target="_blank" rel="noreferrer">AI in QA — Issue #18</a>. The original Python package is listed in <a href="https://github.com/cleder/awesome-python-testing" target="_blank" rel="noreferrer">awesome-python-testing</a>.</p></section>
      <section className="closing wrap"><p className="eyebrow">See what the test actually caught</p><h2>Follow the failure.<br />Inspect the fix.</h2><div className="actions"><a className="button primary" href="/demo.html#proof">Open the demo ↗</a><a className="button secondary" href={repository}>Explore the repository ↗</a></div></section>
    </main>
    <footer className="wrap"><div className="footer-top"><Brand /><p>9lives · Open-source Playwright tools<br /><span>Built by QualityMax. Apache-2.0.</span></p><a href={`${repository}/issues`}>Questions &amp; feedback ↗</a></div><nav aria-label="Legal information"><a href="https://qualitymax.io/legal">Imprint / Impressum</a><a href="https://qualitymax.io/privacy">Privacy / Datenschutz</a><a href="https://qualitymax.io/terms">Terms / Nutzungsbedingungen</a><button type="button" data-qm-cookie-settings hidden>Cookie Settings</button></nav><details><summary>QualityMax service contract information</summary><p><a href="https://qualitymax.io/legal/widerruf">Withdrawal / Widerruf</a> · <a href="https://qualitymax.io/cancel-contract">Cancel contract / Vertrag kündigen</a></p></details><p className="small-note">© 2026 QualityMax · Legal information is maintained on qualitymax.io.</p></footer>
  </>;
}
