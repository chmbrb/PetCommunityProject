// SPDX-License-Identifier: AGPL-3.0-or-later
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createVuetify } from 'vuetify'

vi.mock('../appInstance', async () => {
  const { parseInstanceAddress } = await import('../instanceAddress')
  const defaultInstance = parseInstanceAddress('pets.example.org')
  return {
    allowLocalInstances: false,
    defaultInstance,
    isKnownInstance: (a: { origin: string }) => a.origin === defaultInstance.origin,
    connectTo: vi.fn(async () => undefined),
  }
})

const { connectTo } = await import('../appInstance')
const { default: InstanceView } = await import('./InstanceView.vue')

const instanceInfo = (domain: string, name = 'Example Pets') => ({
  name,
  domain,
  version: '1.0.0',
  api_version: 'v1',
  source_url: 'https://example.org/src',
  features: {},
  software: { name: 'petcommunity', version: '1.0.0' },
})

function respondWith(body: unknown) {
  const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200 }))
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

const mountView = () => mount(InstanceView, { global: { plugins: [createVuetify()] }, attachTo: document.body })

async function submit(wrapper: ReturnType<typeof mountView>) {
  await wrapper.find('form').trigger('submit')
  await flushPromises()
}

describe('InstanceView', () => {
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
    vi.clearAllMocks()
    document.body.innerHTML = ''
  })

  it('checks the preselected default instance and confirms it by its domain', async () => {
    const fetchMock = respondWith(instanceInfo('pets.example.org'))
    const wrapper = mountView()
    await submit(wrapper)

    expect(fetchMock).toHaveBeenCalledWith('https://pets.example.org/api/v1/instance', expect.anything())
    const text = wrapper.text()
    expect(text).toContain('Connect to this instance?')
    expect(text).toContain('pets.example.org')
    expect(text).toContain('Example Pets')
    expect(text).toContain("This instance is on the app's list.")
    expect(text).not.toContain("isn't on the app's list")
  })

  it("warns about an instance that isn't on the list", async () => {
    respondWith(instanceInfo('other.example', 'Official PetCommunity'))
    const wrapper = mountView()
    await wrapper.findAll('input[type="radio"]')[1]?.setValue(true)
    await wrapper.find('input[type="url"]').setValue('other.example')
    await submit(wrapper)

    const text = wrapper.text()
    expect(text).toContain('other.example')
    expect(text).toContain("This instance isn't on the app's list. Only continue if you trust")
  })

  it('shows why an instance was rejected and stays on the first step', async () => {
    respondWith(instanceInfo('pets.example.com'))
    const wrapper = mountView()
    await submit(wrapper)

    expect(wrapper.text()).toContain("This server says it's pets.example.com, not pets.example.org")
    expect(wrapper.text()).toContain('Choose an instance')
    expect(connectTo).not.toHaveBeenCalled()
  })

  it('remembers the instance once confirmed', async () => {
    respondWith(instanceInfo('pets.example.org'))
    const wrapper = mountView()
    await submit(wrapper)
    const connect = wrapper.findAll('button').find((b) => b.text().startsWith('Connect to'))
    // jsdom can't reload the page, so only the stored choice is checked here.
    const replace = vi.fn()
    vi.spyOn(window, 'location', 'get').mockReturnValue({ ...window.location, replace })
    await connect?.trigger('click')
    await flushPromises()

    expect(connectTo).toHaveBeenCalledWith(expect.objectContaining({ origin: 'https://pets.example.org' }))
    expect(replace).toHaveBeenCalledWith('/')
  })
})
