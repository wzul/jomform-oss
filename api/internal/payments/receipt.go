// Receipt email on successful payment (SES SMTP).
package payments

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"log/slog"
	"strings"
	"text/template"
	texttemplate "text/template"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/email"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/emailtext"
)

// Receipts bundles email config; nil disables receipts entirely.
type Receipts struct {
	Sender *email.Sender
	Base   string
	// Suppression (issue #56): consult before sending so a buyer who hard
	// bounces or complains never receives another receipt. Nil disables the
	// check. Receipts are NOT gated by the verification breaker — paying
	// customers must get their receipts.
	Suppression email.Suppressor
}

// defaultReceiptSubject / defaultReceiptBody are the language-specific receipt
// templates. They live in the shared transactional dictionary
// (api/internal/emailtext) so a receipt, a merchant notification and every auth
// email cannot drift apart — the same strings used to be duplicated here, in
// dbstore/form_email.go and in migration 000021.
func defaultReceipt(lang string) (subject, body string) {
	m := emailtext.Text(lang, emailtext.Receipt, nil)
	return m.Subject, m.Body
}

// SaleRef carries the minimal receipt fields.
type SaleRef struct {
	// SaleID is the sale's uuid, used to persist the SES MessageID back onto
	// the sale row for open/click correlation (issue #106). Empty when the
	// caller has no sale id (e.g. a bare receipt test).
	SaleID       string
	RefCode      string
	BuyerEmail   string
	BuyerName    string
	AmountSen    int64
	ItemsSummary string
	FormURL      string
	WorkspaceID  string // resolves per-workspace email settings (issue #107)
	// AccountID is the sale's owning ACCOUNT (not the workspace). It exists so
	// a receipt that could not even be enqueued can be recorded in
	// email_delivery_events, which is account-scoped (card t_1c4a3f54). The
	// enqueued job keeps carrying WorkspaceID in its AccountID field, which is
	// what the worker has always logged; the two are deliberately separate.
	AccountID string
	FormID    string // resolves per-form email settings override (issue #177)
	TIN       string // LHDN e-invoicing (optional)
	MSIC      string

	// FormLang / WorkspaceLang are the BUYER-facing language inputs (language
	// consistency): the form's own `config.lang` override and the workspace's
	// `default_lang`. They resolve to the receipt's language in that order,
	// falling back to English.
	//
	// They are carried on the ref rather than looked up here because the caller
	// already holds the form and the workspace row, and because it makes the
	// two-axis rule visible at the call site: these fields are the form/workspace
	// axis, and the merchant's personal users.lang must NEVER be assigned to
	// them.
	FormLang      string
	WorkspaceLang string

	// Card t_135c3c72: the processing fee the buyer bore, and the rate that was
	// in force when they paid. These are a SNAPSHOT read from the sale row, so a
	// later rate change cannot rewrite a past receipt. FeeSen == 0 means no fee
	// line is rendered at all.
	FeeSen       int64
	FeeMethod    string
	FeeFixedSen  int64
	FeePercentBp int64
}

// FeeLine is the receipt's processing-fee line, e.g.
// "Processing fee — Card 2.00%: RM 10.10". Empty when the buyer bore no fee, so
// a receipt for a sale without one is byte-identical to before the feature.
//
// The wording comes from the FROZEN snapshot (FeeMethod/FeeFixedSen/
// FeePercentBp), never from the merchant's current settings.
func (r SaleRef) FeeLine() string {
	if r.FeeSen <= 0 {
		return ""
	}
	return "Processing fee — " + FeeRateLabel(r.FeeMethod, r.FeeFixedSen, r.FeePercentBp) +
		": RM " + formatSen(r.FeeSen)
}

// receiptData is the placeholder context rendered into a receipt template.
// Field names are the {{placeholders}} a merchant can use in the editor.
type receiptData struct {
	RefCode   string
	Amount    string // formatted "123.45" (RM, two decimals from integer sen)
	Items     string
	FormURL   string
	BuyerName string
	Date      string
}

// renderReceiptBody builds the plain-text receipt body from a SaleRef. Kept
// as a pure function so the money/URL formatting is unit-testable (issue #99).
// It renders the built-in template for lang.
func renderReceiptBody(ref SaleRef, lang string) string {
	subject, body := defaultReceipt(lang)
	_, body = renderReceipt(ref, subject, body, lang, false)
	return body
}

// renderReceipt renders a receipt subject+body from a template string using
// the sale data. html=true uses html/template (auto-escaping, no XSS);
// otherwise text/template. Empty subject/body fall back to the built-in
// template for lang.
func renderReceipt(ref SaleRef, subject, body, lang string, html bool) (subj, rendered string) {
	if strings.TrimSpace(subject) == "" || strings.TrimSpace(body) == "" {
		defSubject, defBody := defaultReceipt(lang)
		if strings.TrimSpace(subject) == "" {
			subject = defSubject
		}
		if strings.TrimSpace(body) == "" {
			body = defBody
		}
	}
	data := receiptData{
		RefCode:   ref.RefCode,
		Amount:    fmt.Sprintf("%.2f", float64(ref.AmountSen)/100),
		Items:     receiptItems(ref),
		FormURL:   ref.FormURL,
		BuyerName: ref.BuyerName,
		Date:      time.Now().Format("2006-01-02"),
	}
	subj = renderTemplate(subject, data, html)
	rendered = renderTemplate(body, data, html)
	return subj, rendered
}

// receiptItems is the {{items}} value for a receipt: the item summary, plus the
// processing fee as its OWN line when the buyer bore one (card t_135c3c72). The
// fee is never folded into an item's price — a buyer who reads the receipt must
// be able to see what the goods cost and what the charge for paying was.
func receiptItems(ref SaleRef) string {
	items := ref.ItemsSummary
	if line := ref.FeeLine(); line != "" {
		if items == "" {
			return line
		}
		return items + "\n" + line
	}
	return items
}

// renderTemplate executes a single template string against the receipt data.
// The {{placeholders}} (no leading dot) are registered as template functions
// so a merchant can write {{ref_code}}, {{amount}}, etc. directly. html=true
// uses html/template (context-aware auto-escaping); otherwise text/template.
// A template error falls back to the raw string so a bad merchant template
// can never block a paying buyer's receipt.
func renderTemplate(src string, data receiptData, html bool) string {
	funcs := template.FuncMap{
		"ref_code":   func() string { return data.RefCode },
		"amount":     func() string { return data.Amount },
		"items":      func() string { return data.Items },
		"form_url":   func() string { return data.FormURL },
		"buyer_name": func() string { return data.BuyerName },
		"date":       func() string { return data.Date },
	}
	if html {
		t, err := htmltemplate.New("receipt").Funcs(funcs).Parse(src)
		if err != nil {
			return src
		}
		var b bytes.Buffer
		if err := t.Execute(&b, data); err != nil {
			return src
		}
		return b.String()
	}
	t, err := texttemplate.New("receipt").Funcs(funcs).Parse(src)
	if err != nil {
		return src
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return src
	}
	return b.String()
}

// sendReceiptBackgroundTimeout bounds the receipt work that runs after the HTTP
// response (card t_1c4a3f54): rendering the PDF, resolving the workspace
// settings and offering the jobs to the queue. A detached context has no
// deadline of its own, so without this a hung DB or render would leak the
// goroutine forever.
const sendReceiptBackgroundTimeout = 2 * time.Minute

// Enqueue ladder for the receipt + merchant jobs (card t_1c4a3f54). A queue that
// is briefly unreachable (Valkey restart, network blip) must not cost a paying
// buyer their receipt, so the offer is retried before it is declared failed.
const (
	enqueueAttempts = 4
	// enqueueBackoff is multiplied by the attempt number: 250ms, 500ms, 750ms.
	enqueueBackoff = 250 * time.Millisecond
)

// enqueueEmailJob offers one job to the email queue and retries a transient
// failure. When every attempt fails it records a durable `enqueue_failed`
// delivery event (account-scoped, correlated to the sale) and returns the last
// error, so the caller can log it.
//
// Why the record matters: an ENQUEUE failure is invisible to the queue's own
// retry ladder (delayed set → dead letter), because that ladder only ever sees
// jobs that reached Valkey. A job that could not be enqueued is gone, and before
// this the only trace was a `slog.Warn` — which is how the buyer receipt for sale
// JF-374daeb0ce61 disappeared in production with the seller never finding out.
//
// The queue write itself runs on a context that carries the caller's values but
// NOT its cancellation: a job must never be dropped because the request that
// produced it finished first.
func (s *Service) enqueueEmailJob(ctx context.Context, accountID string, job emailqueue.Job) error {
	queueCtx := context.WithoutCancel(ctx)
	var lastErr error
	tried := 0
	for attempt := 1; attempt <= enqueueAttempts; attempt++ {
		tried = attempt
		if lastErr = s.EmailQueue.Enqueue(queueCtx, job); lastErr == nil {
			return nil
		}
		if attempt == enqueueAttempts || ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
			// The caller's context is gone; stop waiting, but still record the
			// failure below rather than returning silently.
		case <-time.After(enqueueBackoff * time.Duration(attempt)):
		}
	}
	if s.EmailEvents != nil {
		detail := map[string]any{
			"job_type": job.Type,
			"subject":  job.Subject,
			"attempts": tried,
			"error":    lastErr.Error(),
		}
		if rerr := s.EmailEvents.Record(queueCtx, accountID, job.To,
			dbstore.EmailEventEnqueueFailed, "", job.SaleRef, detail); rerr != nil {
			slog.Error("email_enqueue_failure_unrecorded",
				"type", job.Type, "ref", job.SaleRef, "err", rerr)
		}
	}
	return lastErr
}

// SendSaleReceipt emails the buyer a receipt rendered from the workspace's email
// settings (issue #107). Respects receipt_enabled (off = no buyer email) and
// suppression (#56). Failures are logged but NEVER fail the payment flow.
//
// Issue #199: when the Service has an EmailQueue wired, the receipt + merchant
// notification are enqueued and sent by a background worker — the request
// handler returns immediately and a slow SES call never blocks the payment
// callback. Rendering (cheap local work) still happens here so the worker only
// performs the SES network call. When EmailQueue is nil (bare-mode tests) the
// send happens synchronously as before.
func (s *Service) SendSaleReceipt(ctx context.Context, ref SaleRef) {
	if s.Receipts == nil || s.Receipts.Sender == nil || ref.BuyerEmail == "" {
		return
	}
	// Issue #56: never resend a receipt to a suppressed address.
	if s.Receipts.Suppression != nil {
		sup, err := email.ShouldSuppress(ctx, s.Receipts.Suppression, ref.BuyerEmail)
		if err != nil {
			// suppression check failure: log, don't block a paying buyer's receipt
			slog.Warn("receipt_suppression_check_error", "err", err, "ref", ref.RefCode)
		} else if sup {
			slog.Warn("receipt_suppressed_skipped", "ref", ref.RefCode, "to", ref.BuyerEmail)
			return
		}
	}

	// Per-workspace settings (issue #107). Defaults reproduce the old output.
	var ws *dbstore.EmailSettings
	if s.Workspaces != nil && ref.WorkspaceID != "" {
		if st, err := s.Workspaces.EmailSettings(ctx, ref.WorkspaceID); err == nil {
			ws = st
		}
	}
	// Issue #177/#453: resolve per-form override -> workspace -> default. A
	// per-form override wins field-by-field: non-empty subject/body/merchant
	// email, or an explicit on/off toggle. A form with no override at all
	// inherits the workspace settings unchanged.
	var formSt *dbstore.FormEmailSettings
	if s.Forms != nil && ref.FormID != "" {
		if st, err := s.Forms.FormEmailSettings(ctx, ref.FormID); err == nil {
			formSt = st
		}
	}
	// Language consistency: the BUYER-facing language is
	// form.config.lang ?? workspace_settings.default_lang ?? en — never the
	// merchant's personal users.lang. A guest buyer has no profile to consult,
	// which is why this is the only correct axis here. Resolved before the
	// settings so an inherited (never-customised) system template is rendered in
	// the buyer's language rather than the Malay literal the DB defaults carry.
	lang := dbstore.EmailLang(ref.FormLang, ref.WorkspaceLang)
	resolved := dbstore.ResolveFormEmailSettings(formSt, ws, lang)
	subject, body, html := resolved.ReceiptSubject, resolved.ReceiptBody, resolved.HTML
	receiptEnabled := resolved.ReceiptEnabled
	notifyMerchant := resolved.NotifyMerchant
	merchantEmail := resolved.MerchantNotifyEmail

	subj, rendered := renderReceipt(ref, subject, body, lang, html)

	// Issue #199: background path — enqueue the fully-rendered jobs and return.
	if s.EmailQueue != nil {
		if receiptEnabled {
			// Generate the PDF here (cheap local work) so the worker only does
			// the SES call. A PDF failure falls back to a text-only receipt.
			var pdf []byte
			if p, err := s.ReceiptPDF(ref); err == nil && len(p) > 0 {
				pdf = p
			}
			if err := s.enqueueEmailJob(ctx, ref.AccountID, emailqueue.Job{
				Type: "receipt", To: ref.BuyerEmail,
				Subject: subj, Body: rendered, Lang: lang,
				AccountID: ref.WorkspaceID, SaleRef: ref.RefCode, SaleID: ref.SaleID,
				PDFFilename: receiptAttachmentName(ref.RefCode), PDFData: pdf,
			}); err != nil {
				slog.Error("receipt_enqueue_failed", "ref", ref.RefCode, "to", ref.BuyerEmail,
					"err", err, "attempts", enqueueAttempts, "recorded", s.EmailEvents != nil)
			}
		}
		if notifyMerchant {
			to := s.resolveMerchantEmail(ctx, ref, merchantEmail)
			if to == "" {
				slog.Warn("receipt_merchant_notify_no_recipient", "ref", ref.RefCode)
			} else if err := s.enqueueEmailJob(ctx, ref.AccountID, emailqueue.Job{
				Type: "merchant_notify", To: to,
				Subject: emailtext.Phrase(lang, emailtext.PhraseMerchantPrefix) + subj,
				Body:    rendered, Lang: lang,
				AccountID: ref.WorkspaceID, SaleRef: ref.RefCode,
			}); err != nil {
				slog.Error("receipt_merchant_notify_enqueue_failed", "ref", ref.RefCode, "to", to,
					"err", err, "attempts", enqueueAttempts, "recorded", s.EmailEvents != nil)
			}
		}
		return
	}

	// Synchronous path (bare-mode tests / no queue wired).
	persistReceiptMessageID := func(messageID string) {
		if messageID == "" || ref.SaleID == "" || s.Sales == nil {
			return
		}
		if err := s.Sales.SetReceiptMessageID(ctx, ref.SaleID, messageID); err != nil {
			slog.Warn("receipt_message_id_store_failed", "ref", ref.RefCode, "err", err)
		}
	}

	// Send the buyer receipt only when enabled.
	if receiptEnabled {
		s.sendReceipt(ctx, ref, lang, subj, rendered, html, persistReceiptMessageID)
	}

	// Also notify the merchant (copy or "new sale" notification).
	if notifyMerchant {
		s.notifyMerchant(ctx, ref, lang, subj, rendered, html, merchantEmail)
	}
}

// resolveMerchantEmail returns the merchant notification recipient: the
// configured merchant_notify_email, else the workspace owner's account email.
func (s *Service) resolveMerchantEmail(ctx context.Context, ref SaleRef, merchantEmail string) string {
	to := strings.TrimSpace(merchantEmail)
	if to == "" {
		if s.Workspaces != nil && ref.WorkspaceID != "" {
			if owner, err := s.Workspaces.ByID(ctx, ref.WorkspaceID); err == nil && owner != nil {
				if s.Users != nil {
					if u, uerr := s.Users.ByID(ctx, owner.Owner); uerr == nil && u != nil {
						to = u.Email
					}
				}
			}
		}
	}
	return to
}

// receiptAttachmentName is the file name of the receipt PDF in the buyer's mail
// client. It is a token rather than prose, but it is ENGLISH like the document
// it names ("receipt-", not "resit-"): the file name is the part of the receipt
// a buyer sees before opening anything, and a reader who does not know Malay
// reads "resit-JF-1234.pdf" as an unknown word at exactly the moment they are
// looking for their payment proof. Card t_129664c9.
func receiptAttachmentName(refCode string) string {
	return "receipt-" + refCode + ".pdf"
}

// sendReceipt delivers the buyer receipt (with PDF attachment when available).
//
// The recipient was already checked at (*email.Sender).SendReceipt — the
// pre-send choke point — so a suppression hit here arrives as
// email.ErrSuppressed. It is logged and swallowed: the receipt is correctly not
// sent, no MessageID is persisted, and the payment flow is unaffected.
func (s *Service) sendReceipt(ctx context.Context, ref SaleRef, lang, subject, body string, html bool, persistReceiptMessageID func(string)) {
	if pdf, err := s.ReceiptPDF(ref); err == nil && len(pdf) > 0 {
		messageID, err := s.Receipts.Sender.SendReceiptWithAttachment(ref.BuyerEmail, subject, body, lang,
			receiptAttachmentName(ref.RefCode), pdf)
		if err != nil {
			if errors.Is(err, email.ErrSuppressed) {
				slog.Warn("receipt_suppressed_skipped", "ref", ref.RefCode, "to", ref.BuyerEmail)
				return
			}
			slog.Warn("receipt_send_failed", "ref", ref.RefCode, "err", err)
		} else {
			slog.Info("receipt_sent_pdf", "ref", ref.RefCode, "to", ref.BuyerEmail, "lang", lang)
			persistReceiptMessageID(messageID)
		}
		return
	}
	// fallback: text-only
	//
	// Reachability note (issue #601): ReceiptPDF has no failure path a caller can
	// provoke — it renders from a fixed template and returned a valid PDF for
	// every SaleRef tried, including the zero value — so in practice a receipt
	// with a buyer address always takes the attachment branch above. This branch
	// is kept as the guard-of-last-resort for a ref whose PDF cannot be built,
	// and it carries the same suppression handling so it cannot become a silent
	// hole if that ever changes. It is deliberately NOT covered by a test: no
	// input reaches it, so a test here would assert behaviour nobody can produce.
	messageID, err := s.Receipts.Sender.SendReceipt(ref.BuyerEmail, subject, body, lang)
	if err != nil {
		if errors.Is(err, email.ErrSuppressed) {
			slog.Warn("receipt_suppressed_skipped", "ref", ref.RefCode, "to", ref.BuyerEmail)
			return
		}
		slog.Warn("receipt_send_failed", "ref", ref.RefCode, "err", err)
	} else {
		slog.Info("receipt_sent", "ref", ref.RefCode, "to", ref.BuyerEmail, "lang", lang)
		persistReceiptMessageID(messageID)
	}
}

// notifyMerchant sends a copy of the receipt (or a "new sale" notification)
// to the merchant. The recipient is merchant_notify_email when set, else the
// workspace owner's account email.
//
// Suppression APPLIES to the merchant too. This deliberately reverses the
// earlier exemption ("the merchant opted in, always notify"): a merchant whose
// mailbox hard-bounces keeps generating bounces from the same sending identity
// that delivers buyer receipts, so exempting them is what lets one bad mailbox
// damage deliverability for every buyer. A merchant who stops receiving
// notifications changes the address in workspace email settings, which is the
// action the notification itself enables.
func (s *Service) notifyMerchant(ctx context.Context, ref SaleRef, lang, subject, body string, html bool, merchantEmail string) {
	to := strings.TrimSpace(merchantEmail)
	if to == "" {
		// Resolve the merchant's email from the workspace owner's account.
		if s.Workspaces != nil && ref.WorkspaceID != "" {
			if owner, err := s.Workspaces.ByID(ctx, ref.WorkspaceID); err == nil && owner != nil {
				if s.Users != nil {
					if u, uerr := s.Users.ByID(ctx, owner.Owner); uerr == nil && u != nil {
						to = u.Email
					}
				}
			}
		}
	}
	if to == "" {
		slog.Warn("receipt_merchant_notify_no_recipient", "ref", ref.RefCode)
		return
	}
	// Prefix the subject so the merchant can tell it apart from the buyer copy.
	// The prefix is in the BUYER-facing language: this is the merchant's copy OF
	// the buyer's receipt, and it deliberately does not switch to his personal
	// users.lang half-way through the message.
	merchantSubject := emailtext.Phrase(lang, emailtext.PhraseMerchantPrefix) + subject
	if err := s.Receipts.Sender.Send(to, merchantSubject, body, lang); err != nil {
		if errors.Is(err, email.ErrSuppressed) {
			slog.Warn("receipt_merchant_suppressed_skipped", "ref", ref.RefCode, "to", to)
			return
		}
		slog.Warn("receipt_merchant_notify_failed", "ref", ref.RefCode, "to", to, "err", err)
	} else {
		slog.Info("receipt_merchant_notify_sent", "ref", ref.RefCode, "to", to, "lang", lang)
	}
}
