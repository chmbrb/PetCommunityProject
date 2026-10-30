// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"context"
	"testing"
)

func TestSignUpQueuesActorKeys(t *testing.T) {
	e := newAuthTestEnv(t)
	pk := e.signUp(e.newClient(), uniqueHandle(t))

	var n int
	err := e.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM river_job j
		JOIN actors a ON a.id::text = j.args->>'actor_id'
		WHERE j.kind = 'actor_keys' AND a.account_id = $1`, pk.accountID).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("actor key jobs queued at sign-up = %d, want 1", n)
	}
}
