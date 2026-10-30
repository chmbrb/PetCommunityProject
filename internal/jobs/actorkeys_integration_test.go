// SPDX-License-Identifier: AGPL-3.0-or-later

package jobs

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/chmbrb/PetCommunityProject/internal/actorkeys"
	"github.com/chmbrb/PetCommunityProject/internal/database"
)

func newKeyMaker(t *testing.T) (*KeyMaker, *database.Store, *actorkeys.Sealer, func(string, ...any)) {
	t.Helper()
	url := os.Getenv("PC_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PC_TEST_DATABASE_URL not set; skipping database integration test")
	}
	ctx := context.Background()
	if _, err := database.Migrate(ctx, url); err != nil {
		t.Fatal(err)
	}
	pool, err := database.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	sealer, err := actorkeys.NewSealer(bytes.Repeat([]byte{42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	st := database.NewStore(pool)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	return NewKeyMaker(st, sealer, slog.New(slog.NewTextHandler(io.Discard, nil))), st, sealer, exec
}

// newActor inserts an account and its person actor, without keys.
func newActor(t *testing.T, exec func(string, ...any)) uuid.UUID {
	t.Helper()
	accountID, actorID := uuid.New(), uuid.New()
	handle := "k_" + accountID.String()[:8]
	userHandle := sha512.Sum512(accountID[:]) // unique, 64 bytes
	exec(`INSERT INTO accounts (id, email, webauthn_user_handle) VALUES ($1, $2, $3)`,
		accountID, handle+"@example.org", userHandle[:])
	exec(`INSERT INTO actors (id, type, handle, display_name, account_id) VALUES ($1, 'person', $2, 'Key Test', $3)`,
		actorID, handle, accountID)
	return actorID
}

func TestEnsureCreatesUsableKeys(t *testing.T) {
	keys, st, sealer, exec := newKeyMaker(t)
	ctx := context.Background()
	actorID := newActor(t, exec)

	if err := keys.Ensure(ctx, actorID); err != nil {
		t.Fatal(err)
	}
	row, err := st.GetActorKeys(ctx, actorID)
	if err != nil {
		t.Fatalf("keys not stored: %v", err)
	}
	priv, err := sealer.Open(actorID, *row.PrivateKeyKid, row.PrivateKeyEnc)
	if err != nil {
		t.Fatalf("stored private key doesn't decrypt: %v", err)
	}
	pub, err := actorkeys.ParsePublicKeyPEM(*row.PublicKeyPem)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("test"))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, digest[:])
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sig); err != nil {
		t.Fatalf("stored keys don't form a pair: %v", err)
	}

	// Running again never replaces existing keys.
	if err := keys.Ensure(ctx, actorID); err != nil {
		t.Fatal(err)
	}
	again, _ := st.GetActorKeys(ctx, actorID)
	if *again.PublicKeyPem != *row.PublicKeyPem {
		t.Error("Ensure replaced an existing key pair")
	}
}

func TestBackfillCreatesMissingKeys(t *testing.T) {
	keys, st, _, exec := newKeyMaker(t)
	ctx := context.Background()
	actorID := newActor(t, exec)

	// Earlier test runs may leave many actors without keys; run until this one has them.
	for range 100 {
		n, err := keys.Backfill(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.GetActorKeys(ctx, actorID); err == nil {
			return
		}
		if n < backfillBatch {
			break
		}
	}
	t.Fatal("backfill didn't create keys for an actor without them")
}
