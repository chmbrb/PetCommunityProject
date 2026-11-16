// SPDX-License-Identifier: AGPL-3.0-or-later
import { beforeEach, describe, expect, it, vi } from 'vitest'

const prefs = new Map<string, string>()

vi.mock('@capacitor/preferences', () => ({
  Preferences: {
    get: vi.fn(async ({ key }: { key: string }) => ({ value: prefs.get(key) ?? null })),
    set: vi.fn(async ({ key, value }: { key: string; value: string }) => {
      prefs.set(key, value)
    }),
    remove: vi.fn(async ({ key }: { key: string }) => {
      prefs.delete(key)
    }),
  },
}))

vi.mock('./api/client', () => ({
  logout: vi.fn(async () => undefined),
  setInstanceOrigin: vi.fn(),
}))

const client = await import('./api/client')
const { connectTo, connectedInstance, disconnect, loadConnectedInstance } = await import('./appInstance')
const { parseInstanceAddress } = await import('./instanceAddress')

describe('the connected instance', () => {
  beforeEach(() => {
    prefs.clear()
    vi.clearAllMocks()
  })

  it('starts with none', async () => {
    await loadConnectedInstance()
    expect(connectedInstance()).toBeNull()
    expect(client.setInstanceOrigin).toHaveBeenCalledWith(null)
  })

  it('is remembered and points the API client at it on the next start', async () => {
    await connectTo(parseInstanceAddress('pets.example.org'))
    await loadConnectedInstance()
    expect(connectedInstance()?.host).toBe('pets.example.org')
    expect(client.setInstanceOrigin).toHaveBeenCalledWith('https://pets.example.org')
  })

  it("ignores a stored address this build wouldn't accept", async () => {
    prefs.set('instance_origin', 'http://10.0.2.2:8080')
    await loadConnectedInstance()
    expect(connectedInstance()).toBeNull()
  })

  it('is forgotten on disconnect, even when signing out fails', async () => {
    prefs.set('instance_origin', 'https://pets.example.org')
    vi.mocked(client.logout).mockRejectedValueOnce(new TypeError('offline'))
    await disconnect()
    expect(client.logout).toHaveBeenCalled()
    expect(prefs.has('instance_origin')).toBe(false)
  })
})
