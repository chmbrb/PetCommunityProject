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
		"verify": VerifyEmail, "sign-in": SignInLink, "sign-up": SignUp,
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

func TestCodes(t *testing.T) {
	data := LinkEmail{InstanceName: "Eastside Pets", DisplayName: "Sam", Link: "https://x/#token=a", ExpiresIn: "15 minutes", Code: "042917"}
	for name, render := range map[string]func(string, LinkEmail) (Message, error){
		"sign-in": SignInLink, "sign-up": SignUp,
	} {
		msg, err := render("sam@example.org", data)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(msg.Text, "042917") || !strings.Contains(msg.HTML, "042917") {
			t.Errorf("%s email lacks the code", name)
		}
	}
	// Sign-in emails without a code (none are sent today) don't mention one.
	data.Code = ""
	msg, err := SignInLink("sam@example.org", data)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(msg.Text, "code") {
		t.Errorf("sign-in email without a code mentions one:\n%s", msg.Text)
	}
}

func TestNotices(t *testing.T) {
	data := LinkEmail{
		InstanceName: "Eastside Pets", DisplayName: "Sam",
		Link: "https://pets.example.org/settings", Detail: "Work laptop <b>",
	}
	for name, render := range map[string]func(string, LinkEmail) (Message, error){
		"added": PasskeyAdded, "removed": PasskeyRemoved,
	} {
		t.Run(name, func(t *testing.T) {
			msg, err := render("sam@example.org", data)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(msg.Subject, name) {
				t.Errorf("Subject %q doesn't say %q", msg.Subject, name)
			}
			if !strings.Contains(msg.Text, "Work laptop <b>") || !strings.Contains(msg.Text, data.Link) {
				t.Errorf("text part:\n%s", msg.Text)
			}
			// The passkey name is user input.
			if strings.Contains(msg.HTML, "<b>") || !strings.Contains(msg.HTML, "Work laptop &lt;b&gt;") {
				t.Errorf("passkey name not escaped in HTML")
			}
		})
	}
}

func TestPasswordEmails(t *testing.T) {
	msg, err := NewDevice("sam@example.org", LinkEmail{
		InstanceName: "Eastside Pets", DisplayName: "Sam", Link: "https://pets.example.org/settings",
		ExpiresIn: "15 minutes", Code: "123456", Detail: "Firefox <on Linux>",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg.Text, "123456") || !strings.Contains(msg.Text, "If this wasn't you") {
		t.Errorf("new-device email lacks the code or the warning:\n%s", msg.Text)
	}
	// The device description comes from the request's User-Agent header.
	if strings.Contains(msg.HTML, "<on Linux>") {
		t.Error("device description not escaped in HTML")
	}
	for _, detail := range []string{"added", "changed", "removed"} {
		msg, err := PasswordChanged("sam@example.org", LinkEmail{InstanceName: "Eastside Pets", DisplayName: "Sam", Link: "https://x/settings", Detail: detail})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(msg.Subject, "password was "+detail) {
			t.Errorf("subject %q", msg.Subject)
		}
		if signedOut := strings.Contains(msg.Text, "other devices were signed out"); signedOut == (detail == "removed") {
			t.Errorf("%s notice: mentions signing out = %v", detail, signedOut)
		}
	}
}

func TestMFAChanged(t *testing.T) {
	msg, err := MFAChanged("sam@example.org", LinkEmail{
		InstanceName: "Eastside Pets", DisplayName: "Sam", Link: "https://x/settings",
		Detail: "A recovery code was used to sign in (9 left)",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg.Text, "A recovery code was used to sign in (9 left) on your Eastside Pets account.") {
		t.Errorf("text:\n%s", msg.Text)
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
