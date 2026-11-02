// Closed, content-free metadata allowed across the native worker bridge.
import {androidVersions} from './android-env';
export {androidVersions};
export const androidRuntimeType = 'application/vnd.9l.android-runtime+json';
export type AndroidRuntime = {
  appPackage: string; apkSHA256: string; api: string; release: string; abi: string;
  appium: string; uiautomator2: string; worker: string;
  /** The physical display the session targets (its id, or unknown), its
   * size in pixels and density, and the fold posture when the device
   * reports one. Layout-dependent specs can be read against these. */
  display: string; displayWidth: string; displayHeight: string; density: string;
  posture: 'opened' | 'half-opened' | 'closed' | 'flat' | 'unknown';
  /** The installed build's version and signing certificate digest, read
   * from the device; `unknown` when not read. apkSHA256 is the verified
   * digest of the installed base APK. */
  versionCode: string; versionName: string; signer: string;
} & ({emulator: string; device?: never; deviceHash?: never} |
     {device: 'physical'; deviceHash: string; emulator?: never});

export function validAndroidRuntime(value: unknown): value is AndroidRuntime {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false;
  const r = value as Record<string, unknown>;
  const keys = ['appPackage', 'apkSHA256', 'api', 'release', 'abi', 'appium', 'uiautomator2', 'worker', 'display', 'displayWidth', 'displayHeight', 'density', 'posture', 'versionCode', 'versionName', 'signer',
    ...(r.device === 'physical' ? ['device', 'deviceHash'] : ['emulator'])];
  if (Object.keys(r).length !== keys.length || keys.some(key => typeof r[key] !== 'string')) return false;
  const text = r as Record<string, string>;
  const version = /^[0-9]{1,3}(\.[0-9]{1,5}){0,3}$/;
  return text.appPackage.length <= 200 && /^[a-zA-Z][a-zA-Z0-9_]*(\.[a-zA-Z0-9_]+)+$/.test(text.appPackage) &&
    /^[a-f0-9]{64}$/.test(text.apkSHA256) && /^[a-f0-9]{64}$/.test(text.worker) &&
    /^[0-9]{1,3}$/.test(text.api) && version.test(text.release) && (text.device === 'physical' ? /^[a-f0-9]{64}$/.test(text.deviceHash) : version.test(text.emulator)) &&
    text.appium === androidVersions.appium && text.uiautomator2 === androidVersions.uiautomator2 && ['arm64-v8a', 'x86_64'].includes(text.abi) &&
    (text.display === 'unknown' || /^[0-9]{1,24}$/.test(text.display)) && /^[0-9]{1,5}$/.test(text.displayWidth) && /^[0-9]{1,5}$/.test(text.displayHeight) && /^[0-9]{1,4}$/.test(text.density) &&
    ['opened', 'half-opened', 'closed', 'flat', 'unknown'].includes(text.posture) &&
    (text.versionCode === 'unknown' || /^[0-9]{1,10}$/.test(text.versionCode)) && (text.versionName === 'unknown' || /^[A-Za-z0-9._+-]{1,64}$/.test(text.versionName)) &&
    (text.signer === 'unknown' || /^[a-f0-9]{64}$/.test(text.signer));
}
