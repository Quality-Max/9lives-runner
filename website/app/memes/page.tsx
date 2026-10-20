/* eslint-disable @next/next/no-html-link-for-pages -- Full document navigation sends the HTML Accept header required by the root installer negotiation. */
import type { Metadata } from 'next';
import Image from 'next/image';
import { Brand } from '../brand';
import { ThemeToggle } from '../theme-toggle';

const description = 'One button rename. Nine lives of debugging. Developer memes from 9lives by QualityMax.';
export const metadata: Metadata = {
  title: '9lives memes — Every test deserves another life.',
  description,
  alternates: { canonical: '/memes' },
  openGraph: {
    title: '9lives memes — Every test deserves another life.',
    description,
    url: 'https://9lives.run/memes',
    type: 'website',
    images: [{ url: '/memes/button-rename-launch.webp', width: 1024, height: 1024, alt: 'One button rename. My afternoon / after the re-run.' }],
  },
  twitter: {
    card: 'summary_large_image',
    title: '9lives memes — Every test deserves another life.',
    description,
    images: ['/memes/button-rename-launch.webp'],
  },
};

const memes = [
  { id: 'button-rename', title: 'One button rename. My afternoon.', caption: 'Repair the locator. Keep the assertion.', alt: 'Two black kittens: exhausted beside a FAIL laptop, then calm beside a PASS laptop after the re-run.' },
  { id: 'harry-potter', title: 'You’re a tester, Harry.', caption: 'Even magic needs a re-run.', alt: 'A black kitten wizard confronts a failed test, then celebrates a passing re-run. Locatorus Brokenus / Rerunus Verifiedus.' },
  { id: 'lotr', title: 'One does not simply rename a button.', caption: 'Repair. Re-run. Review.', alt: 'A black kitten in a wizard cloak raises a paw beside a FAIL laptop, with a smaller passing re-run below.' },
  { id: 'batman-catwoman', title: 'The button moved. Another long night.', caption: 'Keep the assertion. Verify the fix.', alt: 'Two black kitten superheroes: exhausted beside a failing test, then confident beside a passing re-run.' },
];

export default function Memes() {
  return <>
    <a className="skip-link" href="#main">Skip to content</a>
    <header className="site-header wrap">
      <div className="brand-lockup"><Brand /><a className="product-logo" href="/" aria-label="9lives home">9<span>lives</span></a></div>
      <nav aria-label="Primary navigation"><a href="/">Home</a><a href="/demo.html#proof">Demo</a><a href="/memes" aria-current="page">Memes</a><a href="https://github.com/Quality-Max/9lives-runner">GitHub ↗</a></nav>
      <ThemeToggle />
    </header>
    <main id="main" className="wrap">
      <section className="memes-intro" aria-labelledby="memes-title">
        <p className="eyebrow">9lives / Developer humour</p>
        <h1 id="memes-title">Every test deserves<br /><span>another life.</span></h1>
        <p className="hero-copy">For everyone whose afternoon disappeared into one button rename. Save a favourite. Send it to the person who knows.</p>
        <p className="small-note">Illustrated jokes. For actual execution evidence, <a className="text-link" href="/demo.html#proof">open the demo →</a></p>
      </section>
      <section className="meme-grid" aria-label="Launch meme gallery">
        {memes.map(meme => <article className="meme-card" id={meme.id} key={meme.id}>
          <a className="meme-image" href={`/memes/${meme.id}-launch.webp`} aria-label={`Open full-size image: ${meme.title}`}>
            <Image src={`/memes/${meme.id}-launch.webp`} alt={meme.alt} width={1024} height={1024} loading="lazy" unoptimized />
          </a>
          <div className="meme-caption"><h2>{meme.title}</h2><p>{meme.caption}</p><a className="text-link" href={`/memes/${meme.id}-launch.webp`} download>Download image ↓</a></div>
        </article>)}
      </section>
      <section className="meme-outro" aria-labelledby="meme-outro-title"><h2 id="meme-outro-title">Agent fixes. 9l checks the change.</h2><p>Keep your Playwright tests. Inspect what actually passed.</p><a className="button primary" href="/demo.html#proof">Explore the real experiment →</a></section>
    </main>
    <footer className="wrap"><div className="footer-top"><Brand /><p>9lives · By QualityMax</p><a href="/">Back to 9lives →</a></div><nav aria-label="Legal information"><a href="https://qualitymax.io/legal">Imprint / Impressum</a><a href="https://qualitymax.io/privacy">Privacy / Datenschutz</a><a href="https://qualitymax.io/terms">Terms / Nutzungsbedingungen</a><button type="button" data-qm-cookie-settings hidden>Cookie Settings</button></nav><p className="small-note">Original feline parodies. No affiliation with the referenced franchises.</p></footer>
  </>;
}
