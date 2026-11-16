// SPDX-License-Identifier: AGPL-3.0-or-later
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  ApiError,
  fetchInstance,
  fetchMe,
  finishEmailSignIn,
  finishSecondStep,
  logout,
  memorySessionStore,
  pickVariant,
  resolveApiBase,
  setSessionStore,
  type ImageVariant,
  type SessionStore,
} from './client'

describe('resolveApiBase', () => {
  it('uses relative URLs on the web', () => {
    expect(resolveApiBase(false, 'https://pets.example.org')).toBe('')
  })

  it('uses the chosen instance in native apps', () => {
    expect(resolveApiBase(true, 'https://pets.example.org')).toBe('https://pets.example.org')
  })

  it('needs an instance to be chosen in native apps', () => {
    expect(() => resolveApiBase(true, null)).toThrow('no instance chosen')
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

describe('app sessions', () => {
  const me = {
    account: { id: 'a1', email: 'a@example.org', email_verified: true, status: 'active', has_password: false, password_changed_at: null },
    actor: { id: 'p1', handle: 'a', display_name: 'A', avatar: [] },
    mfa: { totp_enabled_at: null, recovery_codes_left: 0 },
  }
  const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status })
  const unauthenticated = () => json({ error: { code: 'unauthenticated', message: 'not signed in' } }, 401)
  const sent = (fetchImpl: ReturnType<typeof vi.fn>) => (fetchImpl.mock.calls[0]?.[1] ?? {}) as RequestInit
  const sentBody = (fetchImpl: ReturnType<typeof vi.fn>) => JSON.parse(sent(fetchImpl).body as string)
  const sentHeaders = (fetchImpl: ReturnType<typeof vi.fn>) => sent(fetchImpl).headers as Record<string, string>

  async function signedInStore(): Promise<SessionStore> {
    const store = memorySessionStore()
    await store.save({ token: 'tok', expires_at: '2027-05-14T00:00:00Z', device_token: 'dev' })
    return store
  }

  afterEach(() => setSessionStore(null))

  it('asks for a mobile session and stores it, without returning it', async () => {
    const store = memorySessionStore()
    setSessionStore(store)
    const fetchImpl = vi.fn().mockResolvedValue(
      json({ ...me, session: { token: 'tok', expires_at: '2027-05-14T00:00:00Z', device_token: 'dev' } }),
    )
    await expect(finishEmailSignIn({ token: 'link' }, fetchImpl)).resolves.toEqual(me)
    expect(sentBody(fetchImpl)).toEqual({ token: 'link', client: 'mobile' })
    expect(store.token()).toBe('tok')
    expect(store.deviceToken()).toBe('dev')
  })

  it('keeps the device token when the API issues no new one', async () => {
    const store = await signedInStore()
    setSessionStore(store)
    const fetchImpl = vi.fn().mockResolvedValue(json({ ...me, session: { token: 'tok2', expires_at: '2027-05-14T00:00:00Z' } }))
    await finishEmailSignIn({ email: 'a@example.org', code: '123456' }, fetchImpl)
    expect(store.token()).toBe('tok2')
    expect(store.deviceToken()).toBe('dev')
  })

  it('sends the bearer and device tokens', async () => {
    setSessionStore(await signedInStore())
    const fetchImpl = vi.fn().mockResolvedValue(json(me))
    await fetchMe(fetchImpl)
    expect(sentHeaders(fetchImpl)).toMatchObject({ Authorization: 'Bearer tok', 'X-Device-Token': 'dev' })
  })

  it('forgets the token when the session has ended', async () => {
    const store = await signedInStore()
    setSessionStore(store)
    await expect(fetchMe(vi.fn().mockResolvedValue(unauthenticated()))).resolves.toBeNull()
    expect(store.token()).toBeNull()
    expect(store.deviceToken()).toBe('dev')
  })

  it('keeps the token on other 401s, such as a sign-in that needs its second step', async () => {
    const store = await signedInStore()
    setSessionStore(store)
    const fetchImpl = vi.fn().mockResolvedValue(
      json({ error: { code: 'mfa_required', message: 'second step required', mfa_token: 'm' } }, 401),
    )
    await expect(finishEmailSignIn({ token: 'link' }, fetchImpl)).rejects.toMatchObject({ code: 'mfa_required' })
    expect(store.token()).toBe('tok')
  })

  it('finishes the second step without a client field and stores the session', async () => {
    const store = memorySessionStore()
    setSessionStore(store)
    const fetchImpl = vi.fn().mockResolvedValue(json({ ...me, session: { token: 'tok', expires_at: '2027-05-14T00:00:00Z' } }))
    await expect(finishSecondStep('m', { code: '123456' }, fetchImpl)).resolves.toEqual(me)
    expect(sentBody(fetchImpl)).toEqual({ mfa_token: 'm', code: '123456' })
    expect(store.token()).toBe('tok')
  })

  it('forgets the token on sign-out, even if the request fails', async () => {
    const store = await signedInStore()
    setSessionStore(store)
    await expect(logout(vi.fn().mockRejectedValue(new TypeError('offline')))).rejects.toThrow('offline')
    expect(store.token()).toBeNull()
  })

  it('leaves web requests unchanged', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(json(me))
    await finishEmailSignIn({ token: 'link' }, fetchImpl)
    expect(sentBody(fetchImpl)).toEqual({ token: 'link' })
    expect(sentHeaders(fetchImpl)).not.toHaveProperty('Authorization')
    expect(sentHeaders(fetchImpl)).not.toHaveProperty('X-Device-Token')
  })
})
