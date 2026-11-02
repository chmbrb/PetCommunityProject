<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { startRegistration } from '@simplewebauthn/browser'
import { mdiCellphone, mdiDotsVertical, mdiKeyVariant, mdiMonitor } from '@mdi/js'
import {
  ApiError,
  beginAddPasskey,
  finishAddPasskey,
  listPasskeys,
  listSessions,
  removePasskey,
  removePassword,
  renamePasskey,
  requestSignInLink,
  revokeOtherSessions,
  revokeSession,
  setPassword,
  type Passkey,
  type SessionInfo,
} from '../api/client'
import EmailCodeForm from '../components/EmailCodeForm.vue'
import PasswordField from '../components/PasswordField.vue'
import ProfilePhotoSection from '../components/ProfilePhotoSection.vue'
import { deviceLabel } from '../deviceLabel'
import { describePasskeyError } from '../passkeyErrors'
import { useSession } from '../useSession'

const { me, loaded, signIn, signInWithEmail, refresh } = useSession()

const passkeys = ref<Passkey[]>([])
const sessions = ref<SessionInfo[]>([])
const error = ref<string | null>(null)
const notice = ref<string | null>(null)
const busy = ref(false)

const otherSessions = computed(() => sessions.value.filter((s) => !s.current))
// The only passkey can be removed once the email is confirmed: email sign-in is then
// the way back in.
const canRemoveLast = computed(() => me.value?.account.email_verified ?? false)

async function load() {
  ;[passkeys.value, sessions.value] = await Promise.all([listPasskeys(), listSessions()])
}

watch(
  () => me.value?.account.id,
  (id) => {
    if (id) {
      load().catch((err) => (error.value = describePasskeyError(err)))
    }
  },
  { immediate: true },
)

// Changing passkeys needs a recent sign-in. When the server asks for one, the person
// confirms with a passkey or an emailed code, and the action is retried.
const confirmOpen = ref(false)
const confirmError = ref<string | null>(null)
const confirmCodeSent = ref(false)
let pendingAction: (() => Promise<void>) | null = null

async function run(action: () => Promise<void>, done?: string) {
  error.value = null
  notice.value = null
  busy.value = true
  try {
    await action()
    if (done) {
      notice.value = done
    }
  } catch (err) {
    if (err instanceof ApiError && err.code === 'reauthentication_required') {
      pendingAction = async () => {
        await action()
        if (done) {
          notice.value = done
        }
      }
      confirmError.value = null
      confirmCodeSent.value = false
      confirmOpen.value = true
    } else {
      error.value = describePasskeyError(err)
    }
  } finally {
    busy.value = false
  }
}

/** Signs in again with signInAgain, then retries the action that needed it. */
async function reconfirm(signInAgain: () => Promise<void>) {
  const accountBefore = me.value?.account.id
  await signInAgain()
  confirmOpen.value = false
  if (me.value?.account.id !== accountBefore) {
    pendingAction = null
    error.value = 'You confirmed as a different account, so nothing was changed.'
    return
  }
  const action = pendingAction
  pendingAction = null
  if (action) {
    await action()
  }
  await load()
}

async function confirmWithPasskey() {
  confirmError.value = null
  busy.value = true
  try {
    await reconfirm(signIn)
  } catch (err) {
    confirmError.value = describePasskeyError(err)
  } finally {
    busy.value = false
  }
}

async function sendConfirmCode() {
  if (!me.value) {
    return
  }
  confirmError.value = null
  busy.value = true
  try {
    await requestSignInLink(me.value.account.email)
    confirmCodeSent.value = true
  } catch (err) {
    confirmError.value = describePasskeyError(err)
  } finally {
    busy.value = false
  }
}

function confirmWithCode(code: string) {
  const email = me.value?.account.email ?? ''
  return reconfirm(() => signInWithEmail({ email, code }))
}

// Adding a passkey
const addOpen = ref(false)
const addName = ref('')

function openAdd() {
  addName.value = deviceLabel(navigator.userAgent)
  addOpen.value = true
}

function addPasskey() {
  const name = addName.value
  return run(async () => {
    const { challenge_id, options } = await beginAddPasskey()
    const credential = await startRegistration({ optionsJSON: options })
    await finishAddPasskey(challenge_id, credential, name)
    addOpen.value = false
    await load()
  }, 'Passkey added. We emailed you a notice about it.')
}

// Renaming a passkey
const renameTarget = ref<Passkey | null>(null)
const renameName = ref('')

function openRename(p: Passkey) {
  renameTarget.value = p
  renameName.value = p.name
}

function saveRename() {
  const target = renameTarget.value
  if (!target) {
    return
  }
  return run(async () => {
    await renamePasskey(target.id, renameName.value)
    renameTarget.value = null
    await load()
  })
}

// Removing a passkey
const removeTarget = ref<Passkey | null>(null)

function confirmRemove() {
  const target = removeTarget.value
  if (!target) {
    return
  }
  return run(async () => {
    await removePasskey(target.id)
    removeTarget.value = null
    await load()
  }, 'Passkey removed.')
}

// Password
const passwordOpen = ref(false)
const newPassword = ref('')
const removePasswordOpen = ref(false)

function openPassword() {
  newPassword.value = ''
  error.value = null
  passwordOpen.value = true
}

function savePassword() {
  const changing = me.value?.account.has_password ?? false
  const password = newPassword.value
  return run(async () => {
    await setPassword(password)
    passwordOpen.value = false
    newPassword.value = ''
    await Promise.all([refresh(), load()])
  }, changing
    ? 'Password changed. Your other devices were signed out, and we emailed you a notice.'
    : 'Password added. Your other devices were signed out, and we emailed you a notice.')
}

function confirmRemovePassword() {
  return run(async () => {
    await removePassword()
    removePasswordOpen.value = false
    await refresh()
  }, 'Password removed. We emailed you a notice.')
}

// Signed-in devices
function signOutDevice(s: SessionInfo) {
  return run(async () => {
    await revokeSession(s.id)
    await load()
  }, 'Signed out that device.')
}

function signOutOthers() {
  return run(async () => {
    const { revoked } = await revokeOtherSessions()
    await load()
    notice.value = revoked === 1 ? 'Signed out 1 other device.' : `Signed out ${revoked} other devices.`
  })
}

function sessionLabel(s: SessionInfo) {
  return s.client === 'mobile' ? 'PetCommunity app' : deviceLabel(s.user_agent)
}

function formatDate(iso: string) {
  return new Date(iso).toLocaleDateString(undefined, { dateStyle: 'medium' })
}

function formatDateTime(iso: string) {
  return new Date(iso).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
}
</script>

<template>
  <v-container
    class="py-8"
    max-width="720"
  >
    <h1 class="text-h4 mb-6">
      Settings
    </h1>

    <v-alert
      v-if="loaded && !me"
      type="info"
      variant="tonal"
    >
      <router-link :to="{ name: 'signin' }">
        Sign in
      </router-link>
      to see your settings.
    </v-alert>

    <template v-else-if="me">
      <v-alert
        v-if="notice"
        type="success"
        variant="tonal"
        class="mb-4"
        closable
        @click:close="notice = null"
      >
        {{ notice }}
      </v-alert>
      <v-alert
        v-if="error"
        type="error"
        variant="tonal"
        class="mb-4"
        closable
        @click:close="error = null"
      >
        {{ error }}
      </v-alert>

      <ProfilePhotoSection />

      <section class="mb-10">
        <h2 class="text-h6 mb-1">
          Password
        </h2>
        <p class="text-body-2 text-medium-emphasis mb-4">
          Optional. With a password you can sign in without waiting for an email; on a new
          device we'll still email you a code to confirm it's you.
        </p>
        <p class="text-body-2 mb-3">
          <template v-if="me.account.has_password">
            You have a password<template v-if="me.account.password_changed_at">
              , set
              {{ formatDate(me.account.password_changed_at) }}
            </template>.
          </template>
          <template v-else>
            You don't have a password.
          </template>
        </p>
        <div class="d-flex flex-wrap ga-2">
          <v-btn
            color="primary"
            @click="openPassword"
          >
            {{ me.account.has_password ? 'Change password' : 'Add a password' }}
          </v-btn>
          <v-btn
            v-if="me.account.has_password"
            variant="text"
            @click="removePasswordOpen = true"
          >
            Remove password
          </v-btn>
        </div>
      </section>

      <section class="mb-10">
        <h2 class="text-h6 mb-1">
          Passkeys
        </h2>
        <p class="text-body-2 text-medium-emphasis mb-4">
          Passkeys let you sign in with your fingerprint, face, or device PIN, and can't be
          phished. They're optional: you can always sign in with a code we email you. Add one
          for each device you use.
        </p>

        <p
          v-if="passkeys.length === 0"
          class="text-body-2 mb-3"
        >
          You don't have a passkey yet. You sign in with codes we email you.
        </p>
        <v-list
          v-else
          lines="two"
          class="mb-3"
          border
          rounded
        >
          <v-list-item
            v-for="p in passkeys"
            :key="p.id"
            :prepend-icon="mdiKeyVariant"
            :title="p.name"
          >
            <template #subtitle>
              Added {{ formatDate(p.created_at) }}
              <template v-if="p.last_used_at">
                · last used {{ formatDate(p.last_used_at) }}
              </template>
              <template v-if="p.synced">
                · synced
              </template>
            </template>
            <template #append>
              <v-menu>
                <template #activator="{ props }">
                  <v-btn
                    v-bind="props"
                    :icon="mdiDotsVertical"
                    variant="text"
                    size="small"
                    :aria-label="`Options for ${p.name}`"
                  />
                </template>
                <v-list density="compact">
                  <v-list-item
                    title="Rename"
                    @click="openRename(p)"
                  />
                  <v-list-item
                    title="Remove"
                    :disabled="passkeys.length < 2 && !canRemoveLast"
                    base-color="error"
                    @click="removeTarget = p"
                  />
                </v-list>
              </v-menu>
            </template>
          </v-list-item>
        </v-list>
        <p
          v-if="passkeys.length === 1 && !canRemoveLast"
          class="text-body-2 text-medium-emphasis mb-3"
        >
          Confirm your email address before removing your only passkey, so you can still sign
          in by email.
        </p>
        <v-btn
          color="primary"
          :loading="busy && addOpen"
          @click="openAdd"
        >
          Add a passkey
        </v-btn>
      </section>

      <section>
        <h2 class="text-h6 mb-1">
          Signed-in devices
        </h2>
        <p class="text-body-2 text-medium-emphasis mb-4">
          If you don't recognize a device, sign it out, change your password, and check your passkeys.
        </p>

        <v-list
          lines="two"
          class="mb-3"
          border
          rounded
        >
          <v-list-item
            v-for="s in sessions"
            :key="s.id"
            :prepend-icon="s.client === 'mobile' ? mdiCellphone : mdiMonitor"
            :title="sessionLabel(s)"
          >
            <template #subtitle>
              <template v-if="s.current">
                This device ·
              </template>
              Last active {{ formatDateTime(s.last_seen_at) }} · signed in {{ formatDate(s.created_at) }}
            </template>
            <template #append>
              <v-btn
                v-if="!s.current"
                variant="text"
                size="small"
                @click="signOutDevice(s)"
              >
                Sign out
              </v-btn>
            </template>
          </v-list-item>
        </v-list>
        <v-btn
          v-if="otherSessions.length > 0"
          variant="outlined"
          :loading="busy"
          @click="signOutOthers"
        >
          Sign out all other devices
        </v-btn>
      </section>
    </template>

    <v-dialog
      v-model="addOpen"
      max-width="440"
    >
      <v-card title="Add a passkey">
        <v-card-text>
          <p class="mb-4">
            Your browser will ask you to create a passkey on this device or a security key.
          </p>
          <v-text-field
            v-model="addName"
            label="Name"
            hint="So you can tell your passkeys apart, e.g. &quot;Work laptop&quot;."
            persistent-hint
            maxlength="100"
          />
          <v-alert
            v-if="error"
            type="error"
            variant="tonal"
            class="mt-4"
          >
            {{ error }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="addOpen = false">
            Cancel
          </v-btn>
          <v-btn
            color="primary"
            :loading="busy"
            @click="addPasskey"
          >
            Continue
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog
      v-model="passwordOpen"
      max-width="440"
    >
      <v-card :title="me?.account.has_password ? 'Change password' : 'Add a password'">
        <v-form @submit.prevent="savePassword">
          <v-card-text>
            <PasswordField
              v-model="newPassword"
              autocomplete="new-password"
              label="New password"
              hint="At least 12 characters. A few unrelated words make a strong one."
            />
            <p class="text-body-2 mt-4">
              Your other devices will be signed out.
            </p>
            <v-alert
              v-if="error"
              type="error"
              variant="tonal"
              class="mt-4"
            >
              {{ error }}
            </v-alert>
          </v-card-text>
          <v-card-actions>
            <v-spacer />
            <v-btn @click="passwordOpen = false">
              Cancel
            </v-btn>
            <v-btn
              type="submit"
              color="primary"
              :loading="busy"
            >
              Save
            </v-btn>
          </v-card-actions>
        </v-form>
      </v-card>
    </v-dialog>

    <v-dialog
      v-model="removePasswordOpen"
      max-width="440"
    >
      <v-card title="Remove password?">
        <v-card-text>
          You'll sign in with codes we email you<template v-if="passkeys.length > 0">
            , or a
            passkey
          </template>. We'll email you a notice.
          <v-alert
            v-if="error"
            type="error"
            variant="tonal"
            class="mt-4"
          >
            {{ error }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="removePasswordOpen = false">
            Cancel
          </v-btn>
          <v-btn
            color="error"
            :loading="busy"
            @click="confirmRemovePassword"
          >
            Remove
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog
      :model-value="renameTarget !== null"
      max-width="440"
      @update:model-value="(open: boolean) => { if (!open) renameTarget = null }"
    >
      <v-card title="Rename passkey">
        <v-card-text>
          <v-text-field
            v-model="renameName"
            label="Name"
            maxlength="100"
            autofocus
          />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="renameTarget = null">
            Cancel
          </v-btn>
          <v-btn
            color="primary"
            :loading="busy"
            @click="saveRename"
          >
            Save
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog
      :model-value="removeTarget !== null"
      max-width="440"
      @update:model-value="(open: boolean) => { if (!open) removeTarget = null }"
    >
      <v-card title="Remove passkey?">
        <v-card-text>
          You won't be able to sign in with “{{ removeTarget?.name }}” any more.
          <template v-if="passkeys.length === 1">
            It's your only passkey, so you'll sign in with codes we email you.
          </template>
          We'll email you a notice.
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="removeTarget = null">
            Cancel
          </v-btn>
          <v-btn
            color="error"
            :loading="busy"
            @click="confirmRemove"
          >
            Remove
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog
      v-model="confirmOpen"
      max-width="440"
      persistent
    >
      <v-card title="Confirm it's you">
        <v-card-text>
          <p class="mb-4">
            To change how you sign in, please confirm it's you again. This protects your
            account if someone else gets hold of this device.
          </p>
          <template v-if="confirmCodeSent">
            <p class="mb-4">
              We've emailed a code to <strong>{{ me?.account.email }}</strong>.
            </p>
            <EmailCodeForm
              :submit="confirmWithCode"
              label="Confirm"
            />
          </template>
          <v-alert
            v-if="confirmError"
            type="error"
            variant="tonal"
            class="mt-3"
          >
            {{ confirmError }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="confirmOpen = false">
            Cancel
          </v-btn>
          <v-btn
            v-if="!confirmCodeSent"
            :loading="busy"
            @click="sendConfirmCode"
          >
            Email me a code
          </v-btn>
          <v-btn
            v-if="passkeys.length > 0"
            color="primary"
            :loading="busy"
            @click="confirmWithPasskey"
          >
            Use a passkey
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </v-container>
</template>
