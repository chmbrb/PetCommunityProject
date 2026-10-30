// SPDX-License-Identifier: AGPL-3.0-or-later
import { ApiError } from './api/client'

/** Turns a sign-up or sign-in failure into a message for the person. */
export function describePasskeyError(err: unknown): string {
  if (err instanceof ApiError) {
    return err.message
  }
  if (err instanceof Error) {
    // Raised by the browser when the person closes the passkey prompt or it times out.
    if (err.name === 'NotAllowedError') {
      return 'The passkey request was cancelled or timed out. Please try again.'
    }
    if (err.name === 'InvalidStateError') {
      return 'This device already has a passkey for this account.'
    }
    return err.message
  }
  return 'Something went wrong. Please try again.'
}
