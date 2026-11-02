<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ApiError } from '../api/client'
import { takeLinkToken } from '../linkToken'
import { useSession } from '../useSession'

const props = defineProps<{ purpose: 'signin' | 'signup' }>()

const router = useRouter()
const { signInWithEmail, signUpWithEmail } = useSession()

const text = computed(() =>
  props.purpose === 'signup'
    ? {
        title: 'Create your account',
        intro: 'Finish creating your account with the link from your email.',
        button: 'Create my account',
        retry: 'Start again',
        retryRoute: 'signup',
      }
    : {
        title: 'Sign in',
        intro: 'Finish signing in with the link from your email.',
        button: 'Finish signing in',
        retry: 'Request a new sign-in email',
        retryRoute: 'signin',
      },
)

// Finishing takes a click, not just opening the link: email security scanners often
// open links automatically, and would otherwise use up the one-time token.
const token = takeLinkToken()
const error = ref<string | null>(token ? null : 'This link is incomplete. Please request a new email.')
const busy = ref(false)

async function finish() {
  if (!token) {
    return
  }
  error.value = null
  busy.value = true
  try {
    await (props.purpose === 'signup' ? signUpWithEmail({ token }) : signInWithEmail({ token }))
    await router.replace({ name: 'home' })
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : 'Something went wrong. Please try again.'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <v-container
    class="py-8"
    max-width="480"
  >
    <h1 class="text-h4 mb-2">
      {{ text.title }}
    </h1>

    <v-alert
      v-if="error"
      type="error"
      variant="tonal"
      class="mb-4"
    >
      {{ error }}
    </v-alert>

    <template v-if="token">
      <p class="text-body-1 mb-6">
        {{ text.intro }}
      </p>
      <v-btn
        color="primary"
        size="large"
        block
        :loading="busy"
        @click="finish"
      >
        {{ text.button }}
      </v-btn>
    </template>

    <p class="text-body-2 mt-6">
      <router-link :to="{ name: text.retryRoute }">
        {{ text.retry }}
      </router-link>
    </p>
  </v-container>
</template>
