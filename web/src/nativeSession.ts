// SPDX-License-Identifier: AGPL-3.0-or-later
import { KeychainAccess, SecureStorage } from '@aparajita/capacitor-secure-storage'
import { Preferences } from '@capacitor/preferences'
import type { AppSession, SessionStore } from './api/client'

const tokenKey = 'session_token'
const deviceTokenKey = 'device_token'
// Kept in Preferences, which are deleted with the app, unlike the iOS Keychain.
const installedKey = 'installed'

// Readable in the background after the first unlock, never copied to backups or a new
// phone, and never synced to iCloud Keychain (ADR 0023).
const access = KeychainAccess.afterFirstUnlockThisDeviceOnly

/**
 * Opens the app's session storage in the Keychain or the Android Keystore. Only the
 * native apps load this: on the web, the plugin falls back to localStorage.
 */
export async function openNativeSessionStore(): Promise<SessionStore> {
  await SecureStorage.setSynchronize(false)
  await SecureStorage.setDefaultKeychainAccess(access)

  // iOS keeps Keychain items after the app is deleted, so a reinstall starts clean.
  const { value: installed } = await Preferences.get({ key: installedKey })
  if (!installed) {
    await SecureStorage.clear()
    await Preferences.set({ key: installedKey, value: '1' })
  }

  let token = await read(tokenKey)
  let deviceToken = await read(deviceTokenKey)

  return {
    token: () => token,
    deviceToken: () => deviceToken,
    async save(session: AppSession) {
      await SecureStorage.set(tokenKey, session.token, false, false, access)
      token = session.token
      if (session.device_token) {
        await SecureStorage.set(deviceTokenKey, session.device_token, false, false, access)
        deviceToken = session.device_token
      }
    },
    async clear() {
      token = null
      await SecureStorage.remove(tokenKey, false)
    },
  }
}

/**
 * Reads a stored string. A value that can't be read (a reset Keystore, or data restored
 * from elsewhere) is deleted and treated as missing, which signs the app out.
 */
async function read(key: string): Promise<string | null> {
  try {
    const value = await SecureStorage.get(key, false, false)
    return typeof value === 'string' ? value : null
  } catch {
    await SecureStorage.remove(key, false).catch(() => undefined)
    return null
  }
}
