import { expect, it } from 'vitest';
import config from './vercel.json';
const rules = config.headers.filter(rule => rule.headers.some(h => h.key === 'Content-Security-Policy'));
function policy(path: string) {
  const matched = rules.filter(rule => new RegExp(`^${rule.source}$`).test(path));
  expect(matched).toHaveLength(1); // Multiple CSP headers intersect and can block the approved embed.
  return new Map(matched[0].headers.find(h => h.key === 'Content-Security-Policy')!.value.split(';').map(d => {
    const [name, ...values] = d.trim().split(/\s+/);
    return [name, values];
  }));
}
it('allows only the standalone demo to be embedded by QualityMax', () => {
  expect(policy('/demo.html').get('frame-ancestors')).toEqual(["'self'", 'https://qualitymax.io', 'https://www.qualitymax.io']);
  for (const path of ['/', '/install.sh', '/demo.html-other', '/some/demo.html']) {
    expect(policy(path).get('frame-ancestors')).toEqual(["'none'"]);
  }
  expect(config.headers.flatMap(r => r.headers).some(h => h.key === 'X-Frame-Options')).toBe(false);
  expect(policy('/').get('frame-src')).toEqual(["'self'"]);
  expect(policy('/demo.html').get('font-src')).toEqual(["'self'", 'data:']);
});
