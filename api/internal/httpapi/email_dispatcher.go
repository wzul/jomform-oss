// Background email worker dispatcher (issue #199).
//
// The emailqueue.Worker polls the Valkey-backed email queue and calls this
// dispatcher's Send for each job. The dispatcher routes each job type to the
// appropriate SES sender:
//
//   - receipt / merchant_notify / form_notification → the payments Service's
//     receipt sender (with PDF attachment when present for receipts),
//     persisting the SES MessageID back onto the sale (issue #106) for
//     open/click correlation.
//   - verify / magic_link / password_reset / email2fa / 2fa_active / invite /
//     connect → the shared auth mailer, recording the SES send metric + breaker
//     counter exactly as the synchronous path did.
//   - spam_judge / asset_scan → the background AI judge / asset gate.
//
// Every job type the repo enqueues has a case below; a type with no case would
// be mail the dispatcher discards, so `default:` fails loudly instead of
// returning nil, and email_dispatcher_jobtype_test.go asserts that each `Type:`
// literal enqueued anywhere in the repo has a matching case here.
//
// The dispatcher is wired in cmd/jomform-api and is nil-safe: a nil field means
// that job type is dropped with a warning (should not happen in production).
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/wzul/jomform/api/internal/assetgate"
	"github.com/wzul/jomform/api/internal/email"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/payments"
	"github.com/wzul/jomform/api/internal/spamjudge"
)

// errUnknownJobType marks a queued job the dispatcher has no case for. It is
// deliberately an error rather than a logged no-op: a silent nil discard means
// the message never retries and never dead-letters, so mail disappears with no
// trace anywhere. On the worker's ladder a missing case surfaces as retries and
// then a dead-letter entry, which is visible and replayable. No type the repo
// actually enqueues may reach it (email_dispatcher_jobtype_test.go asserts that).
var errUnknownJobType = errors.New("emailq: unknown job type")

// EmailDispatcher routes queued email jobs to the right sender. It implements
// emailqueue.Handler.
type EmailDispatcher struct {
	// AuthMailer delivers auth/invite emails (verify, magic link, reset, 2FA,
	// invite). Nil → those job types are dropped.
	AuthMailer VerifyMailer
	// Breaker records verification sends (issue #56). Nil-safe.
	Breaker *email.Breaker
	// Pay is the payments Service whose Receipts.Sender delivers receipts and
	// whose Sales store persists the SES MessageID (issue #106).
	Pay *payments.Service
	// SpamJudge (issue #245) classifies spam_judge jobs via the background AI
	// judge. Nil → spam_judge jobs are dropped.
	SpamJudge *spamjudge.Handler
	// AssetScan (issue #364) classifies asset_scan jobs via the deterministic
	// + LLM scan gate. Nil → asset_scan jobs are dropped (leaves assets in
	// 'pending' — never served).
	AssetScan *assetgate.Handler
}

// Send delivers one queued email job. A returned error triggers the worker's
// retry-with-backoff ladder.
//
// A suppression hit is NOT one of those errors. The pre-send guard at
// (*email.Sender).SendReceipt returns email.ErrSuppressed when the recipient is
// on the suppression list, and this dispatcher maps it to a logged skip and a
// nil return: retrying can never succeed, and a non-nil error would push a job
// that is correctly being dropped onto the retry ladder and eventually into the
// dead-letter list, where it would look like a delivery failure forever.
func (d *EmailDispatcher) Send(ctx context.Context, job emailqueue.Job) error {
	switch job.Type {
	case "receipt":
		return d.sendReceipt(ctx, job)
	case "merchant_notify":
		return d.sendMerchantNotify(ctx, job)
	case "form_notification":
		return d.sendFormNotification(ctx, job)
	// connect (issue #302, the social-login email-ownership link) rides the
	// shared auth mailer exactly as its synchronous branch does — same sender,
	// same sent metric, same breaker.
	case "verify", "magic_link", "password_reset", "email2fa", "2fa_active", "invite", "connect":
		return d.sendAuth(ctx, job)
	case spamjudge.JobType:
		if d.SpamJudge == nil {
			slog.Warn("emailq_spam_judge_no_handler", "account", job.AccountID)
			return nil
		}
		return d.SpamJudge.Send(ctx, job)
	case assetgate.JobType:
		if d.AssetScan == nil {
			slog.Warn("emailq_asset_scan_no_handler", "account", job.AccountID)
			return nil
		}
		return d.AssetScan.Send(ctx, job)
	default:
		// Fail loudly. Returning nil here (the pre-fix behaviour) discards the
		// job with no error, so nothing retries and nothing dead-letters: the
		// message is simply gone. An error puts it on the worker's retry ladder
		// and then the dead-letter list (#547), where it is visible and
		// replayable. email_dispatcher_jobtype_test.go keeps this branch
		// unreachable for every type the repo actually enqueues.
		slog.Error("emailq_unknown_type", "type", job.Type)
		recordError("emailq_unknown_type")
		return fmt.Errorf("%w: %q", errUnknownJobType, job.Type)
	}
}

// suppressionSkip reports whether err is the pre-send suppression sentinel, and
// if so logs the structured skip line for a queued job. It returns true when the
// caller must treat the job as successfully handled (nil error).
func (d *EmailDispatcher) suppressionSkip(job emailqueue.Job, err error) bool {
	if !errors.Is(err, email.ErrSuppressed) {
		return false
	}
	slog.Warn("emailq_skip_suppressed", "type", job.Type, "to", obfuscateEmail(job.To),
		"ref", job.SaleRef, "account", job.AccountID)
	recordSESSuppression(job.Type)
	return true
}

// sendReceipt delivers a buyer receipt (with PDF attachment when present) and
// persists the SES MessageID onto the sale for open/click correlation.
func (d *EmailDispatcher) sendReceipt(ctx context.Context, job emailqueue.Job) error {
	if d.Pay == nil || d.Pay.Receipts == nil || d.Pay.Receipts.Sender == nil {
		slog.Warn("emailq_receipt_no_sender", "to", job.To, "ref", job.SaleRef)
		return nil
	}
	var messageID string
	var err error
	if len(job.PDFData) > 0 {
		messageID, err = d.Pay.Receipts.Sender.SendReceiptWithAttachment(
			job.To, job.Subject, job.Body, job.PDFFilename, job.PDFData)
	} else {
		messageID, err = d.Pay.Receipts.Sender.SendReceipt(job.To, job.Subject, job.Body)
	}
	if err != nil {
		if d.suppressionSkip(job, err) {
			// Suppressed: the sale keeps its (absent) MessageID — persisting
			// nothing is correct, and the job is done.
			return nil
		}
		slog.Warn("emailq_receipt_send_failed", "ref", job.SaleRef, "to", job.To, "err", err)
		return err
	}
	slog.Info("emailq_receipt_sent", "ref", job.SaleRef, "to", job.To)
	// Persist the SES MessageID so SNS Open/Click/Delivered events correlate
	// back to this sale (issue #106). Best-effort.
	if messageID != "" && job.SaleID != "" && d.Pay.Sales != nil {
		if perr := d.Pay.Sales.SetReceiptMessageID(ctx, job.SaleID, messageID); perr != nil {
			slog.Warn("emailq_receipt_message_id_store_failed", "ref", job.SaleRef, "err", perr)
		}
	}
	return nil
}

// sendMerchantNotify delivers the merchant copy / "new sale" notification.
func (d *EmailDispatcher) sendMerchantNotify(ctx context.Context, job emailqueue.Job) error {
	if d.Pay == nil || d.Pay.Receipts == nil || d.Pay.Receipts.Sender == nil {
		slog.Warn("emailq_merchant_notify_no_sender", "to", job.To, "ref", job.SaleRef)
		return nil
	}
	if err := d.Pay.Receipts.Sender.Send(job.To, job.Subject, job.Body); err != nil {
		if d.suppressionSkip(job, err) {
			return nil
		}
		slog.Warn("emailq_merchant_notify_failed", "ref", job.SaleRef, "to", job.To, "err", err)
		return err
	}
	slog.Info("emailq_merchant_notify_sent", "ref", job.SaleRef, "to", job.To)
	return nil
}

// sendFormNotification delivers a per-form notification rule (issue #193) —
// the merchant-facing "someone paid / responded" alert evaluated by
// payments.Service.FireFormNotifications.
//
// It goes through pay.Receipts.Sender, the same sender the synchronous branch
// in payments.sendNotification uses, so the message passes the pre-send
// suppression choke point. Before this case existed the job type fell to the
// dispatcher's default branch and was discarded: the alert was never delivered
// and the choke point never saw it, so it was neither suppression-checked nor
// auditable. No SaleID is persisted — a form notification is not a receipt, so
// there is no sale to attach a SES MessageID to.
func (d *EmailDispatcher) sendFormNotification(ctx context.Context, job emailqueue.Job) error {
	if d.Pay == nil || d.Pay.Receipts == nil || d.Pay.Receipts.Sender == nil {
		slog.Warn("emailq_form_notification_no_sender", "to", job.To, "ref", job.SaleRef)
		return nil
	}
	if err := d.Pay.Receipts.Sender.Send(job.To, job.Subject, job.Body); err != nil {
		if d.suppressionSkip(job, err) {
			return nil
		}
		slog.Warn("emailq_form_notification_failed", "ref", job.SaleRef, "to", job.To, "err", err)
		return err
	}
	slog.Info("emailq_form_notification_sent", "ref", job.SaleRef, "to", job.To)
	return nil
}

// sendAuth delivers an auth/invite email via the shared mailer, recording the
// SES send metric + breaker counter (mirroring the synchronous path).
func (d *EmailDispatcher) sendAuth(ctx context.Context, job emailqueue.Job) error {
	if d.AuthMailer == nil {
		slog.Warn("emailq_auth_no_mailer", "type", job.Type, "to", job.To)
		return nil
	}
	if err := d.AuthMailer.Send(job.To, job.Subject, job.Body); err != nil {
		if d.suppressionSkip(job, err) {
			return nil
		}
		slog.Warn("emailq_auth_send_failed", "type", job.Type, "to", job.To, "err", err)
		return err
	}
	recordSESSend(job.Type)
	if d.Breaker != nil {
		if err := d.Breaker.RecordSend(ctx); err != nil {
			slog.Warn("emailq_breaker_record_send", "type", job.Type, "err", err)
		}
	}
	return nil
}
