// SPDX-License-Identifier: AGPL-3.0-or-later
import { ref } from 'vue'
import { fetchInstance, type InstanceInfo } from './api/client'

const instance = ref<InstanceInfo | null>(null)
const error = ref<string | null>(null)
let loading: Promise<void> | null = null

/** Loads instance metadata once and shares it across components. */
export function useInstance() {
  if (!loading) {
    loading = fetchInstance()
      .then((info) => {
        instance.value = info
      })
      .catch((err: unknown) => {
        error.value = err instanceof Error ? err.message : String(err)
      })
  }
  return { instance, error }
}
