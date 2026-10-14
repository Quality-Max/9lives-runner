/* QualityMax's public-site consent banner, PostHog project and GA4 property. */
(function () {
  'use strict';
  if (window.QualityMaxCookies) return;
  const production = /^(www\.)?9lives\.run$/.test(location.hostname);
  const preview = location.hostname.endsWith('.vercel.app');
  // Downloaded/GitHub demos remain entirely offline.
  if (!production && !preview) return;
  const consentKey = 'qm_cookie_consent';
  const measurementId = 'G-B0P0BRN845';
  let consent = null;
  let loading = false;
  let providersStarted = false;
  let gaReady = false;
  let phReady = false;
  try {
    const stored = JSON.parse(localStorage.getItem(consentKey));
    if (stored && stored.version === '1' && typeof stored.analytics === 'boolean') consent = stored;
  } catch { /* Unavailable storage leaves the banner visible. */ }

  function allowed() {
    return production && !navigator.webdriver && navigator.doNotTrack !== '1' &&
      consent?.analytics === true && !document.cookie.split(';').some(item => item.trim() === 'qm_analytics_opt_out=1');
  }

  function loadScript(id, src) {
    return new Promise((resolve, reject) => {
      const script = document.createElement('script');
      script.id = id;
      script.async = true;
      script.src = src;
      const timer = setTimeout(() => { script.remove(); reject(new Error('Analytics load timed out')); }, 10000);
      script.onload = () => { clearTimeout(timer); resolve(); };
      script.onerror = () => { clearTimeout(timer); script.remove(); reject(new Error('Analytics unavailable')); };
      document.head.appendChild(script);
    });
  }

  function pageView() {
    if (!allowed()) return;
    const properties = { surface: '9lives', page_path: location.pathname + location.hash };
    if (phReady) window.posthog.capture('page_view', properties);
    if (gaReady) window.gtag('event', 'page_view', {
      ...properties, page_location: location.href, page_title: document.title,
    });
  }

  async function startGoogle() {
    window['ga-disable-' + measurementId] = false;
    window.dataLayer = window.dataLayer || [];
    window.gtag = function () { window.dataLayer.push(arguments); };
    window.gtag('consent', 'default', {
      analytics_storage: 'granted', ad_storage: 'denied',
      ad_user_data: 'denied', ad_personalization: 'denied',
    });
    window.gtag('js', new Date());
    window.gtag('config', measurementId, {
      send_page_view: false, allow_google_signals: false, allow_ad_personalization_signals: false,
    });
    await loadScript('qm-google-analytics', 'https://www.googletagmanager.com/gtag/js?id=' + measurementId);
    if (allowed()) gaReady = true;
  }

  async function startPostHog() {
    const response = await fetch('/api/posthog/config', { signal: AbortSignal.timeout(8000) });
    if (!response.ok) return;
    const config = await response.json();
    if (!allowed() || config.enabled !== true || config.host !== 'https://t.qualitymax.io') return;
    await loadScript('qm-posthog', config.host + '/static/array.js');
    if (!allowed() || !window.posthog) return;
    window.posthog.init(config.api_key, {
      api_host: config.host, ui_host: 'https://eu.posthog.com', defaults: '2026-01-30',
      person_profiles: 'identified_only', capture_pageview: 'history_change', capture_pageleave: true,
      capture_heatmaps: true, persistence: 'localStorage', respect_dnt: true, autocapture: true,
      disable_session_recording: false,
      session_recording: { maskAllInputs: true, maskInputOptions: { password: true, email: true } },
      loaded: function (posthog) {
        if (!allowed()) { posthog.opt_out_capturing(); return; }
        posthog.opt_in_capturing({ captureEventName: false });
        phReady = true;
        posthog.capture('page_view', { surface: '9lives', page_path: location.pathname + location.hash });
      },
    });
  }

  async function start() {
    if (!allowed() || loading) return;
    loading = true;
    providersStarted = true;
    // Provider failures remain independent and never break the page.
    await Promise.allSettled([startGoogle(), startPostHog()]);
    if (gaReady && allowed()) window.gtag('event', 'page_view', {
      surface: '9lives', page_path: location.pathname + location.hash,
      page_location: location.href, page_title: document.title,
    });
  }

  function clearAnalyticsStorage() {
    // Delete analytics identifiers only; retain the visitor's consent choice.
    try {
      Object.keys(localStorage).filter(key => key.startsWith('ph_')).forEach(key => localStorage.removeItem(key));
    } catch { /* Storage may be blocked. */ }
    document.cookie.split(';').forEach(item => {
      const name = item.trim().split('=')[0];
      if (!name.startsWith('_ga') && !name.startsWith('ph_')) return;
      ['', '; Domain=' + location.hostname, '; Domain=.9lives.run'].forEach(domain => {
        document.cookie = name + '=; Max-Age=0; Path=/; SameSite=Lax; Secure' + domain;
      });
    });
  }

  function choose(analytics) {
    consent = { analytics, timestamp: new Date().toISOString(), version: '1' };
    try { localStorage.setItem(consentKey, JSON.stringify(consent)); } catch { /* Keep this session's choice in memory. */ }
    document.getElementById('qm-cookie-banner')?.remove();
    if (analytics) {
      document.cookie = 'qm_analytics_opt_out=; Max-Age=0; Path=/; SameSite=Lax; Secure';
      void start();
    } else {
      window['ga-disable-' + measurementId] = true;
      if (phReady) window.posthog.opt_out_capturing();
      clearAnalyticsStorage();
      // Unload already loaded SDKs, recording and pending analytics requests.
      if (providersStarted) location.reload();
    }
  }

  function showBanner() {
    if (document.getElementById('qm-cookie-banner')) return;
    const banner = document.createElement('section');
    banner.id = 'qm-cookie-banner';
    banner.setAttribute('aria-label', 'Cookie preferences');
    banner.innerHTML = `
      <div style="position:fixed;bottom:0;left:0;right:0;z-index:99999;background:#1a1a2e;color:#e8e6f0;padding:16px 24px;display:flex;align-items:center;justify-content:space-between;gap:16px;flex-wrap:wrap;border-top:1px solid #2a2a4a;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;font-size:14px;line-height:1.5;box-shadow:0 -4px 20px rgba(0,0,0,0.3)">
        <div style="flex:1;min-width:min(280px,100%)">
          <strong>We value your privacy</strong>
          <p style="margin:4px 0 0;color:#a0a0b8;font-size:13px">We use cookies for essential functionality and optional analytics to improve our product.
            <a href="https://qualitymax.io/privacy" style="color:#6366f1;text-decoration:underline">Privacy Policy</a>
          </p>
        </div>
        <div style="display:flex;gap:8px;flex-wrap:wrap">
          <button type="button" id="qm-cookie-reject" style="padding:8px 20px;min-height:44px;border-radius:6px;font-size:13px;font-weight:600;cursor:pointer;border:1px solid #3a3a5a;background:transparent;color:#a0a0b8">Essential Only</button>
          <button type="button" id="qm-cookie-accept" style="padding:8px 20px;min-height:44px;border-radius:6px;font-size:13px;font-weight:600;cursor:pointer;border:0;background:#6366f1;color:#fff">Accept All</button>
        </div>
      </div>`;
    document.body.appendChild(banner);
    banner.querySelector('#qm-cookie-reject').onclick = () => choose(false);
    banner.querySelector('#qm-cookie-accept').onclick = () => choose(true);
  }

  window.QualityMaxCookies = { show: showBanner };
  window.cookieConsentAllowsAnalytics = allowed;
  window.cookieConsentOptOut = () => choose(false);
  window.cookieConsentOptIn = () => choose(true);
  function mount() {
    document.querySelectorAll('[data-qm-cookie-settings]').forEach(button => {
      button.hidden = false;
      button.addEventListener('click', showBanner);
    });
    if (!consent) showBanner();
    void start();
  }
  window.addEventListener('hashchange', pageView);
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', mount, { once: true });
  else mount();
})();
