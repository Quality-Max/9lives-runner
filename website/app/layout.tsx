import type { Metadata } from 'next';
import Script from 'next/script';
import { themeBootstrap } from './theme-bootstrap';
import './globals.css';
export const metadata: Metadata = {
  metadataBase: new URL('https://9lives.run'),
  title: '9lives — Codex fixes. 9l checks the change.',
  description: 'Run Playwright tests locally, confirm a fix against the same test, and challenge assertions with controlled faults. Explore real Codex and 9l execution evidence.',
  alternates: { canonical: '/' },
  openGraph: {
    title: '9lives — Codex fixes. 9l checks the change.',
    description: 'From a reproduced failure to a checked fix. Inspect real Codex runs, before/after receipts, and caught faults with the open-source 9lives runner.',
    url: 'https://9lives.run',
    siteName: '9Lives',
    type: 'website',
    images: [{ url: '/og.png', width: 1200, height: 630, alt: '9lives by QualityMax — Codex fixes. 9l checks the change.' }],
  },
  twitter: {
    card: 'summary_large_image',
    title: '9lives — Codex fixes. 9l checks the change.',
    description: 'Your agent proposes a fix. 9l runs the tests, compares revisions, and records what the assertions caught.',
    images: ['/og.png'],
  },
  icons: {
    icon: [
      { url: '/favicon.ico', sizes: '16x16 32x32 48x48' },
      { url: '/qualitymax-pendant.svg', type: 'image/svg+xml', sizes: 'any' },
    ],
    apple: { url: '/apple-touch-icon.png', sizes: '180x180' },
  },
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en" suppressHydrationWarning>
      <head><script dangerouslySetInnerHTML={{ __html: themeBootstrap }} /></head>
      <body>
        {children}
        <Script src="/qualitymax-analytics.js" strategy="afterInteractive" />
      </body>
    </html>
  );
}
