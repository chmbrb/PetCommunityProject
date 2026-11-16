// SPDX-License-Identifier: AGPL-3.0-or-later
import type { CapacitorConfig } from '@capacitor/cli'

// Development builds may connect to a local instance over plain HTTP (ADR 0023). The
// Android app loads from https://localhost, so it also has to allow mixed content and
// cleartext traffic. Release builds never set this.
const localInstances = process.env.VITE_ALLOW_LOCAL_INSTANCES === 'true'

// appId is permanent once an app is published to the stores. It's a placeholder
// until the project has a final name and domain (see docs/DEVELOPMENT.md).
const config: CapacitorConfig = {
  appId: 'org.petcommunityproject.app',
  appName: 'PetCommunity',
  webDir: 'dist',
  ...(localInstances && {
    android: { allowMixedContent: true },
    server: { cleartext: true },
  }),
}

export default config
