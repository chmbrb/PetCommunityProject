<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { ref } from 'vue'
import { mdiEye, mdiEyeOff } from '@mdi/js'

// A password input with a show/hide toggle, so people can check what they typed
// instead of typing it twice.
defineProps<{
  label?: string
  /** "current-password" to sign in, "new-password" to set one. */
  autocomplete: 'current-password' | 'new-password'
  hint?: string
  errorMessages?: string
  disabled?: boolean
}>()
const model = defineModel<string>({ required: true })
const visible = ref(false)
</script>

<template>
  <v-text-field
    v-model="model"
    :label="label ?? 'Password'"
    :type="visible ? 'text' : 'password'"
    :autocomplete="autocomplete"
    :hint="hint"
    :persistent-hint="!!hint"
    :error-messages="errorMessages"
    :disabled="disabled"
    :append-inner-icon="visible ? mdiEyeOff : mdiEye"
    :aria-label="label ?? 'Password'"
    autocapitalize="none"
    spellcheck="false"
    @click:append-inner="visible = !visible"
  />
</template>
