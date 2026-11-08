package payments

// Issue #597 END-TO-END PROOF — sends ONE REAL email through SES.
//
// Skipped unless JOMFORM_E2E_SEND_TO is set, so `go test ./...` never mails
// anyone. Run it explicitly:
//
//	JOMFORM_E2E_SEND_TO=someone@example.com \
//	JOMFORM_E2E_SES_USER=... JOMFORM_E2E_SES_PASS=... \
//	JOMFORM_E2E_SES_FROM=noreply@jomform.com \
//	go test ./internal/payments/ -run TestE2EResponseNotificationSendsRealEmail -v -count=1
//
// It drives the REAL path with no stub at any step: the default rule the store
// creates for a response-mode form -> renderNotification (the real template
// engine) -> email.Sender.SendReceipt (the real SESv2 client).
import (
	"os"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/email"
)

func TestE2EResponseNotificationSendsRealEmail(t *testing.T) {
	to := os.Getenv("JOMFORM_E2E_SEND_TO")
	if to == "" {
		t.Skip("JOMFORM_E2E_SEND_TO not set; skipping the real-send E2E proof")
	}

	// The rule a new response-mode form gets, from the single definition
	// EnsureDefault uses. Its stored halves are empty (card t_1768cb11); the text
	// that leaves is the dictionary's template in the form's language, resolved by
	// the same helper the send path uses.
	_, event, storedSubject, storedBody := dbstore.DefaultNotification(dbstore.FormModeResponse)
	if event != dbstore.NotifyEventResponseSubmitted {
		t.Fatalf("default rule for a response form fires on %q, not %q", event, dbstore.NotifyEventResponseSubmitted)
	}
	lang := "en" // the documented default; the operator sees a language they can read
	subject, body := notificationMessage(event, lang, storedSubject, storedBody)
	t.Logf("response-form default rule: event=%q subject=%q", event, subject)

	// A response submission as the public submit path builds it.
	ref := NotificationRef{
		FormID:    "e2e-597",
		FormURL:   "https://jomform.com/event-registration",
		BuyerName: "Siti Aminah",
		Fields: map[string]string{
			"b1": "Siti Aminah", "b2": "siti@example.test",
			"full_name": "Siti Aminah", "email": "siti@example.test",
		},
	}
	subj, rendered := renderNotification(subject, body, ref)
	t.Logf("RENDERED SUBJECT: %s", subj)
	t.Logf("RENDERED BODY:\n%s", rendered)
	if strings.Contains(subj, "{{") || strings.Contains(rendered, "{{") {
		t.Fatalf("an unrendered placeholder survived — refusing to send")
	}

	sender := email.New(email.SMTPConfig{
		Host:     os.Getenv("JOMFORM_E2E_SES_HOST"),
		Port:     os.Getenv("JOMFORM_E2E_SES_PORT"),
		Username: os.Getenv("JOMFORM_E2E_SES_USER"),
		Password: os.Getenv("JOMFORM_E2E_SES_PASS"),
		From:     os.Getenv("JOMFORM_E2E_SES_FROM"),
		Region:   "ap-southeast-1",
	})
	// The notification is a response-mode default, so its language is the
	// buyer/form axis; `lang` above is the ENGLISH rendering — the documented
	// default — so the operator sees a language they can read.
	msgID, err := sender.SendReceipt(to, subj, rendered, lang)
	if err != nil {
		t.Fatalf("REAL SES SEND FAILED: %v", err)
	}
	t.Logf("SES ACCEPTED: message_id=%q to=%s", msgID, to)
}
