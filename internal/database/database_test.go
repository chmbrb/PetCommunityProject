// SPDX-License-Identifier: AGPL-3.0-or-later

package database

import (
	"context"
	"os"
	"testing"
)

// testDatabaseURL returns the integration test database, or skips the test.
// Set PC_TEST_DATABASE_URL to a disposable PostGIS database to run these tests.
func testDatabaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("PC_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PC_TEST_DATABASE_URL not set; skipping database integration test")
	}
	return url
}

func TestMigrateAndQuery(t *testing.T) {
	url := testDatabaseURL(t)
	ctx := context.Background()

	if _, err := Migrate(ctx, url); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// Running again must be a no-op.
	again, err := Migrate(ctx, url)
	if err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("second Migrate applied %d migrations, want 0", len(again))
	}

	statuses, err := Status(ctx, url)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	for _, s := range statuses {
		if !s.Applied {
			t.Errorf("migration %d (%s) not applied", s.Version, s.Source)
		}
	}

	pool, err := Connect(ctx, url)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer pool.Close()

	flags, err := NewStore(pool).ListFeatureFlags(ctx)
	if err != nil {
		t.Fatalf("ListFeatureFlags: %v", err)
	}
	got := map[string]bool{}
	for _, f := range flags {
		got[f.Flag] = f.Enabled
	}
	// High-liability features must default to off (ADR 0017).
	for _, flag := range []string{"pet_sitting", "breeder_listings", "federation"} {
		enabled, ok := got[flag]
		if !ok {
			t.Errorf("flag %q missing", flag)
		} else if enabled {
			t.Errorf("flag %q enabled by default, want disabled", flag)
		}
	}
}
