// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/chmbrb/PetCommunityProject/internal/config"
)

// These run without a database: every check here happens before connecting.
func TestCasesArguments(t *testing.T) {
	const id = "0193a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"
	for _, tc := range []struct {
		name string
		args []string
		want string // empty: the usage error
	}{
		{"no command", nil, ""},
		{"unknown command", []string{"delete", id}, ""},
		{"show without an ID", []string{"show"}, ""},
		{"show with two IDs", []string{"show", id, id}, ""},
		{"list with an argument", []string{"list", "open"}, ""},
		{"unknown flag", []string{"export", "--operatr", "Sam", id}, ""},
		{"bad ID", []string{"show", "case-1"}, "isn't a case ID"},
		{"bad report time", []string{"report", "--operator", "Sam", "--reference", "1", "--reported-at", "yesterday", id}, "RFC 3339"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := cases(context.Background(), config.Config{}, tc.args, &bytes.Buffer{}, &bytes.Buffer{})
			if tc.want == "" {
				if !errors.Is(err, errCasesUsage) {
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

func TestCasesExportRefusesATerminal(t *testing.T) {
	// /dev/null is a character device, like a terminal.
	tty, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { _ = tty.Close() })
	err = cases(context.Background(), config.Config{}, []string{"export", "--operator", "Sam", "0193a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"}, tty, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "redirect it to a file") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunCasesUsage(t *testing.T) {
	var stderr bytes.Buffer
	getenv := func(k string) string {
		return map[string]string{"PC_DATABASE_URL": "postgres://localhost/none"}[k]
	}
	if code := run(context.Background(), []string{"cases"}, getenv, &bytes.Buffer{}, &stderr); code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "petcommunity cases <command>") {
		t.Errorf("stderr = %q, want the cases usage", stderr.String())
	}
}
