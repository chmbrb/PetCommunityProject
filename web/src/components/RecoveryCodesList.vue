<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, ref } from 'vue'
import { mdiContentCopy, mdiDownload } from '@mdi/js'

// Shows freshly created recovery codes. They're never shown again, so the person can
// copy or download them.
const props = defineProps<{ codes: string[]; instanceName: string }>()

const copied = ref(false)
const text = computed(
  () =>
    `${props.instanceName} recovery codes\n` +
    `Each code works once, if you can't use your authenticator app.\n\n` +
    props.codes.join('\n') +
    '\n',
)
const downloadHref = computed(() => 'data:text/plain;charset=utf-8,' + encodeURIComponent(text.value))

async function copy() {
  await navigator.clipboard.writeText(text.value)
  copied.value = true
}
</script>

<template>
  <div>
    <v-sheet
      border
      rounded
      class="pa-4 mb-3"
    >
      <ul
        class="recovery-codes"
        aria-label="Recovery codes"
      >
        <li
          v-for="c in codes"
          :key="c"
        >
          {{ c }}
        </li>
      </ul>
    </v-sheet>
    <div class="d-flex flex-wrap ga-2">
      <v-btn
        variant="outlined"
        :prepend-icon="mdiContentCopy"
        @click="copy"
      >
        {{ copied ? 'Copied' : 'Copy' }}
      </v-btn>
      <v-btn
        variant="outlined"
        :prepend-icon="mdiDownload"
        :href="downloadHref"
        download="recovery-codes.txt"
      >
        Download
      </v-btn>
    </div>
  </div>
</template>

<style scoped>
.recovery-codes {
  list-style: none;
  padding: 0;
  margin: 0;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(10rem, 1fr));
  gap: 0.5rem 1.5rem;
  font-family: ui-monospace, Menlo, Consolas, monospace;
  font-size: 1rem;
}
</style>
