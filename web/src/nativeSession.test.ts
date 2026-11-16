// SPDX-License-Identifier: AGPL-3.0-or-later
import { beforeEach, describe, expect, it, vi } from 'vitest'

// In-memory stand-ins for the Keychain/Keystore and for Preferences.
const secure = new Map<string, unknown>()
const unreadable = new Set<string>()
const prefs = new Map<string, string>()

vi.mock('@aparajita/capacitor-secure-storage', () => ({
  KeychainAccess: { afterFirstUnlockThisDeviceOnly: 3 },
  SecureStorage: {
    setSynchronize: vi.fn(async () => undefined),
    setDefaultKeychainAccess: vi.fn(async () => undefined),
    get: vi.fn(async (key: string) => {
      if (unreadable.has(key)) {
        throw new Error('osError')
      }
      return secure.get(key) ?? null
    }),
    set: vi.fn(async (key: string, value: unknown) => {
      secure.set(key, value)
    }),
    remove: vi.fn(async (key: string) => secure.delete(key)),
    clear: vi.fn(async () => secure.clear()),
  },
}))

vi.mock('@capacitor/preferences', () => ({
  Preferences: {
    get: vi.fn(async ({ key }: { key: string }) => ({ value: prefs.get(key) ?? null })),
    set: vi.fn(async ({ key, value }: { key: string; value: string }) => {
      prefs.set(key, value)
    }),
  },
}))

const { SecureStorage } = await import('@aparajita/capacitor-secure-storage')
const { openNativeSessionStore } = await import('./nativeSession')

describe('openNativeSessionStore', () => {
  beforeEach(() => {
    secure.clear()
    unreadable.clear()
    prefs.clear()
    vi.clearAllMocks()
  })

  it('never syncs to iCloud and keeps items on this device only', async () => {
    await openNativeSessionStore()
    expect(SecureStorage.setSynchronize).toHaveBeenCalledWith(false)
    expect(SecureStorage.setDefaultKeychainAccess).toHaveBeenCalledWith(3)
  })

  it('clears what a previous install left in the Keychain', async () => {
    secure.set('session_token', 'old')
    const store = await openNativeSessionStore()
    expect(store.token()).toBeNull()
    expect(prefs.get('installed')).toBe('1')
  })

  it('keeps the session on later launches', async () => {
    prefs.set('installed', '1')
    secure.set('session_token', 'tok')
    secure.set('device_token', 'dev')
    const store = await openNativeSessionStore()
    expect(SecureStorage.clear).not.toHaveBeenCalled()
    expect(store.token()).toBe('tok')
    expect(store.deviceToken()).toBe('dev')
  })

  it('deletes a value it can no longer read and treats it as signed out', async () => {
    prefs.set('installed', '1')
    secure.set('session_token', 'garbled')
    unreadable.add('session_token')
    const store = await openNativeSessionStore()
    expect(store.token()).toBeNull()
    expect(secure.has('session_token')).toBe(false)
  })

  it('saves the session on this device only, and clears it without the device token', async () => {
    const store = await openNativeSessionStore()
    await store.save({ token: 'tok', expires_at: '2027-05-14T00:00:00Z', device_token: 'dev' })
    expect(SecureStorage.set).toHaveBeenCalledWith('session_token', 'tok', false, false, 3)
    expect(SecureStorage.set).toHaveBeenCalledWith('device_token', 'dev', false, false, 3)

    await store.clear()
    expect(store.token()).toBeNull()
    expect(secure.has('session_token')).toBe(false)
    expect(store.deviceToken()).toBe('dev')
    expect(secure.get('device_token')).toBe('dev')
  })
})
