// Package email tests.
package email

import (
	"strings"
	"testing"
)

func TestBuildMultipart(t *testing.T) {
	msg := string(buildMultipart(
		"noreply@jomform.com", "a@b.c", "Hai", "Badan emel",
		"resit.pdf", []byte("PDFDATA"),
	))
	for _, want := range []string{
		"From: noreply@jomform.com",
		"Subject: Hai",
		"Badan emel",
		"Content-Type: application/pdf; name=\"resit.pdf\"",
		"Content-Transfer-Encoding: base64",
		"UERGREFUQQ==", // base64("PDFDATA")
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("mesti ada %q", want)
		}
	}
}

func TestNewDefaultsRegion(t *testing.T) {
	s := New(SMTPConfig{From: "noreply@jomform.com"})
	if s.cfg.Region != "ap-southeast-1" {
		t.Fatalf("region = %q, want ap-southeast-1", s.cfg.Region)
	}
	if s.from != "noreply@jomform.com" {
		t.Fatalf("from = %q", s.from)
	}
}
