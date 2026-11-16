// SPDX-License-Identifier: AGPL-3.0-or-later
import { Capacitor } from '@capacitor/core'
import { createApp } from 'vue'
import App from './App.vue'
import { memorySessionStore, setSessionStore } from './api/client'
import { loadConnectedInstance } from './appInstance'
import router from './router'
import vuetify from './plugins/vuetify'

async function start() {
  // The apps sign in with a stored bearer token, opened before the first request; the
  // web app uses its cookie. Loaded only in the apps, so the web build never runs it.
  if (Capacitor.isNativePlatform()) {
    try {
      const { openNativeSessionStore } = await import('./nativeSession')
      setSessionStore(await openNativeSessionStore())
    } catch (err) {
      // Still let people sign in; they'll need to again after closing the app.
      console.error('secure storage unavailable; keeping the session in memory', err)
      setSessionStore(memorySessionStore())
    }
    // Before the first request: the API client needs to know which instance to call.
    await loadConnectedInstance()
  }
  createApp(App).use(router).use(vuetify).mount('#app')
}

void start()
