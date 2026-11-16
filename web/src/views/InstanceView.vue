<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { mdiAlertOutline, mdiCheckDecagram } from '@mdi/js'
import { computed, ref } from 'vue'
import type { InstanceInfo } from '../api/client'
import { allowLocalInstances, connectTo, defaultInstance, isKnownInstance } from '../appInstance'
import { checkInstance, InstanceAddressError, parseInstanceAddress, type InstanceAddress } from '../instanceAddress'

// The instance picker (ADR 0023): choose an instance, check it, then confirm it by its
// domain. The name an instance gives itself proves nothing; its domain does.

const choice = ref<'default' | 'other'>(defaultInstance ? 'default' : 'other')
const typed = ref('')
const error = ref<string | null>(null)
const busy = ref(false)
// Set once an instance passes the checks, for the confirmation step.
const checked = ref<{ address: InstanceAddress; info: InstanceInfo } | null>(null)
const known = computed(() => (checked.value ? isKnownInstance(checked.value.address) : false))

async function check() {
  error.value = null
  busy.value = true
  try {
    const address =
      choice.value === 'default' && defaultInstance
        ? defaultInstance
        : parseInstanceAddress(typed.value, allowLocalInstances)
    const info = await checkInstance(address, { allowLocal: allowLocalInstances })
    checked.value = { address, info }
  } catch (err) {
    error.value = err instanceof InstanceAddressError ? err.message : 'Something went wrong. Please try again.'
  } finally {
    busy.value = false
  }
}

async function connect() {
  if (!checked.value) {
    return
  }
  busy.value = true
  await connectTo(checked.value.address)
  // Start the app afresh, so everything loads from the new instance.
  window.location.replace('/')
}
</script>

<template>
  <v-container
    class="py-8"
    max-width="480"
  >
    <template v-if="checked">
      <h1 class="text-h4 mb-6">
        Connect to this instance?
      </h1>

      <v-card
        variant="outlined"
        class="mb-6"
      >
        <v-card-text>
          <div class="text-overline">
            Domain
          </div>
          <div class="text-h5 text-break mb-4">
            {{ checked.address.host }}
          </div>
          <div class="text-overline">
            Name it gives itself
          </div>
          <div class="text-body-1">
            {{ checked.info.name }}
          </div>
        </v-card-text>
      </v-card>

      <v-alert
        v-if="known"
        type="success"
        variant="tonal"
        :icon="mdiCheckDecagram"
        class="mb-6"
      >
        This instance is on the app's list.
      </v-alert>
      <v-alert
        v-else
        type="warning"
        variant="tonal"
        :icon="mdiAlertOutline"
        class="mb-6"
      >
        This instance isn't on the app's list. Only continue if you trust
        <strong>{{ checked.address.host }}</strong>. Anyone can run an instance, and you'll sign in
        with your email and password there.
      </v-alert>

      <v-btn
        color="primary"
        size="large"
        block
        :loading="busy"
        class="mb-3"
        @click="connect"
      >
        Connect to {{ checked.address.host }}
      </v-btn>
      <v-btn
        variant="text"
        block
        :disabled="busy"
        @click="checked = null"
      >
        Choose another instance
      </v-btn>
    </template>

    <template v-else>
      <h1 class="text-h4 mb-2">
        Choose an instance
      </h1>
      <p class="text-body-1 mb-6">
        PetCommunity is made of independent instances, each run by its own community. Your
        account lives on one of them.
      </p>

      <v-alert
        v-if="error"
        type="error"
        variant="tonal"
        class="mb-4"
      >
        {{ error }}
      </v-alert>

      <v-form @submit.prevent="check">
        <v-radio-group
          v-if="defaultInstance"
          v-model="choice"
          :disabled="busy"
        >
          <v-radio
            value="default"
            :label="defaultInstance.host"
          />
          <v-radio
            value="other"
            label="Another instance"
          />
        </v-radio-group>

        <v-text-field
          v-if="choice === 'other'"
          v-model="typed"
          label="Instance address"
          placeholder="pets.example.org"
          type="url"
          inputmode="url"
          autocapitalize="off"
          autocomplete="url"
          spellcheck="false"
          :disabled="busy"
          autofocus
        />

        <v-btn
          type="submit"
          color="primary"
          size="large"
          block
          :loading="busy"
        >
          Continue
        </v-btn>
      </v-form>
    </template>
  </v-container>
</template>
