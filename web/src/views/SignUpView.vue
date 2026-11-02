<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { browserSupportsWebAuthn } from '@simplewebauthn/browser'
import { ApiError, requestEmailSignup } from '../api/client'
import EmailCodeForm from '../components/EmailCodeForm.vue'
import PasswordField from '../components/PasswordField.vue'
import { describePasskeyError } from '../passkeyErrors'
import { useSession } from '../useSession'

const router = useRouter()
const { signUp, signUpWithEmail } = useSession()

const supported = browserSupportsWebAuthn()
const form = reactive({ email: '', handle: '', display_name: '' })
const fieldErrors = reactive<Record<string, string>>({})
const error = ref<string | null>(null)
const busy = ref<'passkey' | 'email' | null>(null)
// Set once the sign-up email is on its way: the address it went to.
const emailSentTo = ref<string | null>(null)
// Optional, set when confirming the email.
const password = ref('')
const passwordError = ref<string | null>(null)

/** Runs one way of signing up, showing field errors next to their fields. */
async function attempt(kind: 'passkey' | 'email', action: () => Promise<void>) {
  error.value = null
  for (const key of Object.keys(fieldErrors)) {
    delete fieldErrors[key]
  }
  busy.value = kind
  try {
    await action()
  } catch (err) {
    if (err instanceof ApiError && err.field) {
      fieldErrors[err.field] = err.message
    } else {
      error.value = describePasskeyError(err)
    }
  } finally {
    busy.value = null
  }
}

function withPasskey() {
  return attempt('passkey', async () => {
    await signUp({ ...form })
    await router.push({ name: 'home' })
  })
}

function withEmail() {
  return attempt('email', async () => {
    await requestEmailSignup({ ...form })
    emailSentTo.value = form.email.trim()
  })
}

async function confirmCode(code: string) {
  passwordError.value = null
  try {
    await signUpWithEmail({ email: emailSentTo.value ?? '', code }, password.value || undefined)
  } catch (err) {
    // A refused password belongs next to the password field; the code is still good.
    if (err instanceof ApiError && err.field === 'password') {
      passwordError.value = err.message
      return
    }
    throw err
  }
  await router.push({ name: 'home' })
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

    <template v-if="emailSentTo">
      <p class="text-body-1 mb-6">
        We've sent an email to <strong>{{ emailSentTo }}</strong>. Open the link in it, or
        enter the code from it here. Both work for 1 hour.
      </p>
      <PasswordField
        v-model="password"
        autocomplete="new-password"
        label="Choose a password (optional)"
        hint="At least 12 characters. Lets you sign in without waiting for an email. You can also add one later."
        :error-messages="passwordError ?? undefined"
        class="mb-4"
      />
      <EmailCodeForm
        :submit="confirmCode"
        label="Create my account"
      />
      <p class="text-body-2 mt-6">
        No email? Check your spam folder, or
        <a
          href="#"
          @click.prevent="emailSentTo = null"
        >go back and check the address</a>.
      </p>
    </template>

    <template v-else>
      <p class="text-body-1 mb-6">
        We recommend a passkey: you sign in with your fingerprint, face, or device PIN, and
        there's no password to remember or steal. You can also sign in with codes we email
        you, and add a passkey later.
      </p>

      <v-form @submit.prevent="supported ? withPasskey() : withEmail()">
        <v-text-field
          v-model="form.display_name"
          label="Your name"
          autocomplete="name"
          :error-messages="fieldErrors.display_name"
          :disabled="busy !== null"
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
          :disabled="busy !== null"
          class="mb-2"
          required
        />
        <v-text-field
          v-model="form.email"
          label="Email"
          type="email"
          hint="Kept private. Used to sign in and recover your account."
          persistent-hint
          autocomplete="email"
          :error-messages="fieldErrors.email"
          :disabled="busy !== null"
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
          v-if="supported"
          type="submit"
          color="primary"
          size="large"
          block
          class="mb-3"
          :loading="busy === 'passkey'"
          :disabled="busy !== null"
        >
          Create account with a passkey
        </v-btn>
        <v-btn
          :type="supported ? 'button' : 'submit'"
          :color="supported ? undefined : 'primary'"
          :variant="supported ? 'outlined' : 'elevated'"
          size="large"
          block
          :loading="busy === 'email'"
          :disabled="busy !== null"
          @click="supported && withEmail()"
        >
          Continue with email
        </v-btn>
      </v-form>
    </template>

    <p class="text-body-2 mt-6">
      Already have an account?
      <router-link :to="{ name: 'signin' }">
        Sign in
      </router-link>
    </p>
  </v-container>
</template>
