// SES notification endpoint (issue #56): receives SNS Bounce/Complaint
// notifications subscribed to the SES configuration set, verifies the SNS
// signature, and records permanent bounces / complaints into the suppression
// list, flagging the owning user unverified. No unauthenticated write: a
// request without a valid SNS signature is rejected.
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
	switch msg.EventType {
	case "Bounce":
		if msg.Bounce == nil {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
			return
		}
		permanent := msg.Bounce.BounceType == "Permanent"
		for _, rec := range msg.Bounce.BouncedRecipients {
			if !permanent {
				// Transient bounces are not suppressed, but still counted so the
				// breaker can react to an anomalous transient-bounce storm.
				if sw.Breaker != nil {
					_ = sw.Breaker.RecordBounceCount(r.Context(), 1)
				}
				continue
			}
			sw.suppress(r.Context(), rec.EmailAddress, "permanent bounce ("+msg.Bounce.BounceType+")", dbstore.SuppressionSourceBounce, true)
		}
	case "Complaint":
		if msg.Complaint == nil {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
			return
		}
		for _, rec := range msg.Complaint.ComplainedRecipients {
			sw.suppress(r.Context(), rec.EmailAddress, "complaint", dbstore.SuppressionSourceComplaint, false)
		}
	default:
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
