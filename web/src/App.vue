<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { mdiAccountCircle, mdiPaw } from '@mdi/js'
import { useRouter } from 'vue-router'
import { useInstance } from './useInstance'
import { useSession } from './useSession'

const router = useRouter()
const { instance } = useInstance()
const { me, loaded, signOut } = useSession()

async function onSignOut() {
  await signOut()
  await router.push({ name: 'home' })
}
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
      <v-app-bar-title>
        <router-link
          to="/"
          class="app-title"
        >
          {{ instance?.name ?? 'PetCommunityProject' }}
        </router-link>
      </v-app-bar-title>

      <template
        v-if="loaded"
        #append
      >
        <v-menu v-if="me">
          <template #activator="{ props }">
            <v-btn
              v-bind="props"
              :prepend-icon="mdiAccountCircle"
              variant="text"
            >
              {{ me.actor.display_name }}
            </v-btn>
          </template>
          <v-list density="compact">
            <v-list-item
              :subtitle="'@' + me.actor.handle"
              :title="me.actor.display_name"
            />
            <v-divider />
            <v-list-item
              title="Sign out"
              @click="onSignOut"
            />
          </v-list>
        </v-menu>
        <v-btn
          v-else
          :to="{ name: 'signin' }"
          variant="text"
        >
          Sign in
        </v-btn>
      </template>
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

<style scoped>
.app-title {
  color: inherit;
  text-decoration: none;
}
</style>
