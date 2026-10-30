// SPDX-License-Identifier: AGPL-3.0-or-later
import type { CapacitorConfig } from '@capacitor/cli'

// appId is permanent once an app is published to the stores. It's a placeholder
// until the project has a final name and domain (see docs/DEVELOPMENT.md).
const config: CapacitorConfig = {
  appId: 'org.petcommunityproject.app',
  appName: 'PetCommunity',
  webDir: 'dist',
}

export default config
