// Background email worker dispatcher (issue #199).
//
// The emailqueue.Worker polls the Valkey-backed email queue and calls this
// dispatcher's Send for each job. The dispatcher routes each job type to the
// appropriate SES sender:
//
//   - receipt / merchant_notify → the payments Service's receipt sender (with
//     PDF attachment when present), persisting the SES MessageID back onto the
//     sale (issue #106) for open/click correlation.
//   - verify / magic_link / password_reset / email2fa / 2fa_active / invite →
//     the shared auth mailer, recording the SES send metric + breaker counter
//     exactly as the synchronous path did.
//
// The dispatcher is wired in cmd/jomform-api and is nil-safe: a nil field means
// that job type is dropped with a warning (should not happen in production).
package httpapi

import (
	"context"
	"log/slog"

	"github.com/wzul/jomform/api/internal/assetgate"
	"github.com/wzul/jomform/api/internal/email"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/payments"
	"github.com/wzul/jomform/api/internal/spamjudge"
)

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
func (d *EmailDispatcher) Send(ctx context.Context, job emailqueue.Job) error {
	switch job.Type {
	case "receipt":
		return d.sendReceipt(ctx, job)
	case "merchant_notify":
		return d.sendMerchantNotify(ctx, job)
	case "verify", "magic_link", "password_reset", "email2fa", "2fa_active", "invite":
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
		slog.Warn("emailq_unknown_type", "type", job.Type, "to", job.To)
		return nil
	}
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
		slog.Warn("emailq_merchant_notify_failed", "ref", job.SaleRef, "to", job.To, "err", err)
		return err
	}
	slog.Info("emailq_merchant_notify_sent", "ref", job.SaleRef, "to", job.To)
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
