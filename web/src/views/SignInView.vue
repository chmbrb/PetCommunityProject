<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { browserSupportsWebAuthn } from '@simplewebauthn/browser'
import { ApiError, requestSignInLink } from '../api/client'
import EmailCodeForm from '../components/EmailCodeForm.vue'
import { describePasskeyError } from '../passkeyErrors'
import { useSession } from '../useSession'

const router = useRouter()
const { signIn, signInWithEmail } = useSession()

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

async function confirmCode(code: string) {
  await signInWithEmail({ email: linkEmail.value, code })
  await router.push({ name: 'home' })
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

    <template v-if="supported">
      <p class="text-body-1 mb-6">
        Use a passkey you created for this site, or get a sign-in email.
      </p>

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
    </template>

    <h2 class="text-h6 mb-2">
      Get a sign-in email
    </h2>
    <template v-if="linkSent">
      <p class="text-body-1 mb-4">
        If an account uses <strong>{{ linkEmail }}</strong>, we've sent it a sign-in link and
        code. Open the link, or enter the code here. Both work once, for 15 minutes.
      </p>
      <EmailCodeForm
        :submit="confirmCode"
        label="Sign in"
      />
      <p class="text-body-2 mt-4">
        No email? Check your spam folder, or
        <a
          href="#"
          @click.prevent="linkSent = false"
        >send another</a>.
      </p>
    </template>
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
        Email me a sign-in link and code
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
