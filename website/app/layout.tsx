import type { Metadata } from 'next';
import Script from 'next/script';
import { themeBootstrap } from './theme-bootstrap';
import './globals.css';
export const metadata: Metadata = {
  metadataBase: new URL('https://9lives.run'),
  title: '9lives by QualityMax | Open-source testing framework',
  description: 'Open-source testing for coding agents: Playwright, experimental Android/Appium, MCP tools and execution receipts. Run locally or with QualityMax Cloud.',
  alternates: { canonical: '/' },
  openGraph: {
    title: '9lives by QualityMax | Open-source testing framework',
    description: 'Run your Playwright tests, connect your coding agent, and inspect the evidence. Experimental Android/Appium support and an optional QualityMax Cloud workflow.',
    url: 'https://9lives.run',
    siteName: '9Lives',
    type: 'website',
    images: [{ url: '/og.png', width: 1200, height: 630, alt: '9lives by QualityMax: Playwright, experimental Android/Appium, and optional cloud workflows' }],
  },
  twitter: {
    card: 'summary_large_image',
    title: '9lives by QualityMax | Open-source testing framework',
    description: 'Open-source testing framework by QualityMax. Playwright, experimental Android/Appium, MCP tools and attributable execution receipts.',
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
