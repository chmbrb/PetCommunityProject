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
    // API messages are lowercase sentence fragments; capitalize them for display.
    super(message.charAt(0).toUpperCase() + message.slice(1))
    this.name = 'ApiError'
  }
}

/** The signed-in account and its public identity, from GET /api/v1/me. */
/** One published size of an uploaded image. */
export interface ImageVariant {
  name: string
  width: number
  height: number
  content_type: string
  url: string
}

export interface Me {
  account: { id: string; email: string; email_verified: boolean; status: string }
  actor: { id: string; handle: string; display_name: string; avatar: ImageVariant[] }
}

/** An uploaded image, as its owner sees it. */
export interface MediaAsset {
  id: string
  purpose: string
  status: 'pending' | 'ready' | 'rejected' | 'quarantined'
  width?: number
  height?: number
  variants: ImageVariant[]
  rejection_reason?: string
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
  method: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE',
  path: string,
  body?: unknown,
  fetchImpl: typeof fetch = fetch,
): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  let payload: BodyInit | undefined
  if (body instanceof Blob) {
    // Files are sent as-is; the server identifies the format from the content.
    headers['Content-Type'] = 'application/octet-stream'
    payload = body
  } else if (body !== undefined) {
    headers['Content-Type'] = 'application/json'
    payload = JSON.stringify(body)
  }
  const res = await fetchImpl(`${apiBase()}${path}`, { method, headers, body: payload })
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

/** Emails a sign-in link if the address has an account. The answer is the same either way. */
export function requestSignInLink(email: string) {
  return request<{ status: string }>('POST', '/api/v1/auth/login/email/request', { email })
}

export function finishSignInLink(token: string) {
  return request<Me>('POST', '/api/v1/auth/login/email/finish', { token })
}

export function verifyEmail(token: string) {
  return request<void>('POST', '/api/v1/auth/email/verify', { token })
}

/** Sends another verification email to the signed-in account. */
export function resendVerification() {
  return request<{ status: string }>('POST', '/api/v1/me/email/verification')
}

/** One of the signed-in account's passkeys. */
export interface Passkey {
  id: string
  name: string
  created_at: string
  last_used_at: string | null
  /** Synced between devices by a password manager. */
  synced: boolean
}

/** A device signed in to the account. */
export interface SessionInfo {
  id: string
  client: 'web' | 'mobile'
  user_agent: string
  created_at: string
  last_seen_at: string
  /** The device making the request. */
  current: boolean
}

export async function listPasskeys() {
  return (await request<{ passkeys: Passkey[] }>('GET', '/api/v1/me/passkeys')).passkeys
}

export function beginAddPasskey() {
  return request<PasskeyChallenge<PublicKeyCredentialCreationOptionsJSON>>('POST', '/api/v1/me/passkeys/begin')
}

export function finishAddPasskey(challengeId: string, credential: RegistrationResponseJSON, name: string) {
  return request<Passkey>('POST', '/api/v1/me/passkeys/finish', { challenge_id: challengeId, credential, name })
}

export function renamePasskey(id: string, name: string) {
  return request<void>('PATCH', `/api/v1/me/passkeys/${encodeURIComponent(id)}`, { name })
}

export function removePasskey(id: string) {
  return request<void>('DELETE', `/api/v1/me/passkeys/${encodeURIComponent(id)}`)
}

export async function listSessions() {
  return (await request<{ sessions: SessionInfo[] }>('GET', '/api/v1/me/sessions')).sessions
}

export function revokeSession(id: string) {
  return request<void>('DELETE', `/api/v1/me/sessions/${encodeURIComponent(id)}`)
}

export function revokeOtherSessions() {
  return request<{ revoked: number }>('POST', '/api/v1/me/sessions/revoke-others')
}

/** Largest accepted upload, matching the server's limit. */
export const maxUploadBytes = 15 * 1024 * 1024

/** Uploads an image for processing. Poll fetchMedia until it's ready or rejected. */
export function uploadImage(file: Blob, purpose: 'avatar') {
  return request<MediaAsset>('POST', `/api/v1/media?purpose=${purpose}`, file)
}

export function fetchMedia(id: string) {
  return request<MediaAsset>('GET', `/api/v1/media/${encodeURIComponent(id)}`)
}

export function setAvatar(mediaId: string) {
  return request<Me>('PUT', '/api/v1/me/avatar', { media_id: mediaId })
}

export function removeAvatar() {
  return request<void>('DELETE', '/api/v1/me/avatar')
}

/** Picks the image size closest to (but not below) the wanted width. */
export function pickVariant(variants: ImageVariant[], width: number): ImageVariant | undefined {
  const webp = variants.filter((v) => v.content_type === 'image/webp').sort((a, b) => a.width - b.width)
  return webp.find((v) => v.width >= width) ?? webp.at(-1) ?? variants[0]
}
