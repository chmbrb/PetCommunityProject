// SPDX-License-Identifier: AGPL-3.0-or-later
import { describe, expect, it, vi } from 'vitest'
import { checkInstance, parseInstanceAddress } from './instanceAddress'

describe('parseInstanceAddress', () => {
  it('adds https:// and keeps only the origin', () => {
    expect(parseInstanceAddress('  Pets.Example.org ')).toEqual({
      origin: 'https://pets.example.org',
      host: 'pets.example.org',
      hostname: 'pets.example.org',
    })
    expect(parseInstanceAddress('https://pets.example.org/').origin).toBe('https://pets.example.org')
    expect(parseInstanceAddress('pets.example.org:8443').origin).toBe('https://pets.example.org:8443')
  })

  it('shows Unicode domains in their ASCII form', () => {
    expect(parseInstanceAddress('pëts.example').host).toBe('xn--pts-jma.example')
  })

  it.each([
    ['', "Enter the instance's address."],
    ['http://pets.example.org', 'Only https://'],
    ['ftp://pets.example.org', 'Only https://'],
    ['https://me@pets.example.org', 'user name'],
    ['pets.example.org/signin', 'without a path'],
    ['pets.example.org?x=1', 'without a path'],
    ['pets.example.org?', 'without a path'],
    ['pets.example.org#top', 'without a path'],
    ['https://', "isn't a valid address"],
  ])('rejects %j', (input, message) => {
    expect(() => parseInstanceAddress(input)).toThrow(message)
  })

  it('allows plain HTTP only to local hosts, and only when allowed', () => {
    expect(() => parseInstanceAddress('http://10.0.2.2:8080')).toThrow('Only https://')
    expect(parseInstanceAddress('http://10.0.2.2:8080', true).origin).toBe('http://10.0.2.2:8080')
    expect(parseInstanceAddress('http://localhost:8080', true).origin).toBe('http://localhost:8080')
    expect(parseInstanceAddress('http://192.168.1.20', true).origin).toBe('http://192.168.1.20')
    expect(() => parseInstanceAddress('http://pets.example.org', true)).toThrow('Only https://')
    expect(() => parseInstanceAddress('http://172.32.0.1', true)).toThrow('Only https://')
  })
})

describe('checkInstance', () => {
  const address = parseInstanceAddress('pets.example.org')
  const info = {
    name: 'Example Pets',
    domain: 'pets.example.org',
    version: '1.0.0',
    api_version: 'v1',
    source_url: 'https://example.org/src',
    features: {},
    software: { name: 'petcommunity', version: '1.0.0' },
  }
  const respond = (body: unknown, status = 200) =>
    vi.fn().mockResolvedValue(new Response(typeof body === 'string' ? body : JSON.stringify(body), { status }))

  it('accepts an instance that says it is the domain entered', async () => {
    const fetchImpl = respond(info)
    await expect(checkInstance(address, { fetchImpl })).resolves.toEqual(info)
    expect(fetchImpl).toHaveBeenCalledWith(
      'https://pets.example.org/api/v1/instance',
      expect.objectContaining({ redirect: 'error', credentials: 'omit' }),
    )
  })

  it('compares domains in any case or Unicode form', async () => {
    const unicode = parseInstanceAddress('pëts.example')
    await expect(checkInstance(unicode, { fetchImpl: respond({ ...info, domain: 'PËTS.example' }) })).resolves.toBeTruthy()
  })

  it('rejects an instance claiming another domain', async () => {
    await expect(checkInstance(address, { fetchImpl: respond({ ...info, domain: 'pets.example.com' }) })).rejects.toThrow(
      "This server says it's pets.example.com, not pets.example.org",
    )
  })

  it.each([
    ['other software', { ...info, software: { name: 'mastodon', version: '4' } }],
    ['a missing software field', { ...info, software: undefined }],
    ['an empty name', { ...info, name: '' }],
    ['not JSON', '<html></html>'],
  ])('rejects %s', async (_, body) => {
    await expect(checkInstance(address, { fetchImpl: respond(body) })).rejects.toThrow("isn't a PetCommunity instance")
  })

  it('rejects error responses', async () => {
    await expect(checkInstance(address, { fetchImpl: respond(info, 404) })).rejects.toThrow("isn't a PetCommunity instance")
  })

  it('rejects API versions the app does not support', async () => {
    await expect(checkInstance(address, { fetchImpl: respond({ ...info, api_version: 'v2' }) })).rejects.toThrow(
      "doesn't support",
    )
  })

  it('reports unreachable servers and redirects the same way', async () => {
    const fetchImpl = vi.fn().mockRejectedValue(new TypeError('Failed to fetch'))
    await expect(checkInstance(address, { fetchImpl })).rejects.toThrow("Couldn't reach pets.example.org")
  })

  it('accepts an instance without a domain only when local instances are allowed', async () => {
    const local = parseInstanceAddress('http://10.0.2.2:8080', true)
    await expect(checkInstance(local, { fetchImpl: respond({ ...info, domain: '' }) })).rejects.toThrow(
      "doesn't say which domain",
    )
    await expect(
      checkInstance(local, { allowLocal: true, fetchImpl: respond({ ...info, domain: '' }) }),
    ).resolves.toBeTruthy()
  })
})
