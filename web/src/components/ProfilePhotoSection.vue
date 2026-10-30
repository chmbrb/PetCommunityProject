<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { mdiAccount } from '@mdi/js'
import {
  ApiError,
  fetchMedia,
  maxUploadBytes,
  pickVariant,
  removeAvatar,
  setAvatar,
  uploadImage,
  type MediaAsset,
} from '../api/client'
import { useSession } from '../useSession'

const { me, refresh } = useSession()

const fileInput = ref<HTMLInputElement | null>(null)
const state = ref<'idle' | 'uploading' | 'processing' | 'saving'>('idle')
const error = ref<string | null>(null)
const notice = ref<string | null>(null)

const avatar = computed(() => (me.value ? pickVariant(me.value.actor.avatar, 160) : undefined))
const verified = computed(() => me.value?.account.email_verified ?? false)
const busy = computed(() => state.value !== 'idle')

let pollTimer: ReturnType<typeof setTimeout> | undefined
onBeforeUnmount(() => clearTimeout(pollTimer))

function choose() {
  error.value = null
  notice.value = null
  fileInput.value?.click()
}

async function onFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = '' // allow choosing the same file again
  if (!file) {
    return
  }
  if (file.size > maxUploadBytes) {
    error.value = 'That photo is larger than 15 MB. Please choose a smaller one.'
    return
  }
  try {
    state.value = 'uploading'
    const asset = await uploadImage(file, 'avatar')
    state.value = 'processing'
    const done = await waitForProcessing(asset.id)
    if (done.status !== 'ready') {
      error.value = done.rejection_reason
        ? `We couldn't use that photo: ${done.rejection_reason}.`
        : "We couldn't use that photo."
      return
    }
    state.value = 'saving'
    await setAvatar(done.id)
    await refresh()
    notice.value = 'Profile photo updated.'
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : 'Something went wrong. Please try again.'
  } finally {
    state.value = 'idle'
  }
}

/** Polls until the worker has processed the upload, for up to about 90 seconds. */
async function waitForProcessing(id: string): Promise<MediaAsset> {
  for (let attempt = 0; attempt < 90; attempt++) {
    const asset = await fetchMedia(id)
    if (asset.status !== 'pending') {
      return asset
    }
    await new Promise((resolve) => (pollTimer = setTimeout(resolve, 1000)))
  }
  throw new Error('Processing is taking longer than expected. Please try again later.')
}

async function remove() {
  error.value = null
  notice.value = null
  state.value = 'saving'
  try {
    await removeAvatar()
    await refresh()
    notice.value = 'Profile photo removed.'
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : 'Something went wrong. Please try again.'
  } finally {
    state.value = 'idle'
  }
}

const statusText = computed(() => {
  switch (state.value) {
    case 'uploading':
      return 'Uploading…'
    case 'processing':
      return 'Processing your photo…'
    case 'saving':
      return 'Saving…'
  }
  return ''
})
</script>

<template>
  <section class="mb-10">
    <h2 class="text-h6 mb-1">
      Profile photo
    </h2>
    <p class="text-body-2 text-medium-emphasis mb-4">
      Shown with your name. We remove location and camera details from every photo.
    </p>

    <div class="d-flex align-center ga-4 mb-3">
      <v-avatar
        size="80"
        color="surface-variant"
      >
        <v-img
          v-if="avatar"
          :src="avatar.url"
          :alt="`Profile photo of ${me?.actor.display_name}`"
          cover
        />
        <v-icon
          v-else
          :icon="mdiAccount"
          size="40"
        />
      </v-avatar>
      <div class="d-flex flex-wrap ga-2">
        <v-btn
          color="primary"
          :loading="busy"
          :disabled="!verified || busy"
          @click="choose"
        >
          {{ avatar ? 'Change photo' : 'Upload photo' }}
        </v-btn>
        <v-btn
          v-if="avatar"
          variant="text"
          :disabled="busy"
          @click="remove"
        >
          Remove
        </v-btn>
      </div>
      <input
        ref="fileInput"
        type="file"
        accept="image/jpeg,image/png,image/webp,image/heic,image/heif,image/avif"
        hidden
        @change="onFile"
      >
    </div>

    <p
      v-if="!verified"
      class="text-body-2 text-medium-emphasis"
    >
      Confirm your email address to upload photos.
    </p>
    <p
      v-if="statusText"
      class="text-body-2"
      role="status"
    >
      {{ statusText }}
    </p>
    <v-alert
      v-if="notice"
      type="success"
      variant="tonal"
      density="compact"
    >
      {{ notice }}
    </v-alert>
    <v-alert
      v-if="error"
      type="error"
      variant="tonal"
      density="compact"
    >
      {{ error }}
    </v-alert>
  </section>
</template>
