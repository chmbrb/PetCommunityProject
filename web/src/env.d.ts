// SPDX-License-Identifier: AGPL-3.0-or-later
/// <reference types="vite/client" />

interface ImportMetaEnv {
  /**
   * The instance the native apps preselect in the instance picker, and the first on its
   * list of known instances, e.g. https://pets.example.org
   */
  readonly VITE_DEFAULT_INSTANCE_URL?: string
  /**
   * "true" in development builds of the apps: lets the instance picker connect to local
   * instances over plain HTTP, and to instances without PC_INSTANCE_DOMAIN set.
   */
  readonly VITE_ALLOW_LOCAL_INSTANCES?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
