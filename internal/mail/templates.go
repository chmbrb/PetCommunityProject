// SPDX-License-Identifier: AGPL-3.0-or-later

package mail

import (
	"bytes"
	"embed"
	htmltemplate "html/template"
	"strings"
	texttemplate "text/template"
)

//go:embed templates
var templateFiles embed.FS

var (
	textTemplates = texttemplate.Must(texttemplate.ParseFS(templateFiles, "templates/*.txt"))
	htmlTemplates = htmltemplate.Must(htmltemplate.ParseFS(templateFiles, "templates/*.html"))
)

// LinkEmail is the content of an email whose purpose is a single link.
type LinkEmail struct {
	InstanceName string
	DisplayName  string
	Link         string
	// ExpiresIn describes how long the link works, e.g. "15 minutes".
	ExpiresIn string
	// Detail is extra context for notices, e.g. the passkey's name.
	Detail string
}

// VerifyEmail asks the person to confirm their email address.
func VerifyEmail(to string, data LinkEmail) (Message, error) {
	return render(to, "Confirm your email for "+data.InstanceName, "verify_email", data)
}

// SignInLink sends a one-time sign-in link.
func SignInLink(to string, data LinkEmail) (Message, error) {
	return render(to, "Your sign-in link for "+data.InstanceName, "sign_in_link", data)
}

// PasskeyAdded tells the person a passkey was added, in case it wasn't them.
func PasskeyAdded(to string, data LinkEmail) (Message, error) {
	return render(to, "A passkey was added to your "+data.InstanceName+" account", "passkey_added", data)
}

// PasskeyRemoved tells the person a passkey was removed, in case it wasn't them.
func PasskeyRemoved(to string, data LinkEmail) (Message, error) {
	return render(to, "A passkey was removed from your "+data.InstanceName+" account", "passkey_removed", data)
}

func render(to, subject, name string, data LinkEmail) (Message, error) {
	var text, html bytes.Buffer
	if err := textTemplates.ExecuteTemplate(&text, name+".txt", data); err != nil {
		return Message{}, err
	}
	if err := htmlTemplates.ExecuteTemplate(&html, name+".html", data); err != nil {
		return Message{}, err
	}
	return Message{To: to, Subject: subject, Text: strings.TrimSpace(text.String()) + "\n", HTML: html.String()}, nil
}
