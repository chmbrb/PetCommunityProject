// SPDX-License-Identifier: AGPL-3.0-or-later
import { describe, expect, it, vi } from 'vitest'
import { ApiError, fetchInstance, fetchMe, pickVariant, resolveApiBase, type ImageVariant } from './client'

describe('resolveApiBase', () => {
  it('uses relative URLs on the web', () => {
    expect(resolveApiBase(false, 'https://pets.example.org')).toBe('')
  })

  it('uses the configured instance in native apps, without a trailing slash', () => {
    expect(resolveApiBase(true, 'https://pets.example.org/')).toBe('https://pets.example.org')
  })

  it('requires an instance URL in native apps', () => {
    expect(() => resolveApiBase(true, undefined)).toThrow('VITE_DEFAULT_INSTANCE_URL')
  })
})

describe('fetchInstance', () => {
  it('returns instance metadata', async () => {
    const body = { name: 'Test', domain: '', version: 'dev', api_version: 'v1', source_url: 'https://x', features: {} }
    const fetchImpl = vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200 }))
    await expect(fetchInstance(fetchImpl)).resolves.toEqual(body)
    expect(fetchImpl).toHaveBeenCalledWith('/api/v1/instance', expect.anything())
  })

  it('turns API error bodies into ApiError', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ error: { code: 'internal', message: 'internal error' } }), { status: 500 }),
    )
    await expect(fetchInstance(fetchImpl)).rejects.toMatchObject(
      new ApiError(500, 'internal', 'internal error'),
    )
  })
})

describe('fetchMe', () => {
  it('returns null when nobody is signed in', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ error: { code: 'unauthenticated', message: 'not signed in' } }), { status: 401 }),
    )
    await expect(fetchMe(fetchImpl)).resolves.toBeNull()
  })

  it('keeps the invalid field from validation errors', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({ error: { code: 'validation_failed', message: 'bad', field: 'email' } }),
        { status: 422 },
      ),
    )
    await expect(fetchMe(fetchImpl)).rejects.toMatchObject({ status: 422, field: 'email' })
  })
})

describe('pickVariant', () => {
  const v = (name: string, width: number, type = 'image/webp'): ImageVariant => ({
    name, width, height: width, content_type: type, url: `https://media.test/${name}`,
  })
  const variants = [v('800.jpg', 800, 'image/jpeg'), v('1600.webp', 1600), v('320.webp', 320), v('800.webp', 800)]

  it('picks the smallest WebP at least as wide as needed', () => {
    expect(pickVariant(variants, 160)?.name).toBe('320.webp')
    expect(pickVariant(variants, 500)?.name).toBe('800.webp')
  })

  it('falls back to the largest WebP, then anything', () => {
    expect(pickVariant(variants, 4000)?.name).toBe('1600.webp')
    expect(pickVariant([v('800.jpg', 800, 'image/jpeg')], 100)?.name).toBe('800.jpg')
    expect(pickVariant([], 100)).toBeUndefined()
  })
})
