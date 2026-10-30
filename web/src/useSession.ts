// SPDX-License-Identifier: AGPL-3.0-or-later
import { ref } from 'vue'
import { startAuthentication, startRegistration } from '@simplewebauthn/browser'
import {
  beginLogin,
  beginSignup,
  fetchMe,
  finishLogin,
  finishSignInLink,
  finishSignup,
  logout,
  type Me,
  type SignupDetails,
} from './api/client'

const me = ref<Me | null>(null)
const loaded = ref(false)
let loading: Promise<void> | null = null

/** The signed-in account, shared across components. */
export function useSession() {
  if (!loading) {
    loading = fetchMe()
      .then((result) => {
        me.value = result
      })
      .catch(() => {
        me.value = null
      })
      .finally(() => {
        loaded.value = true
      })
  }

  /** Creates an account with a new passkey. The browser prompts for the passkey. */
  async function signUp(details: SignupDetails) {
    const { challenge_id, options } = await beginSignup(details)
    const credential = await startRegistration({ optionsJSON: options })
    me.value = await finishSignup(challenge_id, credential)
  }

  /** Signs in with any passkey saved for this site. */
  async function signIn() {
    const { challenge_id, options } = await beginLogin()
    const credential = await startAuthentication({ optionsJSON: options })
    me.value = await finishLogin(challenge_id, credential)
  }

  /** Signs in with the token from a sign-in email. */
  async function signInWithLink(token: string) {
    me.value = await finishSignInLink(token)
  }

  /** Reloads the account, e.g. after verifying the email address. */
  async function refresh() {
    me.value = await fetchMe()
  }

  async function signOut() {
    await logout()
    me.value = null
  }

  return { me, loaded, signUp, signIn, signInWithLink, refresh, signOut }
}
