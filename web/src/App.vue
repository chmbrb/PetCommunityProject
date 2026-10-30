<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { mdiPaw } from '@mdi/js'
import { useInstance } from './useInstance'

const { instance } = useInstance()
</script>

<template>
  <v-app>
    <v-app-bar
      color="primary"
      density="comfortable"
    >
      <template #prepend>
        <v-icon
          :icon="mdiPaw"
          class="ms-2"
        />
      </template>
      <v-app-bar-title>{{ instance?.name ?? 'PetCommunityProject' }}</v-app-bar-title>
    </v-app-bar>

    <v-main>
      <router-view />
    </v-main>

    <v-footer class="text-body-2 justify-center ga-4">
      <!-- AGPL section 13: offer the source of the running version (ADR 0002). -->
      <a
        v-if="instance"
        :href="instance.source_url"
        rel="noopener"
        target="_blank"
      >Source code</a>
      <span v-if="instance">Version {{ instance.version }}</span>
    </v-footer>
  </v-app>
</template>
