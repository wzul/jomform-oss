// SES notification endpoint (issue #56): receives SNS Bounce/Complaint
// notifications subscribed to the SES configuration set, verifies the SNS
// signature, and records permanent bounces / complaints into the suppression
// list, flagging the owning user unverified. Issue #93 extends it to also
// record Delivered/Open/Click events into email_delivery_events (informational
// only — no suppression/breaker action). No unauthenticated write: a request
// without a valid SNS signature is rejected.
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/email"
)

// sesSNSMessage is the inner "Message" payload SNS delivers (not the envelope).
type sesSNSMessage struct {
	EventType string     `json:"eventType"`
	Bounce    *sesBounce `json:"bounce,omitempty"`
	Complaint *struct {
		ComplainedRecipients []sesRecipient `json:"complainedRecipients"`
	} `json:"complaint,omitempty"`
	// Issue #93: informational delivery events (no suppression/breaker action).
	Delivery *struct {
		Recipients []string `json:"recipients"`
	} `json:"delivery,omitempty"`
	Open *struct {
		Recipients []string `json:"recipients"`
		IPAddress  string   `json:"ipAddress"`
		UserAgent  string   `json:"userAgent"`
	} `json:"open,omitempty"`
	Click *struct {
		Recipients []string `json:"recipients"`
		Link       string   `json:"link"`
	} `json:"click,omitempty"`
	Mail struct {
		Destination []string `json:"destination"`
		// MessageID is the SES message id of the original email (issue #106).
		// Open/Click/Delivered events carry it; we use it to correlate the
		// event back to the sale that sent the receipt.
		MessageID string `json:"messageId"`
	} `json:"mail"`
}

type sesBounce struct {
	BounceType        string         `json:"bounceType"`    // Permanent | Transient
	BounceSubType     string         `json:"bounceSubType"` // General | NoEmail | Suppressed | OnAccountSuppressionList | UnsubscribedRecipient | …
	BouncedRecipients []sesRecipient `json:"bouncedRecipients"`
}

// Soft-bounce rule (issue #601). SES retries a transient bounce internally
// before emitting the event, so one Transient event is not worth suppressing —
// but repeated transient bounces for the same mailbox mean it is effectively
// dead, and continuing to mail it only damages the sending identity. Suppress
// on the third Transient bounce for the same address inside a rolling 7 days.
const (
	softBounceThreshold = 3
	softBounceWindow    = 7 * 24 * time.Hour
)

type sesRecipient struct {
	EmailAddress string `json:"emailAddress"`
}

// suppressionWriter records a suppression + flags the user unverified.
type suppressionWriter struct {
	Suppressions *dbstore.SuppressionStore
	Users        *dbstore.UserStore
	Breaker      *email.Breaker
	// Issue #92: SES delivery-event log (bounce/complaint). Nil → skip write.
	EmailDeliveryEvents *dbstore.EmailDeliveryEventStore
	// Issue #106: sale lookup by receipt MessageID, to correlate Open/Click/
	// Delivered events to a specific sale. Nil → events recorded without a
	// sale_ref.
	Sales *dbstore.SaleStore
}

// recordDeliveryEvent appends a bounce/complaint row to email_delivery_events
// (issue #92), in addition to the suppression write. saleRef is the sale's
// ref_code this event correlates to (issue #106); empty → NULL. Best-effort: a
// nil store or a write error is logged, never fatal to the suppression path.
//
// The address is lower-cased and trimmed before the insert. The column is plain
// TEXT (no CITEXT), so without this the same mailbox could be logged as
// "Buyer@Example.com" by one SES event and "buyer@example.com" by the next, and
// any per-address query — the soft-bounce tally in handleTransientBounce, and
// the store's own CountRecentBounces — would undercount a real repeat offender
// and let a dead mailbox keep being mailed.
func (sw *suppressionWriter) recordDeliveryEvent(ctx context.Context, email, eventType, bounceType, saleRef string, detail map[string]any) {
	if sw.EmailDeliveryEvents == nil {
		return
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if err := sw.EmailDeliveryEvents.Record(ctx, "", email, eventType, bounceType, saleRef, detail); err != nil {
		slog.Error("ses_delivery_event_record", "err", err, "email", email, "event", eventType)
	}
}

// correlateSaleRef resolves the sale_ref for an SES event (issue #106). SES
// Open/Click/Delivered events carry the original message's MessageID in
// mail.messageId; we look up the sale whose receipt produced that message and
// return its ref_code. Returns "" when the message id is absent or no sale
// matches (system mail, or a receipt sent before this feature shipped).
func (sw *suppressionWriter) correlateSaleRef(ctx context.Context, messageID string) string {
	if messageID == "" || sw.Sales == nil {
		return ""
	}
	sale, err := sw.Sales.ByReceiptMessageID(ctx, messageID)
	if err != nil {
		return ""
	}
	return sale.RefCode
}

// handleTransientBounce applies the soft-bounce rule to one Transient bounce.
//
// SES already retries a transient bounce internally before it emits the event,
// so a single one is not suppressed: the mailbox may simply be full or the
// receiving server briefly unavailable, and suppressing on the first event would
// cut off a recoverable address. The signal is *repetition* — three Transient
// bounces for the same address inside a rolling 7 days means the mailbox is
// effectively dead, and continuing to mail it only spends quota and damages the
// sending identity, so on the third the address is suppressed like a hard
// bounce (reason hard_bounce, note carries the tally).
//
// Counting: the caller has ALREADY recorded the current event via
// recordDeliveryEvent, so the count below includes the event that triggered the
// check and no off-by-one adjustment is applied — the third event counts 3 and
// fires. (Deliberately not "count before writing" plus a +1: counting what is
// actually stored is the number an operator can reproduce by hand with a SELECT
// over email_delivery_events.) With no event store wired there is nothing to
// count, so no suppression decision can be made and the bounce is only counted
// towards the breaker.
//
// Transient bounces always also bump the breaker counter (unchanged #56
// behaviour) so an anomalous transient-bounce storm stays visible.
func (sw *suppressionWriter) handleTransientBounce(ctx context.Context, addr string) {
	if sw.Breaker != nil {
		_ = sw.Breaker.RecordBounceCount(ctx, 1)
	}
	if sw.EmailDeliveryEvents == nil || sw.Suppressions == nil {
		return
	}
	n, err := sw.EmailDeliveryEvents.CountRecentBounces(ctx, addr,
		[]string{"bounce"}, "Transient", time.Now().Add(-softBounceWindow))
	if err != nil {
		slog.Error("ses_soft_bounce_count", "err", err, "email", addr)
		return
	}
	if n < softBounceThreshold {
		return
	}
	sw.suppress(ctx, addr, dbstore.SuppressionReasonHardBounce,
		fmt.Sprintf("repeated soft bounce (%d in 7d)", n), dbstore.SuppressionSourceBounce, false)
}

// suppress persists a suppression for one address and flips its user row
// (if any) back to unverified. Breaker bounce counts are only incremented for
// permanent bounces (they are what damage SES reputation).
//
// reason must be one of the canonical codes migration 000061's CHECK allows
// (dbstore.SuppressionReason*); note carries the provider detail (bounce
// sub-type, or the soft-bounce tally) that used to be crammed into the reason
// string. The write goes through SuppressionStore.Add, the one write path, so a
// repeat event refreshes the single row for that address instead of erroring.
//
// MarkUnverified only gates *auth* mail — the account's next login/verify
// attempt. It does not stop a buyer receipt or a merchant notification, which is
// exactly why the pre-send guard at (*email.Sender).SendReceipt is required for
// those paths.
func (sw *suppressionWriter) suppress(ctx context.Context, addr string, reason, note string, source dbstore.SuppressionSource, countBounce bool) {
	addr = strings.ToLower(strings.TrimSpace(addr))
	if addr == "" {
		return
	}
	if _, err := sw.Suppressions.Add(ctx, addr, reason, source, note, ""); err != nil {
		slog.Error("ses_suppress_upsert", "err", err, "email", addr)
		return
	}
	if sw.Users != nil {
		if err := sw.Users.MarkUnverified(ctx, addr); err != nil {
			slog.Error("ses_suppress_mark_unverified", "err", err, "email", addr)
		}
	}
	if countBounce && sw.Breaker != nil {
		if err := sw.Breaker.RecordBounceCount(ctx, 1); err != nil {
			slog.Warn("ses_suppress_bounce_count", "err", err)
		}
	}
	slog.Warn("email_suppressed", "email", addr, "reason", reason, "source", source, "note", note)
}

// handleSESNotification processes a verified SNS notification.
func (s *Server) handleSESNotification(w http.ResponseWriter, r *http.Request) {
	sw := &suppressionWriter{
		Suppressions: s.deps.Suppressions, Users: s.deps.Users, Breaker: s.deps.Breaker,
		EmailDeliveryEvents: s.deps.EmailDeliveryEvents,
		// Issue #106: correlate Open/Click/Delivered to a sale via MessageID.
		Sales: s.deps.Sales,
	}
	if sw.Suppressions == nil {
		writeErr(w, http.StatusServiceUnavailable, "suppression store not configured")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad body")
		return
	}

	// 1. Verify the SNS signature BEFORE trusting any field.
	//    SNS signs the full envelope; a missing/invalid signature is rejected.
	if err := email.VerifySNSNotification(raw, nil); err != nil {
		slog.Warn("ses_sns_signature_rejected", "err", err)
		writeErr(w, http.StatusUnauthorized, "sns signature tidak sah")
		return
	}

	// 2. Extract the inner Message payload.
	var env email.SNSNotification
	if err := json.Unmarshal(raw, &env); err != nil {
		writeErr(w, http.StatusBadRequest, "sns payload tidak sah")
		return
	}
	// Auto-confirm SNS HTTPS subscriptions (issue #76): SNS POSTs a
	// SubscriptionConfirmation with a SubscribeURL; hitting that URL confirms
	// the subscription so Bounce/Complaint notifications start flowing. The
	// signature was already verified above, so the SubscribeURL is trusted.
	if env.Type == "SubscriptionConfirmation" {
		if env.SubscribeURL != "" {
			resp, err := http.Get(env.SubscribeURL)
			if err != nil {
				slog.Warn("ses_sns_subscribe_confirm_failed", "err", err)
				writeErr(w, http.StatusBadGateway, "sns subscribe confirm gagal")
				return
			}
			resp.Body.Close()
			slog.Info("ses_sns_subscription_confirmed", "topic", env.TopicArn)
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "confirmed"})
		return
	}
	// Ignore other non-notification types.
	if env.Type != "Notification" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}
	var msg sesSNSMessage
	if err := json.Unmarshal([]byte(env.Message), &msg); err != nil {
		slog.Warn("ses_sns_message_unparseable", "err", err, "type", env.Type)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}

	// 3. Record actionable feedback. Permanent bounce or complaint → suppress.
	//    Delivered/Open/Click are informational (issue #93) — recorded only.
	if !sw.handleEvent(r.Context(), &msg) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleEvent applies one SES event. It returns false when the event type is
// unknown or its payload is missing (caller responds "ignored"). Bounce and
// Complaint suppress + record; Delivered/Open/Click (issue #93) are
// informational and only record into email_delivery_events.
func (sw *suppressionWriter) handleEvent(ctx context.Context, msg *sesSNSMessage) bool {
	switch msg.EventType {
	case "Bounce":
		if msg.Bounce == nil {
			return false
		}
		permanent := msg.Bounce.BounceType == "Permanent"
		for _, rec := range msg.Bounce.BouncedRecipients {
			// Issue #92: record the bounce into email_delivery_events (in
			// addition to the suppression write below).
			sw.recordDeliveryEvent(ctx, rec.EmailAddress, "bounce", msg.Bounce.BounceType, "", map[string]any{
				"bounceType": msg.Bounce.BounceType,
			})
			if !permanent {
				sw.handleTransientBounce(ctx, rec.EmailAddress)
				continue
			}
			// A permanent bounce suppresses immediately. The sub-type decides
			// the note, not the decision: every Permanent variant SES emits
			// means this mailbox will not accept our mail.
			//
			//   General / NoEmail            mailbox does not exist
			//   Suppressed                   on SES's own global list
			//   OnAccountSuppressionList     on this account's list; SES accepts
			//                                the send, never attempts delivery, and
			//                                it still counts against the daily
			//                                quota while being excluded from
			//                                BounceRate — so ingesting it is the
			//                                only way to stop burning quota
			//   UnsubscribedRecipient        delivery not attempted; the
			//                                recipient asked not to be mailed
			note := msg.Bounce.BounceSubType
			if note == "" {
				note = msg.Bounce.BounceType
			}
			if msg.Bounce.BounceSubType == "UnsubscribedRecipient" {
				note = "unsubscribed"
			}
			sw.suppress(ctx, rec.EmailAddress, dbstore.SuppressionReasonHardBounce, note,
				dbstore.SuppressionSourceBounce, true)
		}
	case "Complaint":
		if msg.Complaint == nil {
			return false
		}
		for _, rec := range msg.Complaint.ComplainedRecipients {
			// Issue #92: record the complaint into email_delivery_events.
			sw.recordDeliveryEvent(ctx, rec.EmailAddress, "complaint", "", "", nil)
			// A complaint suppresses immediately — a recipient who presses
			// "report spam" must never be mailed again.
			sw.suppress(ctx, rec.EmailAddress, dbstore.SuppressionReasonComplaint, "",
				dbstore.SuppressionSourceComplaint, false)
		}
	case "Delivery", "Delivered":
		// Issue #93: informational — record delivery, no suppression/breaker.
		// SES emits eventType "Delivery" (not "Delivered"); accept both so a
		// Delivery event is never silently dropped to the default branch.
		if msg.Delivery == nil {
			return false
		}
		// Issue #106: correlate to the sale via the receipt's MessageID.
		saleRef := sw.correlateSaleRef(ctx, msg.Mail.MessageID)
		for _, addr := range msg.Delivery.Recipients {
			sw.recordDeliveryEvent(ctx, addr, "delivered", "", saleRef, nil)
		}
	case "Open":
		// Issue #93: informational — record open (+ user agent / IP), no
		// suppression/breaker. SES Open events carry the recipient in
		// mail.destination (the open object has no recipients array), so fall
		// back to it when open.recipients is absent.
		if msg.Open == nil {
			return false
		}
		detail := map[string]any{}
		if msg.Open.UserAgent != "" {
			detail["userAgent"] = msg.Open.UserAgent
		}
		if msg.Open.IPAddress != "" {
			detail["ipAddress"] = msg.Open.IPAddress
		}
		recipients := msg.Open.Recipients
		if len(recipients) == 0 {
			recipients = msg.Mail.Destination
		}
		// Issue #106: correlate to the sale via the receipt's MessageID.
		saleRef := sw.correlateSaleRef(ctx, msg.Mail.MessageID)
		for _, addr := range recipients {
			sw.recordDeliveryEvent(ctx, addr, "open", "", saleRef, detail)
		}
	case "Click":
		// Issue #93: informational — record click (+ link), no suppression/breaker.
		// As with Open, fall back to mail.destination when click.recipients is
		// absent (the standard SES Click shape has no recipients array).
		if msg.Click == nil {
			return false
		}
		detail := map[string]any{}
		if msg.Click.Link != "" {
			detail["link"] = msg.Click.Link
		}
		recipients := msg.Click.Recipients
		if len(recipients) == 0 {
			recipients = msg.Mail.Destination
		}
		// Issue #106: correlate to the sale via the receipt's MessageID.
		saleRef := sw.correlateSaleRef(ctx, msg.Mail.MessageID)
		for _, addr := range recipients {
			sw.recordDeliveryEvent(ctx, addr, "click", "", saleRef, detail)
		}
	default:
		return false
	}
	return true
}
