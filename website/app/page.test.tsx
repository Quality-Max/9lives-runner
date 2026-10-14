// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, expect, it } from 'vitest';
import Home from './page';
afterEach(cleanup);

it('uses locally served official branding in both header and footer', () => {
  render(<Home />);
  const links = screen.getAllByRole('link', { name: 'QualityMax home' });
  expect(links).toHaveLength(2);
  expect(links.every(link => link.getAttribute('href') === 'https://qualitymax.io')).toBe(true);
  expect(screen.getAllByRole('img', { name: 'QualityMax' }).every(img => img.getAttribute('src') === '/brand/logo-wordmark-white.svg')).toBe(true);
});
it('leads with the recorded experiment and links every chapter', () => {
  render(<Home />);
  expect(screen.getByRole('heading', { level: 1 }).textContent).toBe('Codex fixes.9l checks the change.');
  expect(screen.getByRole('link', { name: /explore the real experiment/i }).getAttribute('href')).toBe('/demo.html#proof');
  for (const chapter of ['locator', 'coverage', 'flow', 'live', 'proof']) {
    expect(screen.getAllByRole('link').some(link => link.getAttribute('href') === `/demo.html#${chapter}`)).toBe(true);
  }
  expect(screen.getByText(/Seven Chromium executions on a synthetic profile app/).textContent).toContain('does not run Codex');
  expect(screen.getByText(/None establishes full requirement coverage/)).toBeTruthy();
  expect(screen.getByText('Experimental')).toBeTruthy();
  expect(screen.getByText('Planned')).toBeTruthy();
});
it('starts with ordinary Playwright execution and keeps optional paths clear', () => {
  render(<Home />);
  for (const command of ['curl -fsSL https://9lives.run/install.sh | sh', 'export PATH="$HOME/.local/share/9lives-runner/bin:$PATH"', '9l run tests/ --timeout 5m --format json', 'claude mcp add 9lives -- 9l mcp', 'uv tool install 9lives']) {
    expect(screen.getByText(command)).toBeTruthy();
  }
  expect(screen.getByText(/The SDK is optional/)).toBeTruthy();
  expect(screen.getByText(/goal completion is unverified/)).toBeTruthy();
  expect(screen.getByRole('link', { name: 'Documentation' }).getAttribute('href')).toBe('https://quality-max.github.io/9lives-runner/');
  expect(screen.getByRole('link', { name: 'Read the migration guide' }).getAttribute('href')).toContain('MIGRATING_TO_GO.md');
});
it('retains nine page checks, legal information, and ecosystem references', () => {
  render(<Home />);
  for (const name of ['console', 'links', 'accessibility', 'performance', 'seo', 'security_headers', 'cookies', 'mixed_content', 'weight']) {
    expect(screen.getByRole('heading', { name })).toBeTruthy();
  }
  for (const name of ['AI in QA — Issue #18', 'awesome-python-testing', 'Imprint / Impressum', 'Privacy / Datenschutz', 'Terms / Nutzungsbedingungen']) {
    expect(screen.getByRole('link', { name })).toBeTruthy();
  }
  expect(screen.getByText(/INP is not measured/)).toBeTruthy();
});
