<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { browserSupportsWebAuthn } from '@simplewebauthn/browser'
import { ApiError, requestSignInLink } from '../api/client'
import EmailCodeForm from '../components/EmailCodeForm.vue'
import PasswordField from '../components/PasswordField.vue'
import SecondStepForm from '../components/SecondStepForm.vue'
import { describePasskeyError } from '../passkeyErrors'
import { useSession } from '../useSession'

const router = useRouter()
const { signIn, signInWithEmail, signInWithPassword } = useSession()

const supported = browserSupportsWebAuthn()
const passkeyError = ref<string | null>(null)
const passkeyBusy = ref(false)

const email = ref('')
const password = ref('')
const error = ref<string | null>(null)
const busy = ref<'password' | 'email' | null>(null)
// After asking for an email: 'sign-in' for a sign-in code, 'new-device' for the code
// that confirms a password sign-in on this device.
const codeFor = ref<'sign-in' | 'new-device' | null>(null)
// Set when the first step succeeded and the account uses two-step sign-in.
const mfaToken = ref<string | null>(null)

/** Moves to the second step if err asks for it; returns false for other errors. */
function secondStepNeeded(err: unknown) {
  if (err instanceof ApiError && err.code === 'mfa_required' && err.mfaToken) {
    mfaToken.value = err.mfaToken
    return true
  }
  return false
}

async function done() {
  await router.push({ name: 'home' })
}

function describe(err: unknown) {
  return err instanceof ApiError ? err.message : 'Something went wrong. Please try again.'
}

async function withPasskey() {
  passkeyError.value = null
  passkeyBusy.value = true
  try {
    await signIn()
    await router.push({ name: 'home' })
  } catch (err) {
    passkeyError.value = describePasskeyError(err)
  } finally {
    passkeyBusy.value = false
  }
}

async function withPassword() {
  error.value = null
  busy.value = 'password'
  try {
    await signInWithPassword(email.value, password.value)
    await router.push({ name: 'home' })
  } catch (err) {
    if (err instanceof ApiError && err.code === 'email_code_required') {
      codeFor.value = 'new-device'
    } else if (!secondStepNeeded(err)) {
      error.value = describe(err)
    }
  } finally {
    busy.value = null
  }
}

async function sendCode() {
  error.value = null
  busy.value = 'email'
  try {
    await requestSignInLink(email.value)
    codeFor.value = 'sign-in'
  } catch (err) {
    error.value = describe(err)
  } finally {
    busy.value = null
  }
}

async function confirmCode(code: string) {
  try {
    if (codeFor.value === 'new-device') {
      await signInWithPassword(email.value, password.value, code)
    } else {
      await signInWithEmail({ email: email.value, code })
    }
  } catch (err) {
    if (secondStepNeeded(err)) {
      return
    }
    throw err
  }
  await done()
}

function startOver() {
  codeFor.value = null
  mfaToken.value = null
  password.value = ''
}
</script>

<template>
  <v-container
    class="py-8"
    max-width="480"
  >
    <h1 class="text-h4 mb-6">
      Sign in
    </h1>

    <template v-if="mfaToken">
      <SecondStepForm
        :mfa-token="mfaToken"
        @done="done"
        @restart="startOver"
      />
    </template>

    <template v-else-if="codeFor">
      <p
        v-if="codeFor === 'new-device'"
        class="text-body-1 mb-4"
      >
        This device is new to your account, so we've emailed a code to
        <strong>{{ email }}</strong> to confirm it's you. Enter it here. It works once, for 15
        minutes.
      </p>
      <p
        v-else
        class="text-body-1 mb-4"
      >
        If an account uses <strong>{{ email }}</strong>, we've sent it a sign-in link and code.
        Open the link, or enter the code here. Both work once, for 15 minutes.
      </p>
      <EmailCodeForm
        :submit="confirmCode"
        label="Sign in"
      />
      <p class="text-body-2 mt-4">
        No email? Check your spam folder, or
        <a
          href="#"
          @click.prevent="startOver"
        >start again</a>.
      </p>
    </template>

    <template v-else>
      <template v-if="supported">
        <v-alert
          v-if="passkeyError"
          type="error"
          variant="tonal"
          class="mb-4"
        >
          {{ passkeyError }}
        </v-alert>
        <v-btn
          color="primary"
          size="large"
          block
          :loading="passkeyBusy"
          @click="withPasskey"
        >
          Sign in with a passkey
        </v-btn>

        <div class="d-flex align-center ga-3 my-8">
          <v-divider />
          <span class="text-body-2 text-medium-emphasis">or</span>
          <v-divider />
        </div>
      </template>

      <v-form @submit.prevent="password ? withPassword() : sendCode()">
        <v-text-field
          v-model="email"
          label="Email"
          type="email"
          autocomplete="username"
          :disabled="busy !== null"
          required
        />
        <PasswordField
          v-model="password"
          autocomplete="current-password"
          label="Password (if you've set one)"
          :disabled="busy !== null"
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
          :color="supported ? undefined : 'primary'"
          :variant="supported ? 'outlined' : 'elevated'"
          size="large"
          block
          class="mb-3"
          :loading="busy === 'password'"
          :disabled="busy !== null || !password"
          @click.prevent="withPassword"
        >
          Sign in with password
        </v-btn>
        <v-btn
          variant="text"
          size="large"
          block
          :loading="busy === 'email'"
          :disabled="busy !== null"
          @click="sendCode"
        >
          Email me a sign-in code instead
        </v-btn>
      </v-form>
    </template>

    <p class="text-body-2 mt-6">
      New here?
      <router-link :to="{ name: 'signup' }">
        Create an account
      </router-link>
    </p>
  </v-container>
</template>
