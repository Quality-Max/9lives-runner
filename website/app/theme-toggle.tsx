'use client';

import { useSyncExternalStore } from 'react';
function subscribe(notify: () => void) {
  const media = window.matchMedia?.('(prefers-color-scheme: dark)');
  const update = () => {
    if (document.documentElement.dataset.themePreference !== 'manual') {
      document.documentElement.dataset.theme = media?.matches ? 'dark' : 'light';
    }
    notify();
  };
  media?.addEventListener('change', update);
  window.addEventListener('9lives-theme-change', notify);
  return () => {
    media?.removeEventListener('change', update);
    window.removeEventListener('9lives-theme-change', notify);
  };
}

export function ThemeToggle() {
  const theme = useSyncExternalStore(subscribe, () => document.documentElement.dataset.theme || 'dark', () => 'dark');
  function toggle() {
    const next = theme === 'dark' ? 'light' : 'dark';
    document.documentElement.dataset.theme = next;
    document.documentElement.dataset.themePreference = 'manual';
    try { localStorage.setItem('9lives-demo-theme', next); } catch { /* Retain the choice in this page when storage is blocked. */ }
    window.dispatchEvent(new Event('9lives-theme-change'));
  }
  return <button className="theme-toggle" type="button" aria-label="Dark theme" aria-pressed={theme === 'dark'} onClick={toggle}><span aria-hidden="true">◐</span><span>Theme</span></button>;
}
