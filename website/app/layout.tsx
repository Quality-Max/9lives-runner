import type { Metadata } from 'next';
import { Geist, Geist_Mono } from 'next/font/google';
import './globals.css';

const geistSans = Geist({ variable: '--font-geist-sans', subsets: ['latin'] });
const geistMono = Geist_Mono({ variable: '--font-geist-mono', subsets: ['latin'] });

export const metadata: Metadata = {
  metadataBase: new URL('https://9lives.run'),
  title: '9Lives — Your tests have nine lives',
  description: 'The Go runner for local Playwright tests: bounded execution, validated receipts, and optional AI help. Python compatibility remains available in maintenance mode.',
  alternates: { canonical: '/' },
  openGraph: {
    title: '9Lives — Your tests have nine lives',
    description: 'Ordinary Playwright tests. Bounded AI help. Evidence for every attempt. Install the Go runner for macOS or Linux.',
    url: 'https://9lives.run',
    siteName: '9Lives',
    type: 'website',
    images: [{ url: '/og.png', width: 1200, height: 630, alt: '9Lives by QualityMax — Your tests have nine lives' }],
  },
  twitter: {
    card: 'summary_large_image',
    title: '9Lives — Your tests have nine lives',
    description: 'Run Playwright locally with Go, keep your assertions, and inspect attributable evidence.',
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
    <html lang="en">
      <body className={`${geistSans.variable} ${geistMono.variable}`}>{children}</body>
    </html>
  );
}
