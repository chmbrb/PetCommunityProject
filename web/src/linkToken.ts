// SPDX-License-Identifier: AGPL-3.0-or-later

/**
 * Reads the one-time token from an email link (`#token=...`) and removes it from the
 * address bar and history, so it isn't left behind or shared by accident. Email links
 * put the token in the fragment, which browsers never send to the server.
 */
export function takeLinkToken(loc: Location = window.location, hist: History = window.history): string | null {
  const token = new URLSearchParams(loc.hash.replace(/^#/, '')).get('token')
  if (token) {
    hist.replaceState(hist.state, '', loc.pathname + loc.search)
  }
  return token
}
