package emailtext

import (
	"strings"
	"testing"
)

// TestBrandedFooterCarriesNoRightsReservation pins the licence line in the
// branded email shell.
//
// WHY THIS EXISTS
//
//	The project went private in 2026-09 and every public surface was rewritten
//	to claim "© JomForm. All rights reserved." — including this footer, in all
//	five locales. The project is MIT again, so a footer that reserves all rights
//	is a false statement about the code the reader is being emailed from, and it
//	is a statement no other surface makes any more. Nothing else notices: the
//	footer is a string in a dictionary, and the only test that read it asserted
//	the MALAY copy was present, which the reservation satisfied just as well.
//
// WHAT IT PINS
//
//	For EVERY locale: the footer names the licence, and it carries no
//	reservation of rights — the English wording and the same claim in the four
//	translations, because the proprietary sentence shipped in all five.
func TestBrandedFooterCarriesNoRightsReservation(t *testing.T) {
	if len(Langs) != 5 {
		t.Fatalf("the dictionary declares %d locales; this guard was written for the five "+
			"shipped locales and would silently check a different set", len(Langs))
	}
	// The reservation of rights, in each shipped language. The English phrase is
	// the one the card measured; the others are the same sentence translated.
	forbidden := []string{
		"all rights reserved",
		"hak cipta terpelihara",
		"版权所有",
		"جميع الحقوق محفوظة",
		"உரிமைகளும் பாதுகாக்கப்பட்டவை",
		"proprietary",
	}
	for _, lang := range Langs {
		phrase := Phrase(lang, PhraseCopyright)
		if strings.TrimSpace(phrase) == "" {
			t.Errorf("PhraseCopyright has no %s rendering — the shell would fall back to another "+
				"language's licence line", lang)
			continue
		}
		for _, bad := range forbidden {
			if strings.Contains(strings.ToLower(phrase), strings.ToLower(bad)) {
				t.Errorf("PhraseCopyright (%s) still reserves rights: %q contains %q. The code is "+
					"MIT-licensed — the footer must say so, not the opposite.", lang, phrase, bad)
			}
		}
		if !strings.Contains(phrase, "MIT") {
			t.Errorf("PhraseCopyright (%s) = %q does not name the licence. A footer that says "+
				"neither MIT nor a reservation tells the reader nothing true.", lang, phrase)
		}
	}
}
