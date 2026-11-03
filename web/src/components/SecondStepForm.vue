<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { ref } from 'vue'
import { browserSupportsWebAuthn } from '@simplewebauthn/browser'
import { ApiError } from '../api/client'
import { describePasskeyError } from '../passkeyErrors'
import { useSession } from '../useSession'

// The second step of a sign-in for accounts with two-step sign-in: a code from the
// authenticator app, a recovery code, or a passkey instead.
const props = defineProps<{ mfaToken: string }>()
const emit = defineEmits<{
  /** Signed in. */
  done: []
  /** The pending sign-in expired or had too many wrong codes: sign in again. */
  restart: []
}>()

const { signIn, signInSecondStep } = useSession()
const passkeys = browserSupportsWebAuthn()
const useRecovery = ref(false)
const value = ref('')
const error = ref<string | null>(null)
const expired = ref(false)
const busy = ref(false)

async function submit() {
  error.value = null
  busy.value = true
  try {
    await signInSecondStep(props.mfaToken, useRecovery.value ? { recovery_code: value.value } : { code: value.value })
    emit('done')
  } catch (err) {
    expired.value = err instanceof ApiError && err.code === 'mfa_expired'
    error.value = err instanceof ApiError ? err.message : 'Something went wrong. Please try again.'
    value.value = ''
  } finally {
    busy.value = false
  }
}

async function withPasskey() {
  error.value = null
  busy.value = true
  try {
    await signIn()
    emit('done')
  } catch (err) {
    error.value = describePasskeyError(err)
  } finally {
    busy.value = false
  }
}

function toggle() {
  useRecovery.value = !useRecovery.value
  value.value = ''
  error.value = null
}
</script>

<template>
  <div>
    <p class="text-body-1 mb-4">
      <template v-if="useRecovery">
        Enter one of the recovery codes you saved when you set up two-step sign-in. Each one
        works once.
      </template>
      <template v-else>
        Enter the 6-digit code from your authenticator app or password manager.
      </template>
    </p>
    <v-form @submit.prevent="submit">
      <v-text-field
        v-model="value"
        :label="useRecovery ? 'Recovery code' : '6-digit code'"
        :inputmode="useRecovery ? 'text' : 'numeric'"
        :autocomplete="useRecovery ? 'off' : 'one-time-code'"
        :maxlength="useRecovery ? 20 : 7"
        autocapitalize="none"
        spellcheck="false"
        :error-messages="error ?? undefined"
        :disabled="busy || expired"
        autofocus
        required
      />
      <v-btn
        v-if="!expired"
        type="submit"
        color="primary"
        size="large"
        block
        :loading="busy"
      >
        Continue
      </v-btn>
      <v-btn
        v-else
        color="primary"
        size="large"
        block
        @click="emit('restart')"
      >
        Sign in again
      </v-btn>
    </v-form>
    <div
      v-if="!expired"
      class="d-flex flex-column align-start mt-4"
    >
      <v-btn
        variant="text"
        size="small"
        :disabled="busy"
        @click="toggle"
      >
        {{ useRecovery ? 'Use the authenticator app instead' : 'Use a recovery code instead' }}
      </v-btn>
      <v-btn
        v-if="passkeys"
        variant="text"
        size="small"
        :disabled="busy"
        @click="withPasskey"
      >
        Use a passkey instead
      </v-btn>
    </div>
  </div>
</template>
