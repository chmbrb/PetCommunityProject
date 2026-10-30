// SPDX-License-Identifier: AGPL-3.0-or-later

// Package version reports the version of the running build.
package version

// Version is set at build time with
// -ldflags "-X github.com/chmbrb/PetCommunityProject/internal/version.Version=<version>".
var Version = "dev"
