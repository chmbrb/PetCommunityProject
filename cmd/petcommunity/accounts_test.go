// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chmbrb/PetCommunityProject/internal/config"
)

// These run without a database: every check here happens before connecting.
func TestAccountsArguments(t *testing.T) {
	const id = "0193a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"
	for _, tc := range []struct {
		name string
		args []string
		want string // empty: the usage error
	}{
		{"no command", nil, ""},
		{"unknown command", []string{"delete", id}, ""},
		{"find without a query", []string{"find"}, ""},
		{"show with two IDs", []string{"show", id, id}, ""},
		{"remove-passkey with one ID", []string{"remove-passkey", "--operator", "Sam", "--reason", "x", id}, ""},
		{"unknown flag", []string{"suspend", "--why", "x", id}, ""},
		{"a handle instead of an ID", []string{"suspend", "--operator", "Sam", "--reason", "x", "@someone"}, "isn't an ID"},
		{"bad passkey ID", []string{"remove-passkey", "--operator", "Sam", "--reason", "x", id, "phone"}, "isn't an ID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := accounts(context.Background(), config.Config{}, tc.args, &bytes.Buffer{}, &bytes.Buffer{})
			if tc.want == "" {
				if !errors.Is(err, errAccountsUsage) {
					t.Fatalf("err = %v, want the usage error", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestSignupsArguments(t *testing.T) {
	for _, args := range [][]string{
		{"maybe"},
		{"off", "now"},
		{"on", "--because", "x"},
	} {
		if err := signups(context.Background(), config.Config{}, args, &bytes.Buffer{}, &bytes.Buffer{}); !errors.Is(err, errSignupsUsage) {
			t.Errorf("signups %v: err = %v, want the usage error", args, err)
		}
		if err := passwordSignIn(context.Background(), config.Config{}, args, &bytes.Buffer{}, &bytes.Buffer{}); !errors.Is(err, errPasswordSignInUsage) {
			t.Errorf("password-sign-in %v: err = %v, want the usage error", args, err)
		}
	}
}

func TestRunAccountsUsage(t *testing.T) {
	getenv := func(k string) string {
		return map[string]string{"PC_DATABASE_URL": "postgres://localhost/none"}[k]
	}
	for cmd, want := range map[string]string{
		"accounts":         "petcommunity accounts <command>",
		"signups":          "petcommunity signups [on|off",
		"password-sign-in": "petcommunity password-sign-in [on|off",
	} {
		var stderr bytes.Buffer
		args := []string{cmd, "help"}
		if code := run(context.Background(), args, getenv, &bytes.Buffer{}, &stderr); code != 2 {
			t.Errorf("%s: exit code = %d, want 2", cmd, code)
		}
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("%s: stderr = %q", cmd, stderr.String())
		}
	}
}
