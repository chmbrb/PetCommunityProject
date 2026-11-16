<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { mdiServerNetwork } from '@mdi/js'
import { ref } from 'vue'
import { connectedInstance, disconnect, isApp } from '../appInstance'
import { useSession } from '../useSession'

// Shows the apps which instance they're connected to, and lets people choose another
// (ADR 0023). Renders nothing on the web, which is its own instance.

const instance = connectedInstance()
const { me } = useSession()
const confirmOpen = ref(false)
const busy = ref(false)

function change() {
  if (me.value) {
    confirmOpen.value = true
  } else {
    void switchInstance()
  }
}

async function switchInstance() {
  busy.value = true
  await disconnect()
  // Start the app afresh at the picker.
  window.location.replace('/')
}
</script>

<template>
  <div
    v-if="isApp && instance"
    class="d-flex align-center flex-wrap ga-2 text-body-2"
  >
    <v-icon
      :icon="mdiServerNetwork"
      size="small"
    />
    <span>On <strong class="text-break">{{ instance.host }}</strong></span>
    <v-btn
      variant="text"
      size="small"
      :loading="busy"
      @click="change"
    >
      Change instance
    </v-btn>

    <v-dialog
      v-model="confirmOpen"
      max-width="440"
    >
      <v-card title="Change instance?">
        <v-card-text>
          You'll be signed out of <strong>{{ instance.host }}</strong> on this device. Your
          account stays there, and you can connect to it again later.
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="confirmOpen = false">
            Cancel
          </v-btn>
          <v-btn
            color="primary"
            :loading="busy"
            @click="switchInstance"
          >
            Sign out and change
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>
