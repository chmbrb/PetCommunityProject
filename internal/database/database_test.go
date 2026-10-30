// SPDX-License-Identifier: AGPL-3.0-or-later

package database

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"testing"
	"time"
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

// TestConcurrentMigrate runs migrations from several overlapping processes against a
// fresh database, as parallel test packages in CI or two migrate containers do. Each
// child process is this test binary running TestMigrateChild.
func TestConcurrentMigrate(t *testing.T) {
	base := testDatabaseURL(t)
	ctx := context.Background()

	admin, err := Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("pc_concurrent_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		admin.Close()
		t.Skipf("can't create a scratch database (%v); skipping", err)
	}
	// Cleanup, not defer: it must run after the child processes finish, and close the
	// admin connection only after dropping the database.
	t.Cleanup(func() {
		defer admin.Close()
		if _, err := admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
			t.Errorf("drop scratch database %s: %v", name, err)
		}
	})

	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name

	// Starts are staggered: processes starting at the same instant queue behind each
	// other's table locks, while one starting partway through another's run collides.
	const processes = 6
	cmds := make([]*exec.Cmd, processes)
	outputs := make([]*bytes.Buffer, processes)
	for i := range cmds {
		time.Sleep(15 * time.Millisecond)
		cmd := exec.Command(os.Args[0], "-test.run=^TestMigrateChild$", "-test.count=1")
		cmd.Env = append(os.Environ(), "PC_MIGRATE_CHILD_URL="+u.String())
		outputs[i] = &bytes.Buffer{}
		cmd.Stdout, cmd.Stderr = outputs[i], outputs[i]
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds[i] = cmd
	}
	for i, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Errorf("migrate process %d failed: %v\n%s", i+1, err, outputs[i])
		}
	}
}

// TestMigrateChild migrates the database named by PC_MIGRATE_CHILD_URL. It only runs
// as a child process of TestConcurrentMigrate.
func TestMigrateChild(t *testing.T) {
	url := os.Getenv("PC_MIGRATE_CHILD_URL")
	if url == "" {
		t.Skip("only runs as a child of TestConcurrentMigrate")
	}
	if _, err := Migrate(context.Background(), url); err != nil {
		t.Fatal(err)
	}
}
