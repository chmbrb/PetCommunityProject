// SPDX-License-Identifier: AGPL-3.0-or-later
import { Capacitor } from '@capacitor/core'
import type {
  AuthenticationResponseJSON,
  PublicKeyCredentialCreationOptionsJSON,
  PublicKeyCredentialRequestOptionsJSON,
  RegistrationResponseJSON,
} from '@simplewebauthn/browser'

/** Instance metadata from GET /api/v1/instance. */
export interface InstanceInfo {
  name: string
  domain: string
  version: string
  api_version: string
  source_url: string
  features: Record<string, boolean>
}

/** Error shape returned by every API endpoint. */
export interface ApiErrorBody {
  error: { code: string; message: string; field?: string }
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly field?: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

/** The signed-in account and its public identity, from GET /api/v1/me. */
export interface Me {
  account: { id: string; email: string; email_verified: boolean; status: string }
  actor: { id: string; handle: string; display_name: string }
}

/** Returned by the first step of sign-up and sign-in: passkey options for the browser. */
export interface PasskeyChallenge<Options> {
  challenge_id: string
  options: Options
}

export interface SignupDetails {
  email: string
  handle: string
  display_name: string
}

/**
 * Returns the origin API requests go to. The web app is served by its own
 * instance, so it uses relative URLs. Native apps must name an instance (ADR 0018).
 */
export function resolveApiBase(isNative: boolean, defaultInstanceUrl: string | undefined): string {
  if (!isNative) {
    return ''
  }
  if (!defaultInstanceUrl) {
    throw new Error('VITE_DEFAULT_INSTANCE_URL must be set for native builds')
  }
  return defaultInstanceUrl.replace(/\/+$/, '')
}

const apiBase = () =>
  resolveApiBase(Capacitor.isNativePlatform(), import.meta.env.VITE_DEFAULT_INSTANCE_URL)

async function request<T>(
  method: 'GET' | 'POST',
  path: string,
  body?: unknown,
  fetchImpl: typeof fetch = fetch,
): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json'
  }
  const res = await fetchImpl(`${apiBase()}${path}`, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!res.ok) {
    let code = 'http_error'
    let message = res.statusText
    let field: string | undefined
    try {
      const err = ((await res.json()) as ApiErrorBody).error
      code = err.code
      message = err.message
      field = err.field
    } catch {
      // Not a JSON error body (e.g. a proxy error page); keep the HTTP status text.
    }
    throw new ApiError(res.status, code, message, field)
  }
  if (res.status === 204) {
    return undefined as T
  }
  return (await res.json()) as T
}

export function fetchInstance(fetchImpl?: typeof fetch): Promise<InstanceInfo> {
  return request<InstanceInfo>('GET', '/api/v1/instance', undefined, fetchImpl)
}

/** Returns the signed-in account, or null when nobody is signed in. */
export async function fetchMe(fetchImpl?: typeof fetch): Promise<Me | null> {
  try {
    return await request<Me>('GET', '/api/v1/me', undefined, fetchImpl)
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) {
      return null
    }
    throw err
  }
}

export function beginSignup(details: SignupDetails) {
  return request<PasskeyChallenge<PublicKeyCredentialCreationOptionsJSON>>(
    'POST',
    '/api/v1/auth/signup/begin',
    details,
  )
}

export function finishSignup(challengeId: string, credential: RegistrationResponseJSON) {
  return request<Me>('POST', '/api/v1/auth/signup/finish', { challenge_id: challengeId, credential })
}

export function beginLogin() {
  return request<PasskeyChallenge<PublicKeyCredentialRequestOptionsJSON>>('POST', '/api/v1/auth/login/begin')
}

export function finishLogin(challengeId: string, credential: AuthenticationResponseJSON) {
  return request<Me>('POST', '/api/v1/auth/login/finish', { challenge_id: challengeId, credential })
}

export function logout() {
  return request<void>('POST', '/api/v1/auth/logout')
}
