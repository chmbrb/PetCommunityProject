// SPDX-License-Identifier: AGPL-3.0-or-later
import { Capacitor } from '@capacitor/core'
import { Preferences } from '@capacitor/preferences'
import { logout, setInstanceOrigin } from './api/client'
import { parseInstanceAddress, type InstanceAddress } from './instanceAddress'

/** True in the Android and iOS apps, which choose an instance; the web app is its own. */
export const isApp = Capacitor.isNativePlatform()

/** Development builds may connect to local instances over plain HTTP (ADR 0023). */
export const allowLocalInstances = import.meta.env.VITE_ALLOW_LOCAL_INSTANCES === 'true'

// Not secret, so it's kept in Preferences rather than secure storage.
const originKey = 'instance_origin'

/** The instance preselected in the picker, if this build names one. */
export const defaultInstance: InstanceAddress | null = parseOrNull(import.meta.env.VITE_DEFAULT_INSTANCE_URL)

/**
 * Instances the app knows. It grows with app releases, never from a server (ADR 0023).
 * Others can still be used, after a notice.
 */
const knownOrigins = new Set([defaultInstance?.origin].filter((o): o is string => !!o))

export function isKnownInstance(address: InstanceAddress): boolean {
  return knownOrigins.has(address.origin)
}

let connected: InstanceAddress | null = null

/** The instance the app is connected to, or null before one is chosen. */
export function connectedInstance(): InstanceAddress | null {
  return connected
}

/** Reads the chosen instance at startup and points the API client at it. */
export async function loadConnectedInstance() {
  const { value } = await Preferences.get({ key: originKey })
  // Checked again, so a stored address that this build wouldn't accept isn't used.
  connected = parseOrNull(value)
  setInstanceOrigin(connected?.origin ?? null)
}

/** Remembers the instance chosen in the picker; reload the app to start using it. */
export async function connectTo(address: InstanceAddress) {
  await Preferences.set({ key: originKey, value: address.origin })
}

/**
 * Signs out of the connected instance and forgets it; reload the app to choose another.
 * Forgetting it doesn't wait on the instance: an unreachable one can't block a switch.
 */
export async function disconnect() {
  try {
    await logout()
  } catch {
    // Not signed in, or the instance is unreachable. The token is forgotten either way.
  }
  await Preferences.remove({ key: originKey })
}

function parseOrNull(value: string | null | undefined): InstanceAddress | null {
  if (!value) {
    return null
  }
  try {
    return parseInstanceAddress(value, allowLocalInstances)
  } catch {
    return null
  }
}
