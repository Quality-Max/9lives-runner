// Reuse QualityMax's public browser configuration without storing a key here.
export async function GET() {
  try {
    const response = await fetch('https://qualitymax.io/api/posthog/config', {
      signal: AbortSignal.timeout(5000),
      next: { revalidate: 300 },
    });
    if (!response.ok) throw new Error('Configuration unavailable');
    const payload: unknown = await response.json();
    if (!payload || typeof payload !== 'object') return Response.json({ enabled: false });
    const config = payload as Record<string, unknown>;
    if (config.enabled !== true || config.host !== 'https://t.qualitymax.io' ||
        typeof config.api_key !== 'string' || !/^phc_[A-Za-z0-9]+$/.test(config.api_key)) {
      return Response.json({ enabled: false });
    }
    // Only the public ingestion key is exposed, never a personal API credential.
    return Response.json({ enabled: true, host: config.host, api_key: config.api_key });
  } catch {
    return Response.json({ enabled: false }, { status: 503 });
  }
}
