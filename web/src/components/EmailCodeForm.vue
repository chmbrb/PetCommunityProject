<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { ref } from 'vue'
import { ApiError } from '../api/client'

const props = defineProps<{
  /** Called with the code; rejecting shows the error. */
  submit: (code: string) => Promise<void>
  label?: string
}>()

const code = ref('')
const error = ref<string | null>(null)
const busy = ref(false)

async function onSubmit() {
  error.value = null
  busy.value = true
  try {
    await props.submit(code.value)
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : 'Something went wrong. Please try again.'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <v-form @submit.prevent="onSubmit">
    <v-text-field
      v-model="code"
      label="6-digit code"
      inputmode="numeric"
      autocomplete="one-time-code"
      maxlength="7"
      spellcheck="false"
      :error-messages="error ?? undefined"
      :disabled="busy"
      autofocus
      required
    />
    <v-btn
      type="submit"
      color="primary"
      size="large"
      block
      :loading="busy"
    >
      {{ label ?? 'Continue' }}
    </v-btn>
  </v-form>
</template>
