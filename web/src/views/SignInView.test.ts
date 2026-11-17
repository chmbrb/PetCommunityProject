// SPDX-License-Identifier: AGPL-3.0-or-later
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { createVuetify } from 'vuetify'
import type { InstanceInfo } from '../api/client'

const instance = ref<InstanceInfo | null>(null)

vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))
vi.mock('../useInstance', () => ({ useInstance: () => ({ instance, error: ref(null) }) }))
vi.mock('../useSession', () => ({
  useSession: () => ({ signIn: vi.fn(), signInWithEmail: vi.fn(), signInWithPassword: vi.fn(), me: ref(null) }),
}))

const { default: SignInView } = await import('./SignInView.vue')

function withFeatures(features: Record<string, boolean>): InstanceInfo {
  return {
    name: 'Example Pets',
    domain: 'pets.example.org',
    version: '1.0.0',
    api_version: 'v1',
    source_url: 'https://example.org/src',
    features,
    software: { name: 'petcommunity', version: '1.0.0' },
  }
}

const mountView = () => mount(SignInView, { global: { plugins: [createVuetify()] }, attachTo: document.body })

describe('SignInView', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'ResizeObserver',
      class {
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    )
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    document.body.innerHTML = ''
  })

  it('offers password sign-in normally', () => {
    instance.value = withFeatures({ password_sign_in: true })
    const text = mountView().text()
    expect(document.querySelector('input[type="password"]')).not.toBeNull()
    expect(text).toContain('Sign in with password')
    expect(text).not.toContain('paused')
  })

  it('hides the password field while password sign-in is paused', () => {
    instance.value = withFeatures({ password_sign_in: false })
    const text = mountView().text()
    expect(document.querySelector('input[type="password"]')).toBeNull()
    expect(text).not.toContain('Sign in with password')
    expect(text).toContain('Signing in with a password is paused')
    expect(text).toContain('Email me a sign-in code')
  })
})
