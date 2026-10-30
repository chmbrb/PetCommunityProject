#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Creates a throwaway certificate authority and a certificate for the local Mailpit
# server, so the worker can send over verified STARTTLS in the local production stack
# (compose.local.yaml). Run via `make local-certs`. The files are gitignored and valid
# for 90 days; delete deploy/local/certs and rerun to renew.
set -eu

dir="$(dirname "$0")/certs"
if [ -f "$dir/mailpit.crt" ] && openssl x509 -checkend 86400 -noout -in "$dir/mailpit.crt" >/dev/null; then
	echo "local certificates are up to date in $dir"
	exit 0
fi
mkdir -p "$dir"
cd "$dir"

openssl req -x509 -new -nodes -days 90 \
	-newkey ec -pkeyopt ec_paramgen_curve:P-256 \
	-subj "/CN=PetCommunityProject local test CA" \
	-addext "basicConstraints=critical,CA:TRUE" \
	-addext "keyUsage=critical,keyCertSign,cRLSign" \
	-keyout ca.key -out ca.crt 2>/dev/null

openssl req -new -nodes \
	-newkey ec -pkeyopt ec_paramgen_curve:P-256 \
	-subj "/CN=mailpit" \
	-keyout mailpit.key -out mailpit.csr 2>/dev/null

printf 'subjectAltName=DNS:mailpit\nextendedKeyUsage=serverAuth\nkeyUsage=critical,digitalSignature\n' > mailpit.ext
openssl x509 -req -days 90 -in mailpit.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
	-extfile mailpit.ext -out mailpit.crt 2>/dev/null
rm -f mailpit.csr mailpit.ext ca.srl

# The CA key is only needed to sign; the Mailpit key must be readable inside its container.
rm -f ca.key
chmod 644 ca.crt mailpit.crt mailpit.key
echo "created local test certificates in $dir"
