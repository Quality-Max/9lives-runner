// Closed, content-free attribution for the experimental owned Simulator path.
export const iosVersions = {appium: '3.8.0', xcuitest: '10.43.1'} as const;
export const iosRuntimeType = 'application/vnd.9l.ios-runtime+json';
export type IOSRuntime = {
  bundleId: string; appSHA256: string; version: string; build: string;
  runtime: string; deviceType: string; os: string; architecture: string;
  xcode: string; appium: string; xcuitest: string; worker: string;
  viewportWidth: string; viewportHeight: string;
};
export const iosRuntimeKeys = ['bundleId', 'appSHA256', 'version', 'build', 'runtime', 'deviceType', 'os',
  'architecture', 'xcode', 'appium', 'xcuitest', 'worker', 'viewportWidth', 'viewportHeight'] as const;

export function validIOSRuntime(value: unknown): value is IOSRuntime {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false;
  const r = value as Record<string, unknown>;
  if (Object.keys(r).length !== iosRuntimeKeys.length || iosRuntimeKeys.some(key => typeof r[key] !== 'string')) return false;
  const v = r as Record<string, string>;
  const version = /^[0-9]{1,3}(\.[0-9]{1,5}){0,3}$/;
  return /^[A-Za-z][A-Za-z0-9-]*(\.[A-Za-z0-9-]+)+$/.test(v.bundleId) && v.bundleId.length <= 200 &&
    /^[a-f0-9]{64}$/.test(v.appSHA256) && /^[a-f0-9]{64}$/.test(v.worker) &&
    version.test(v.version) && version.test(v.build) && version.test(v.os) && version.test(v.xcode) &&
    /^com\.apple\.CoreSimulator\.SimRuntime\.iOS-[0-9]{1,3}(-[0-9]{1,5}){1,2}$/.test(v.runtime) &&
    v.runtime === `com.apple.CoreSimulator.SimRuntime.iOS-${v.os.replaceAll('.', '-')}` &&
    /^com\.apple\.CoreSimulator\.SimDeviceType\.[A-Za-z0-9-]{1,80}$/.test(v.deviceType) &&
    ['arm64', 'x86_64'].includes(v.architecture) && v.appium === iosVersions.appium && v.xcuitest === iosVersions.xcuitest &&
    /^[1-9][0-9]{0,4}$/.test(v.viewportWidth) && /^[1-9][0-9]{0,4}$/.test(v.viewportHeight);
}
