// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/chmbrb/PetCommunityProject/internal/actorkeys"
	"github.com/chmbrb/PetCommunityProject/internal/auth"
	"github.com/chmbrb/PetCommunityProject/internal/config"
	"github.com/chmbrb/PetCommunityProject/internal/database"
	"github.com/chmbrb/PetCommunityProject/internal/keyrotation"
	"github.com/chmbrb/PetCommunityProject/internal/media"
	"github.com/chmbrb/PetCommunityProject/internal/storage"
)

const keysUsage = `usage: petcommunity keys status|reencrypt

Rotating the instance's encryption keys (ADR 0025; docs/DEVELOPMENT.md, "Rotating
encryption keys"). Each key's data is handled where the key is:
  docker compose run --rm -T worker keys reencrypt   # PC_SECRET_KEY: actor keys, evidence
  docker compose run --rm -T app keys reencrypt      # mfa_key: authenticator secrets

commands:
  status
      how much of each kind of data is under the current key, under the previous
      key (still to re-encrypt), or under neither (a key is missing)
  reencrypt
      move everything under the previous key to the current one; safe to stop and
      run again. Then remove the previous key, but keep it offline until backups
      from before the rotation have expired.
`

var errKeysUsage = errors.New("keys usage")

func keys(ctx context.Context, cfg config.Config, args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return errKeysUsage
	}
	sub := args[0]
	if sub != "status" && sub != "reencrypt" {
		return errKeysUsage
	}
	ks, err := rotationKeys(cfg)
	if err != nil {
		return err
	}
	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	r := keyrotation.New(database.NewStore(pool), ks)

	var reports []keyrotation.Report
	if sub == "status" {
		reports, err = r.Status(ctx)
	} else {
		reports, err = r.Reencrypt(ctx)
	}
	if err != nil {
		return err
	}
	var unreadable int64
	for _, rep := range reports {
		fmt.Fprintln(stdout, rep)
		unreadable += rep.Unreadable
	}
	if sub == "reencrypt" && unreadable > 0 {
		return fmt.Errorf("%d values are under neither key; put the key that sealed them in *_PREVIOUS and run again", unreadable)
	}
	return nil
}

// rotationKeys builds the keyrings for the keys this process holds.
func rotationKeys(cfg config.Config) (keyrotation.Keys, error) {
	var ks keyrotation.Keys
	if len(cfg.SecretKey) > 0 {
		sealer, err := actorkeys.NewSealer(cfg.SecretKey, cfg.SecretKeyPrevious)
		if err != nil {
			return ks, err
		}
		evidence, err := media.NewEvidenceSealer(cfg.SecretKey, cfg.SecretKeyPrevious)
		if err != nil {
			return ks, err
		}
		ks.ActorKeys, ks.Evidence = sealer, evidence
		if cfg.Storage.EvidenceBucket != "" {
			objects, err := storage.NewS3(cfg.Storage)
			if err != nil {
				return ks, err
			}
			ks.EvidenceBucket = objects.Evidence
		}
	}
	if len(cfg.MFAKey) > 0 {
		mfa, err := auth.NewMFAKeyring(cfg.MFAKey, cfg.MFAKeyPrevious)
		if err != nil {
			return ks, err
		}
		ks.MFA = mfa
	}
	return ks, nil
}
