// Package email tests.
package email

import (
	"strings"
	"testing"
)

func TestBuildMultipart(t *testing.T) {
	msg := string(buildMultipart(
		"noreply@jomform.com", "a@b.c", nil, "Hai", "Badan emel", "",
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
	// Issue #339: the body must be a multipart/alternative carrying both the
	// plain-text fallback and the branded HTML part. (The HTML part is
	// quoted-printable soft-wrapped, so only short substrings are asserted.)
	for _, want := range []string{
		"multipart/alternative",
		"Content-Type: text/html; charset=\"utf-8\"",
		"JomForm",
		"Made", // "Made in Malaysia" is quoted-printable encoded (=20 for workspaces)
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("mesti ada %q (branded HTML part)", want)
		}
	}
}

func TestBrandedHTML(t *testing.T) {
	html := BrandedHTML("", "Resit pembayaran", "Rujukan : ABC123\nJumlah  : RM 10.00")
	for _, want := range []string{
		"<!DOCTYPE html>",
		"JomForm",          // logo wordmark
		"#5e6ad2",          // brand indigo
		"#4338ca",          // brand deep indigo
		"Resit pembayaran", // title
		"Rujukan : ABC123", // body content
		"Made in Malaysia",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("mesti ada %q", want)
		}
	}
	// Body newlines must be converted to <br/> so the plain-text content
	// renders as separate lines inside the HTML shell.
	if !strings.Contains(html, "Rujukan : ABC123<br/>Jumlah  : RM 10.00") {
		t.Errorf("newlines mesti jadi <br/>")
	}
	// No external image URLs that could break (the xmlns namespace is fine).
	if strings.Contains(html, "src=\"http") || strings.Contains(html, "src='http") {
		t.Errorf("template mesti tiada external image URLs")
	}
}

func TestQuotedPrintable(t *testing.T) {
	// '=' must be encoded as =3D; non-ASCII bytes as =XX.
	got := quotedPrintable("a=b\u00e9")
	if !strings.Contains(got, "a=3Db") {
		t.Errorf("'=' mesti jadi =3D, got %q", got)
	}
	if !strings.Contains(got, "=C3=A9") {
		t.Errorf("non-ASCII mesti jadi =XX, got %q", got)
	}
	// Long lines must be soft-wrapped at 76 chars.
	long := strings.Repeat("x", 200)
	if !strings.Contains(quotedPrintable(long), "=\r\n") {
		t.Errorf("garis panjang mesti soft-wrap")
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
