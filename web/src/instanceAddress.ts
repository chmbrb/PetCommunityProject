// SPDX-License-Identifier: AGPL-3.0-or-later
import type { InstanceInfo } from './api/client'

/** Checking an address or an instance failed; the message is shown as is. */
export class InstanceAddressError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'InstanceAddressError'
  }
}

/** An instance's address: its origin, and its host as it's shown to people. */
export interface InstanceAddress {
  /** e.g. https://pets.example.org, or https://pets.example.org:8443 */
  origin: string
  /** The host in its ASCII (punycode) form, so lookalike Unicode domains stand out. */
  host: string
  hostname: string
}

/**
 * Turns what someone typed into an instance address (ADR 0023). Adds https:// when
 * there's no scheme. Rejects anything besides the host and port, rather than dropping
 * it, so what's shown is what was typed. allowLocal permits plain HTTP to local and
 * private-network hosts, in development builds only.
 */
export function parseInstanceAddress(input: string, allowLocal = false): InstanceAddress {
  const typed = input.trim()
  if (!typed) {
    throw new InstanceAddressError("Enter the instance's address.")
  }
  const withScheme = /^[a-z][a-z0-9+.-]*:\/\//i.test(typed) ? typed : `https://${typed}`
  let url: URL
  try {
    url = new URL(withScheme)
  } catch {
    throw new InstanceAddressError("That isn't a valid address.")
  }
  const local = isLocalHost(url.hostname)
  if (url.protocol !== 'https:' && !(url.protocol === 'http:' && allowLocal && local)) {
    throw new InstanceAddressError('Only https:// addresses can be used.')
  }
  if (url.username || url.password) {
    throw new InstanceAddressError('Remove the user name from the address.')
  }
  // Checked on the typed text too: URL drops an empty "?" or "#".
  if (url.pathname !== '/' || url.search || url.hash || /[?#]/.test(withScheme)) {
    throw new InstanceAddressError('Enter only the domain, without a path, such as pets.example.org.')
  }
  return { origin: url.origin, host: url.host, hostname: url.hostname }
}

/** Hosts that development builds may reach over plain HTTP. */
function isLocalHost(hostname: string): boolean {
  if (hostname === 'localhost' || hostname.endsWith('.localhost') || hostname === '[::1]') {
    return true
  }
  const ip = hostname.match(/^(\d{1,3})\.(\d{1,3})\.\d{1,3}\.\d{1,3}$/)
  if (!ip) {
    return false
  }
  const [a, b] = [Number(ip[1]), Number(ip[2])]
  // Loopback, the Android emulator's host (10.0.2.2), and private networks.
  return a === 127 || a === 10 || (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168)
}

/**
 * Fetches an instance's metadata and accepts it only if it's an instance of this
 * software, serves an API version the app supports, and says it's the domain that was
 * entered (ADR 0023). Redirects aren't followed, and no credentials are sent.
 */
export async function checkInstance(
  address: InstanceAddress,
  { allowLocal = false, fetchImpl = fetch, timeoutMs = 10_000 } = {},
): Promise<InstanceInfo> {
  let res: Response
  try {
    res = await fetchImpl(`${address.origin}/api/v1/instance`, {
      headers: { Accept: 'application/json' },
      redirect: 'error',
      credentials: 'omit',
      signal: AbortSignal.timeout(timeoutMs),
    })
  } catch {
    throw new InstanceAddressError(
      `Couldn't reach ${address.host}. Check the address and your connection. If the instance has moved, enter its new address.`,
    )
  }
  const notAnInstance = new InstanceAddressError(`${address.host} isn't a PetCommunity instance.`)
  if (!res.ok) {
    throw notAnInstance
  }
  let info: unknown
  try {
    info = await res.json()
  } catch {
    throw notAnInstance
  }
  if (!isInstanceInfo(info) || info.software.name !== 'petcommunity') {
    throw notAnInstance
  }
  if (info.api_version !== 'v1') {
    throw new InstanceAddressError(`${address.host} runs a version this app doesn't support. Check for an app update.`)
  }
  if (!info.domain) {
    // Production instances must set PC_INSTANCE_DOMAIN; only development ones leave it out.
    if (!allowLocal) {
      throw new InstanceAddressError(`${address.host} doesn't say which domain it is, so the app can't connect to it.`)
    }
  } else if (hostnameOf(info.domain) !== address.hostname) {
    throw new InstanceAddressError(`This server says it's ${info.domain}, not ${address.host}, so the app won't connect to it.`)
  }
  return info
}

function hostnameOf(domain: string): string | null {
  try {
    return new URL(`https://${domain}`).hostname
  } catch {
    return null
  }
}

function isInstanceInfo(v: unknown): v is InstanceInfo {
  if (typeof v !== 'object' || v === null) {
    return false
  }
  const o = v as Record<string, unknown>
  const software = o.software as Record<string, unknown> | undefined
  return (
    typeof o.name === 'string' &&
    o.name !== '' &&
    typeof o.domain === 'string' &&
    typeof o.api_version === 'string' &&
    typeof o.features === 'object' &&
    o.features !== null &&
    typeof software === 'object' &&
    software !== null &&
    typeof software.name === 'string'
  )
}
