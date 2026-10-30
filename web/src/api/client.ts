// SPDX-License-Identifier: AGPL-3.0-or-later
import { Capacitor } from '@capacitor/core'

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
  error: { code: string; message: string }
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
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

async function getJSON<T>(path: string, fetchImpl: typeof fetch = fetch): Promise<T> {
  const res = await fetchImpl(`${apiBase()}${path}`, { headers: { Accept: 'application/json' } })
  if (!res.ok) {
    let code = 'http_error'
    let message = res.statusText
    try {
      const body = (await res.json()) as ApiErrorBody
      code = body.error.code
      message = body.error.message
    } catch {
      // Not a JSON error body (e.g. a proxy error page); keep the HTTP status text.
    }
    throw new ApiError(res.status, code, message)
  }
  return (await res.json()) as T
}

export function fetchInstance(fetchImpl?: typeof fetch): Promise<InstanceInfo> {
  return getJSON<InstanceInfo>('/api/v1/instance', fetchImpl)
}
