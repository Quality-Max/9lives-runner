import Image from 'next/image';

export function Brand() {
  return <a className="qualitymax-brand" href="https://qualitymax.io" aria-label="QualityMax home">
    <Image className="brand-dark" src="/brand/logo-wordmark-white.svg" alt="QualityMax" width={144} height={22} loading="eager" unoptimized />
    <Image className="brand-light" src="/brand/logo-wordmark-color.svg" alt="" width={144} height={22} loading="eager" unoptimized />
  </a>;
}
