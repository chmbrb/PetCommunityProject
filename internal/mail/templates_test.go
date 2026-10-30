// SPDX-License-Identifier: AGPL-3.0-or-later

package mail

import (
	"strings"
	"testing"
)

func TestTemplates(t *testing.T) {
	data := LinkEmail{
		InstanceName: "Eastside Pets",
		DisplayName:  "Sam <script>",
		Link:         "https://pets.example.org/signin/email#token=abc_123",
		ExpiresIn:    "15 minutes",
	}
	for name, render := range map[string]func(string, LinkEmail) (Message, error){
		"verify": VerifyEmail, "sign-in": SignInLink,
	} {
		t.Run(name, func(t *testing.T) {
			msg, err := render("sam@example.org", data)
			if err != nil {
				t.Fatal(err)
			}
			if msg.To != "sam@example.org" || !strings.Contains(msg.Subject, "Eastside Pets") {
				t.Errorf("To %q, Subject %q", msg.To, msg.Subject)
			}
			for _, part := range []string{msg.Text, msg.HTML} {
				if !strings.Contains(part, data.Link) {
					t.Errorf("body lacks the link:\n%s", part)
				}
				if !strings.Contains(part, "15 minutes") {
					t.Errorf("body lacks the expiry:\n%s", part)
				}
			}
			// Names are user input; the HTML part must escape them.
			if strings.Contains(msg.HTML, "<script>") || !strings.Contains(msg.HTML, "Sam &lt;script&gt;") {
				t.Errorf("display name not escaped in HTML:\n%s", msg.HTML)
			}
			if !strings.HasPrefix(msg.HTML, "<!doctype html>") {
				t.Errorf("HTML part doesn't start with the layout: %.40q", msg.HTML)
			}
		})
	}
}

func TestRecorder(t *testing.T) {
	var r Recorder
	_ = r.Send(t.Context(), Message{To: "a@example.org"})
	_ = r.Send(t.Context(), Message{To: "b@example.org"})
	if got := r.Messages(); len(got) != 2 || got[1].To != "b@example.org" {
		t.Fatalf("Messages = %+v", got)
	}
}
