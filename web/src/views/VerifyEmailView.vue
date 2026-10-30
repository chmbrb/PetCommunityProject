<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ApiError, verifyEmail } from '../api/client'
import { takeLinkToken } from '../linkToken'
import { useSession } from '../useSession'

const { me, refresh } = useSession()

type State = 'verifying' | 'verified' | 'failed'
const state = ref<State>('verifying')
const error = ref('')

onMounted(async () => {
  const token = takeLinkToken()
  if (!token) {
    state.value = 'failed'
    error.value = 'This confirmation link is incomplete.'
    return
  }
  try {
    await verifyEmail(token)
    state.value = 'verified'
    if (me.value) {
      await refresh()
    }
  } catch (err) {
    state.value = 'failed'
    error.value = err instanceof ApiError ? err.message : 'Something went wrong. Please try again.'
  }
})
</script>

<template>
  <v-container
    class="py-8"
    max-width="480"
  >
    <h1 class="text-h4 mb-4">
      Confirm your email
    </h1>

    <v-progress-linear
      v-if="state === 'verifying'"
      indeterminate
      color="primary"
    />

    <v-alert
      v-else-if="state === 'verified'"
      type="success"
      variant="tonal"
      title="Email confirmed"
    >
      Thanks! Your email address is confirmed.
    </v-alert>

    <template v-else>
      <v-alert
        type="error"
        variant="tonal"
        class="mb-4"
      >
        {{ error }}
      </v-alert>
      <p class="text-body-2">
        Sign in and use "Send a new link" on the home page to get a new confirmation email.
      </p>
    </template>

    <p class="text-body-2 mt-6">
      <router-link :to="{ name: 'home' }">
        Go to the home page
      </router-link>
    </p>
  </v-container>
</template>
