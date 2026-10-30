#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Creates the secret files compose.yaml mounts into containers (see secrets/README.md).
# Generates the random ones and creates empty files for the ones your providers give
# you. Never overwrites an existing file: replacing backup_key or secret_key would make
# existing backups or actor keys unusable.
#
# Usage: deploy/make-secrets.sh [DIR]   (default: $PC_SECRETS_DIR, or ./secrets)
set -eu

dir="${1:-${PC_SECRETS_DIR:-./secrets}}"
umask 077
mkdir -p "$dir"
chmod 700 "$dir"

# Files are readable by everyone (0444) because the containers run as their own
# unprivileged users; the 0700 folder keeps other users on the host out.
generate() {
	if [ -e "$dir/$1" ]; then
		echo "  kept       $dir/$1"
		return
	fi
	head -c 32 /dev/urandom | base64 >"$dir/$1"
	chmod 444 "$dir/$1"
	echo "  generated  $dir/$1"
}

provided() {
	if [ -e "$dir/$1" ]; then
		echo "  kept       $dir/$1"
		return
	fi
	: >"$dir/$1"
	chmod 644 "$dir/$1"
	echo "  FILL IN    $dir/$1   ($2)"
}

echo "Secrets in $dir:"
generate db_password
generate secret_key
generate backup_key
provided smtp_password "your email provider's SMTP password; may stay empty if it needs none"
provided s3_access_key "your storage provider's access key ID"
provided s3_secret_key "your storage provider's secret key"
cat <<MSG

Keep an offline copy of backup_key and secret_key, away from this server and its
backups: backups can't be restored without backup_key, and federated identities
can't sign without secret_key. After filling in a file, run: chmod 444 FILE
MSG
