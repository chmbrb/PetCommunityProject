<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { browserSupportsWebAuthn } from '@simplewebauthn/browser'
import { ApiError, requestSignInLink } from '../api/client'
import { describePasskeyError } from '../passkeyErrors'
import { useSession } from '../useSession'

const router = useRouter()
const { signIn } = useSession()

const supported = browserSupportsWebAuthn()
const error = ref<string | null>(null)
const busy = ref(false)

const linkEmail = ref('')
const linkError = ref<string | null>(null)
const linkSent = ref(false)
const linkBusy = ref(false)

async function sendLink() {
  linkError.value = null
  linkBusy.value = true
  try {
    await requestSignInLink(linkEmail.value)
    linkSent.value = true
  } catch (err) {
    linkError.value = err instanceof ApiError ? err.message : 'Something went wrong. Please try again.'
  } finally {
    linkBusy.value = false
  }
}

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

    <div class="d-flex align-center ga-3 my-8">
      <v-divider />
      <span class="text-body-2 text-medium-emphasis">or</span>
      <v-divider />
    </div>

    <h2 class="text-h6 mb-2">
      Get a sign-in link by email
    </h2>
    <v-alert
      v-if="linkSent"
      type="info"
      variant="tonal"
    >
      If an account uses {{ linkEmail }}, we've sent it a sign-in link. The link works once,
      for 15 minutes.
    </v-alert>
    <v-form
      v-else
      @submit.prevent="sendLink"
    >
      <v-text-field
        v-model="linkEmail"
        label="Email"
        type="email"
        autocomplete="email"
        :error-messages="linkError ?? undefined"
        :disabled="linkBusy"
        required
      />
      <v-btn
        type="submit"
        variant="outlined"
        size="large"
        block
        :loading="linkBusy"
      >
        Email me a sign-in link
      </v-btn>
    </v-form>

    <p class="text-body-2 mt-6">
      New here?
      <router-link :to="{ name: 'signup' }">
        Create an account
      </router-link>
    </p>
  </v-container>
</template>
