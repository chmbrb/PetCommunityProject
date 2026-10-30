// SPDX-License-Identifier: AGPL-3.0-or-later

const browsers: [RegExp, string][] = [
  [/PetCommunity/i, 'PetCommunity app'],
  [/Edg\//, 'Edge'],
  [/OPR\/|Opera/, 'Opera'],
  [/Firefox\/|FxiOS/, 'Firefox'],
  [/Chrome\/|CriOS/, 'Chrome'],
  [/Safari\//, 'Safari'],
]

const systems: [RegExp, string][] = [
  [/iPhone|iPad|iPod/, 'iOS'],
  [/Android/, 'Android'],
  [/Windows/, 'Windows'],
  [/Mac OS X|Macintosh/, 'macOS'],
  [/CrOS/, 'ChromeOS'],
  [/Linux/, 'Linux'],
]

/**
 * Describes a device from its User-Agent, e.g. "Firefox on Linux", so people can
 * recognize their signed-in devices. It's a rough guess, which is all it needs to be.
 */
export function deviceLabel(userAgent: string): string {
  const browser = browsers.find(([re]) => re.test(userAgent))?.[1]
  const system = systems.find(([re]) => re.test(userAgent))?.[1]
  if (browser && system) {
    return `${browser} on ${system}`
  }
  return browser ?? system ?? 'Unknown device'
}
