<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { ApiError } from '../api/client'
import { takeLinkToken } from '../linkToken'
import { useSession } from '../useSession'

const router = useRouter()
const { signInWithLink } = useSession()

// Signing in takes a click, not just opening the link: email security scanners often
// open links automatically, and would otherwise use up the one-time token.
const token = takeLinkToken()
const error = ref<string | null>(token ? null : 'This sign-in link is incomplete. Please request a new one.')
const busy = ref(false)

async function finish() {
  if (!token) {
    return
  }
  error.value = null
  busy.value = true
  try {
    await signInWithLink(token)
    await router.replace({ name: 'home' })
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : 'Something went wrong. Please try again.'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <v-container
    class="py-8"
    max-width="480"
  >
    <h1 class="text-h4 mb-2">
      Sign in
    </h1>

    <v-alert
      v-if="error"
      type="error"
      variant="tonal"
      class="mb-4"
    >
      {{ error }}
    </v-alert>

    <template v-if="token">
      <p class="text-body-1 mb-6">
        Finish signing in with the link from your email.
      </p>
      <v-btn
        color="primary"
        size="large"
        block
        :loading="busy"
        @click="finish"
      >
        Finish signing in
      </v-btn>
    </template>

    <p class="text-body-2 mt-6">
      <router-link :to="{ name: 'signin' }">
        Request a new sign-in link
      </router-link>
    </p>
  </v-container>
</template>
