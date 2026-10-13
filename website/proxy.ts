import { NextResponse, type NextRequest } from 'next/server';

// Whether the Accept header lists text/html with a positive quality value.
// `text/html;q=0` explicitly refuses HTML, so that client gets the installer.
function acceptsHtml(accept: string) {
  return accept.split(',').some((range) => {
    const [type, ...parameters] = range.split(';').map((part) => part.trim().toLowerCase());
    if (type !== 'text/html') return false;
    const quality = parameters.find((parameter) => parameter.startsWith('q='));
    return quality === undefined || Number(quality.slice(2)) > 0;
  });
}

export function proxy(request: NextRequest) {
  const wantsHtml = acceptsHtml(request.headers.get('accept') ?? '');
  const canRedirectToInstaller = request.method === 'GET' || request.method === 'HEAD';

  if (canRedirectToInstaller && !wantsHtml) {
    return NextResponse.redirect(new URL('/install.sh', request.url), 307);
  }

  return NextResponse.next();
}

export const config = { matcher: ['/'] };
