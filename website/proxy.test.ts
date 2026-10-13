import { describe, expect, it } from 'vitest';
import { NextRequest } from 'next/server';

import { proxy } from './proxy';

describe('installer content negotiation', () => {
  it('serves the landing page to browsers that accept HTML', () => {
    const response = proxy(
      new NextRequest('https://9lives.run/', {
        headers: { accept: 'text/html,application/xhtml+xml' },
      }),
    );

    expect(response.status).toBe(200);
    expect(response.headers.get('x-middleware-next')).toBe('1');
  });

  it('matches HTML media types case-insensitively', () => {
    const response = proxy(
      new NextRequest('https://9lives.run/', {
        headers: { accept: 'Text/Html; q=0.9' },
      }),
    );

    expect(response.status).toBe(200);
    expect(response.headers.get('x-middleware-next')).toBe('1');
  });

  it('treats text/html with q=0 as refusing HTML', () => {
    for (const accept of ['text/html;q=0', 'text/html; q=0.0, */*', 'application/json, TEXT/HTML;Q=0']) {
      const response = proxy(new NextRequest('https://9lives.run/', { headers: { accept } }));

      expect(response.status, accept).toBe(307);
      expect(response.headers.get('location')).toBe('https://9lives.run/install.sh');
    }
  });

  it('serves HTML when any text/html range has a positive quality', () => {
    const response = proxy(
      new NextRequest('https://9lives.run/', {
        headers: { accept: 'text/html;q=0, text/html;level=1;q=0.5' },
      }),
    );

    expect(response.status).toBe(200);
    expect(response.headers.get('x-middleware-next')).toBe('1');
  });

  it('redirects command-line GET requests to the installer', () => {
    const response = proxy(
      new NextRequest('https://9lives.run/', {
        headers: { accept: '*/*' },
      }),
    );

    expect(response.status).toBe(307);
    expect(response.headers.get('location')).toBe('https://9lives.run/install.sh');
  });

  it('redirects GET requests without an Accept header to the installer', () => {
    const response = proxy(new NextRequest('https://9lives.run/'));

    expect(response.status).toBe(307);
    expect(response.headers.get('location')).toBe('https://9lives.run/install.sh');
  });

  it('redirects HEAD requests to the installer', () => {
    const response = proxy(
      new NextRequest('https://9lives.run/', {
        method: 'HEAD',
        headers: { accept: '*/*' },
      }),
    );

    expect(response.status).toBe(307);
    expect(response.headers.get('location')).toBe('https://9lives.run/install.sh');
  });

  it('does not redirect non-GET methods to the static installer', () => {
    const response = proxy(
      new NextRequest('https://9lives.run/', {
        method: 'POST',
        headers: { accept: 'application/json' },
      }),
    );

    expect(response.status).toBe(200);
    expect(response.headers.get('x-middleware-next')).toBe('1');
  });

  it('serves HTML for non-GET methods', () => {
    const response = proxy(
      new NextRequest('https://9lives.run/', {
        method: 'POST',
        headers: { accept: 'text/html' },
      }),
    );

    expect(response.status).toBe(200);
    expect(response.headers.get('x-middleware-next')).toBe('1');
  });
});
