// Repository wrapper for the command that ships in @9l/playwright as
// `npx 9l-prepare-biometric-avd`; see packages/playwright/src/prepare-biometric-avd.ts.
// node scripts/prepare-biometric-avd.cjs --avd 9l_biometric_API_35 --wipe [--snapshot 9l-biometric]
require('../packages/playwright/dist/prepare-biometric-avd').prepareBiometricAvd(process.argv.slice(2)).then(code => { process.exitCode = code; });
