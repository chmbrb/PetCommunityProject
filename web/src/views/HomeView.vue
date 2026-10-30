<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed } from 'vue'
import { useInstance } from '../useInstance'
import { useSession } from '../useSession'

const { instance, error } = useInstance()
const { me, loaded } = useSession()
const heading = computed(() => (me.value ? `Welcome, ${me.value.actor.display_name}` : 'Welcome'))
</script>

<template>
  <v-container
    class="py-8"
    max-width="720"
  >
    <h1 class="text-h4 mb-4">
      {{ heading }}
    </h1>
    <p class="text-body-1 mb-6">
      A community-run place to adopt, foster, and care for pets. Adoption listings from local
      shelters are coming soon.
    </p>

    <div
      v-if="loaded && !me"
      class="d-flex ga-3 mb-6"
    >
      <v-btn
        :to="{ name: 'signup' }"
        color="primary"
      >
        Create an account
      </v-btn>
      <v-btn
        :to="{ name: 'signin' }"
        variant="outlined"
      >
        Sign in
      </v-btn>
    </div>

    <v-alert
      v-if="error"
      type="warning"
      variant="tonal"
      title="Can't reach this instance"
    >
      {{ error }}
    </v-alert>
    <v-alert
      v-else-if="instance"
      type="success"
      variant="tonal"
      title="Connected"
    >
      {{ instance.name }}<template v-if="instance.domain">
        ({{ instance.domain }})
      </template>
      · API {{ instance.api_version }}
    </v-alert>
    <v-progress-linear
      v-else
      indeterminate
      color="primary"
    />
  </v-container>
</template>
