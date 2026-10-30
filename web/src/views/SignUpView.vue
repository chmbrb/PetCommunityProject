<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { browserSupportsWebAuthn } from '@simplewebauthn/browser'
import { ApiError } from '../api/client'
import { describePasskeyError } from '../passkeyErrors'
import { useSession } from '../useSession'

const router = useRouter()
const { signUp } = useSession()

const supported = browserSupportsWebAuthn()
const form = reactive({ email: '', handle: '', display_name: '' })
const fieldErrors = reactive<Record<string, string>>({})
const error = ref<string | null>(null)
const busy = ref(false)

async function submit() {
  error.value = null
  for (const key of Object.keys(fieldErrors)) {
    delete fieldErrors[key]
  }
  busy.value = true
  try {
    await signUp({ ...form })
    await router.push({ name: 'home' })
  } catch (err) {
    if (err instanceof ApiError && err.field) {
      fieldErrors[err.field] = err.message
    } else {
      error.value = describePasskeyError(err)
    }
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
      Create an account
    </h1>
    <p class="text-body-1 mb-6">
      You'll sign in with a passkey: your fingerprint, face, or device PIN. There's no
      password to remember.
    </p>

    <v-alert
      v-if="!supported"
      type="warning"
      variant="tonal"
      class="mb-4"
    >
      This browser doesn't support passkeys. Please use an up-to-date browser.
    </v-alert>

    <v-form @submit.prevent="submit">
      <v-text-field
        v-model="form.display_name"
        label="Your name"
        autocomplete="name"
        :error-messages="fieldErrors.display_name"
        :disabled="busy"
        required
      />
      <v-text-field
        v-model="form.handle"
        label="Handle"
        prefix="@"
        hint="3–30 lowercase letters, numbers, or underscores. Shown publicly."
        persistent-hint
        autocomplete="username"
        autocapitalize="none"
        spellcheck="false"
        :error-messages="fieldErrors.handle"
        :disabled="busy"
        class="mb-2"
        required
      />
      <v-text-field
        v-model="form.email"
        label="Email"
        type="email"
        hint="Kept private. Used to recover your account."
        persistent-hint
        autocomplete="email"
        :error-messages="fieldErrors.email"
        :disabled="busy"
        class="mb-4"
        required
      />

      <v-alert
        v-if="error"
        type="error"
        variant="tonal"
        class="mb-4"
      >
        {{ error }}
      </v-alert>

      <v-btn
        type="submit"
        color="primary"
        size="large"
        block
        :loading="busy"
        :disabled="!supported"
      >
        Create account with a passkey
      </v-btn>
    </v-form>

    <p class="text-body-2 mt-6">
      Already have an account?
      <router-link :to="{ name: 'signin' }">
        Sign in
      </router-link>
    </p>
  </v-container>
</template>
