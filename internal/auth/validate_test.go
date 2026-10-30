// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"errors"
	"testing"
)

func TestNormalize(t *testing.T) {
	got, err := SignupInput{Email: "  Sam@Example.ORG ", Handle: " @Sam_99 ", DisplayName: " Sam "}.Normalize()
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	want := SignupInput{Email: "sam@example.org", Handle: "sam_99", DisplayName: "Sam"}
	if got != want {
		t.Errorf("Normalize = %+v, want %+v", got, want)
	}
}

func TestNormalizeRejects(t *testing.T) {
	valid := SignupInput{Email: "sam@example.org", Handle: "sam", DisplayName: "Sam"}
	tests := []struct {
		name  string
		edit  func(*SignupInput)
		field string
	}{
		{"missing email", func(in *SignupInput) { in.Email = "" }, "email"},
		{"email with display name", func(in *SignupInput) { in.Email = "Sam <sam@example.org>" }, "email"},
		{"not an email", func(in *SignupInput) { in.Email = "sam" }, "email"},
		{"short handle", func(in *SignupInput) { in.Handle = "ab" }, "handle"},
		{"handle with dash", func(in *SignupInput) { in.Handle = "sam-x" }, "handle"},
		{"reserved handle", func(in *SignupInput) { in.Handle = "admin" }, "handle"},
		{"impersonating handle", func(in *SignupInput) { in.Handle = "petcommunity_help" }, "handle"},
		{"empty name", func(in *SignupInput) { in.DisplayName = "  " }, "display_name"},
		{"control character in name", func(in *SignupInput) { in.DisplayName = "Sam\u0007" }, "display_name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := valid
			tt.edit(&in)
			_, err := in.Normalize()
			var verr *ValidationError
			if !errors.As(err, &verr) || verr.Field != tt.field {
				t.Fatalf("Normalize error = %v, want ValidationError on %q", err, tt.field)
			}
		})
	}
}
