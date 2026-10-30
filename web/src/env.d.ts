// SPDX-License-Identifier: AGPL-3.0-or-later
/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** Instance the native apps connect to until the instance picker exists, e.g. https://pets.example.org */
  readonly VITE_DEFAULT_INSTANCE_URL?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
