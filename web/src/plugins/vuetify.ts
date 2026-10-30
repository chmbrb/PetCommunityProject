// SPDX-License-Identifier: AGPL-3.0-or-later
import 'vuetify/styles'
import { createVuetify } from 'vuetify'
import { aliases, mdi } from 'vuetify/iconsets/mdi-svg'

export default createVuetify({
  icons: {
    defaultSet: 'mdi',
    aliases,
    sets: { mdi },
  },
  theme: {
    defaultTheme: 'system',
    themes: {
      light: { colors: { primary: '#2e7d6b', secondary: '#c46b2d' } },
      dark: { colors: { primary: '#5fbfa8', secondary: '#e39a63' } },
    },
  },
})
