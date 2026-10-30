<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { browserSupportsWebAuthn } from '@simplewebauthn/browser'
import { describePasskeyError } from '../passkeyErrors'
import { useSession } from '../useSession'

const router = useRouter()
const { signIn } = useSession()

const supported = browserSupportsWebAuthn()
const error = ref<string | null>(null)
const busy = ref(false)

async function submit() {
  error.value = null
  busy.value = true
  try {
    await signIn()
    await router.push({ name: 'home' })
  } catch (err) {
    error.value = describePasskeyError(err)
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
    <p class="text-body-1 mb-6">
      Use the passkey you created for this site.
    </p>

    <v-alert
      v-if="!supported"
      type="warning"
      variant="tonal"
      class="mb-4"
    >
      This browser doesn't support passkeys. Please use an up-to-date browser.
    </v-alert>

    <v-alert
      v-if="error"
      type="error"
      variant="tonal"
      class="mb-4"
    >
      {{ error }}
    </v-alert>

    <v-btn
      color="primary"
      size="large"
      block
      :loading="busy"
      :disabled="!supported"
      @click="submit"
    >
      Sign in with a passkey
    </v-btn>

    <p class="text-body-2 mt-6">
      New here?
      <router-link :to="{ name: 'signup' }">
        Create an account
      </router-link>
    </p>
  </v-container>
</template>
