// Package email tests.
package email

import (
	"strings"
	"testing"
)

func TestBuildMessage(t *testing.T) {
	msg := string(buildMessage(map[string]string{
		"From": "noreply@jomform.com", "To": "a@b.c", "Subject": "Hai",
		"MIME-Version": "1.0", "Content-Type": `text/plain; charset="utf-8"`,
	}, "Badan emel"))
	for _, want := range []string{"From: noreply@jomform.com", "Subject: Hai", "Badan emel"} {
		if !strings.Contains(msg, want) {
			t.Errorf("mesti ada %q", want)
		}
	}
}

func TestHostOf(t *testing.T) {
	if hostOf("email-smtp.ap-southeast-1.amazonaws.com") != "email-smtp.ap-southeast-1.amazonaws.com" {
		t.Fatal("plain host rosak")
	}
	if hostOf("https://smtp.example.com") != "smtp.example.com" {
		t.Fatal("scheme tidak dibuang")
	}
}
