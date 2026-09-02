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
	"io"
	"log/slog"
	"net/http"
	"strings"

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
	} `json:"mail"`
}

type sesBounce struct {
	BounceType        string         `json:"bounceType"` // Permanent | Transient
	BouncedRecipients []sesRecipient `json:"bouncedRecipients"`
}

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
}

// recordDeliveryEvent appends a bounce/complaint row to email_delivery_events
// (issue #92), in addition to the suppression write. Best-effort: a nil store
// or a write error is logged, never fatal to the suppression path.
func (sw *suppressionWriter) recordDeliveryEvent(ctx context.Context, email, eventType, bounceType string, detail map[string]any) {
	if sw.EmailDeliveryEvents == nil {
		return
	}
	if err := sw.EmailDeliveryEvents.Record(ctx, "", email, eventType, bounceType, detail); err != nil {
		slog.Error("ses_delivery_event_record", "err", err, "email", email, "event", eventType)
	}
}

// suppress persists a suppression for one address and flips its user row
// (if any) back to unverified. Breaker bounce counts are only incremented for
// permanent bounces (they are what damage SES reputation).
func (sw *suppressionWriter) suppress(ctx context.Context, addr string, reason string, source dbstore.SuppressionSource, countBounce bool) {
	addr = strings.ToLower(strings.TrimSpace(addr))
	if addr == "" {
		return
	}
	if _, err := sw.Suppressions.Upsert(ctx, addr, reason, source); err != nil {
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
	slog.Warn("email_suppressed", "email", addr, "reason", reason, "source", source)
}

// handleSESNotification processes a verified SNS notification.
func (s *Server) handleSESNotification(w http.ResponseWriter, r *http.Request) {
	sw := &suppressionWriter{
		Suppressions: s.deps.Suppressions, Users: s.deps.Users, Breaker: s.deps.Breaker,
		EmailDeliveryEvents: s.deps.EmailDeliveryEvents,
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
			sw.recordDeliveryEvent(ctx, rec.EmailAddress, "bounce", msg.Bounce.BounceType, map[string]any{
				"bounceType": msg.Bounce.BounceType,
			})
			if !permanent {
				// Transient bounces are not suppressed, but still counted so the
				// breaker can react to an anomalous transient-bounce storm.
				if sw.Breaker != nil {
					_ = sw.Breaker.RecordBounceCount(ctx, 1)
				}
				continue
			}
			sw.suppress(ctx, rec.EmailAddress, "permanent bounce ("+msg.Bounce.BounceType+")", dbstore.SuppressionSourceBounce, true)
		}
	case "Complaint":
		if msg.Complaint == nil {
			return false
		}
		for _, rec := range msg.Complaint.ComplainedRecipients {
			// Issue #92: record the complaint into email_delivery_events.
			sw.recordDeliveryEvent(ctx, rec.EmailAddress, "complaint", "", nil)
			sw.suppress(ctx, rec.EmailAddress, "complaint", dbstore.SuppressionSourceComplaint, false)
		}
	case "Delivered":
		// Issue #93: informational — record delivery, no suppression/breaker.
		if msg.Delivery == nil {
			return false
		}
		for _, addr := range msg.Delivery.Recipients {
			sw.recordDeliveryEvent(ctx, addr, "delivered", "", nil)
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
		for _, addr := range recipients {
			sw.recordDeliveryEvent(ctx, addr, "open", "", detail)
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
		for _, addr := range recipients {
			sw.recordDeliveryEvent(ctx, addr, "click", "", detail)
		}
	default:
		return false
	}
	return true
}
