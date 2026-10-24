// Closed, content-free metadata allowed across the native worker bridge.
export const androidVersions = {appium: '3.8.0', uiautomator2: '8.7.0'} as const;
export const androidRuntimeType = 'application/vnd.9l.android-runtime+json';
export type AndroidRuntime = {
  appPackage: string; apkSHA256: string; api: string; release: string; abi: string;
  emulator: string; appium: string; uiautomator2: string; worker: string;
};
export function validAndroidRuntime(value: unknown): value is AndroidRuntime {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false;
  const r = value as Record<string, unknown>;
  const keys = ['appPackage', 'apkSHA256', 'api', 'release', 'abi', 'emulator', 'appium', 'uiautomator2', 'worker'];
  if (Object.keys(r).length !== keys.length || keys.some(key => typeof r[key] !== 'string')) return false;
  const text = r as Record<string, string>;
  const version = /^[0-9]{1,3}(\.[0-9]{1,5}){0,3}$/;
  return text.appPackage.length <= 200 && /^[a-zA-Z][a-zA-Z0-9_]*(\.[a-zA-Z0-9_]+)+$/.test(text.appPackage) &&
    /^[a-f0-9]{64}$/.test(text.apkSHA256) && /^[a-f0-9]{64}$/.test(text.worker) &&
    /^[0-9]{1,3}$/.test(text.api) && version.test(text.release) && version.test(text.emulator) &&
    text.appium === androidVersions.appium && text.uiautomator2 === androidVersions.uiautomator2 && ['arm64-v8a', 'x86_64'].includes(text.abi);
}
