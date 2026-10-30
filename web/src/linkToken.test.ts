// SPDX-License-Identifier: AGPL-3.0-or-later
import { describe, expect, it, vi } from 'vitest'
import { takeLinkToken } from './linkToken'

function fakeLocation(hash: string) {
  return { hash, pathname: '/signin/email', search: '' } as Location
}

describe('takeLinkToken', () => {
  it('returns the token and removes it from the address bar', () => {
    const hist = { state: null, replaceState: vi.fn() } as unknown as History
    expect(takeLinkToken(fakeLocation('#token=abc_123-XYZ'), hist)).toBe('abc_123-XYZ')
    expect(hist.replaceState).toHaveBeenCalledWith(null, '', '/signin/email')
  })

  it('returns null and leaves the URL alone without a token', () => {
    const hist = { state: null, replaceState: vi.fn() } as unknown as History
    expect(takeLinkToken(fakeLocation(''), hist)).toBeNull()
    expect(hist.replaceState).not.toHaveBeenCalled()
  })
})
