// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SignupInput is what a person enters before creating their passkey.
type SignupInput struct {
	Email       string `json:"email"`
	Handle      string `json:"handle"`
	DisplayName string `json:"display_name"`
}

// ValidationError reports one invalid field.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

var handlePattern = regexp.MustCompile(`^[a-z0-9_]{3,30}$`)

// reservedHandles can't be registered, so nobody can pose as the platform or its
// staff (THREAT_MODEL.md A3).
var reservedHandles = map[string]bool{
	"about": true, "abuse": true, "admin": true, "administrator": true, "api": true,
	"contact": true, "help": true, "info": true, "login": true, "logout": true,
	"mail": true, "me": true, "mod": true, "moderator": true, "moderators": true,
	"noreply": true, "official": true, "postmaster": true, "register": true,
	"root": true, "security": true, "settings": true, "signin": true, "signup": true,
	"staff": true, "support": true, "system": true, "team": true, "webmaster": true,
	"www": true,
}

// Normalize trims and lowercases the input and checks every field.
func (in SignupInput) Normalize() (SignupInput, error) {
	out := SignupInput{
		Email:       strings.ToLower(strings.TrimSpace(in.Email)),
		Handle:      strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(in.Handle), "@"))),
		DisplayName: strings.TrimSpace(in.DisplayName),
	}

	if addr, err := mail.ParseAddress(out.Email); err != nil || addr.Address != out.Email || len(out.Email) > 254 {
		return out, &ValidationError{Field: "email", Message: "enter a valid email address"}
	}

	switch {
	case !handlePattern.MatchString(out.Handle):
		return out, &ValidationError{Field: "handle", Message: "use 3–30 lowercase letters, numbers, or underscores"}
	case reservedHandles[out.Handle] || strings.Contains(out.Handle, "petcommunity"):
		return out, &ValidationError{Field: "handle", Message: "this handle is reserved"}
	}

	n := utf8.RuneCountInString(out.DisplayName)
	if n < 1 || n > 100 {
		return out, &ValidationError{Field: "display_name", Message: "enter a name of 1–100 characters"}
	}
	for _, r := range out.DisplayName {
		if unicode.IsControl(r) {
			return out, &ValidationError{Field: "display_name", Message: "the name contains characters that aren't allowed"}
		}
	}

	return out, nil
}
