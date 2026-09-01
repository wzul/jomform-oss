// Webhook Ed25519 signing: keypair per account, generated on first use.
// Headers: X-JomForm-Signature: sha256=<hex>           (HMAC, existing)
//
//	X-JomForm-Signature-Ed25519: <base64 sig>   (new, same body)
//	X-JomForm-Key-Id: <account-uuid>            (key lookup hint)
package httpapi

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
)

// ensureSigningKey loads (or creates) the account's Ed25519 keypair.
func ensureSigningKey(ctx context.Context, h *RESTHandlers, accountID string) (ed25519.PublicKey, ed25519.PrivateKey) {
	pubB64, privB64, err := h.Accounts.SigningKeys(ctx, accountID)
	if err != nil || pubB64 == "" || privB64 == "" {
		_, privKey, gerr := ed25519.GenerateKey(rand.Reader)
		if gerr != nil {
			slog.Error("ed25519_gen", "err", gerr)
			return nil, nil
		}
		pubB64 = base64.StdEncoding.EncodeToString(privKey.Public().(ed25519.PublicKey))
		privB64 = base64.StdEncoding.EncodeToString(privKey)
		if serr := h.Accounts.SaveSigningKeys(ctx, accountID, pubB64, privB64); serr != nil {
			slog.Error("ed25519_save", "err", serr)
			return nil, nil
		}
		slog.Info("ed25519_created", "account", accountID)
	}
	pubB, _ := base64.StdEncoding.DecodeString(pubB64)
	privB, _ := base64.StdEncoding.DecodeString(privB64)
	return ed25519.PublicKey(pubB), ed25519.PrivateKey(privB)
}

// SigningKeyResponse is served to the settings UI (+ MCP tool).
type SigningKeyResponse struct {
	KeyID     string `json:"key_id"`
	PublicKey string `json:"public_key"`
	Alg       string `json:"alg"`
}

// signingKeyInfo for the settings UI / MCP.
func signingKeyInfo(ctx context.Context, h *RESTHandlers, accountID string) *SigningKeyResponse {
	pub, _ := ensureSigningKey(ctx, h, accountID)
	if pub == nil {
		return nil
	}
	return &SigningKeyResponse{
		KeyID:     accountID,
		PublicKey: base64.StdEncoding.EncodeToString(pub),
		Alg:       "ed25519",
	}
}

var _ = json.Marshal

func (h *RESTHandlers) getWebhookKey(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	info := signingKeyInfo(r.Context(), h, actx.Account.ID)
	writeJSON(w, http.StatusOK, info)
}
