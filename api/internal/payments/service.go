// Package payments: sale lifecycle orchestration over CHIP + Postgres.
package payments

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/emailqueue"
)

// Service wires CHIP client + stores for payment operations.
type Service struct {
	Chip       *chip.Client
	Accounts   *dbstore.AccountStore
	Sales      *dbstore.SaleStore
	Subs       *dbstore.SubscriptionStore
	Products   *dbstore.ProductStore // reads subscription_charges for fixed billing counts
	DecryptKey []byte // ENCRYPTION_KEY — used to decrypt per-account CHIP keys
	BaseURL    string // app public base URL for redirects/callbacks
	Forms      FormGetter
	Spaces     *dbstore.SpaceStore
	// Users resolves the merchant's email for notify_merchant (issue #107).
	Users      *dbstore.UserStore
	Receipts   *Receipts
	// EmailQueue (issue #199): when non-nil, receipts + merchant notifications
	// are enqueued and sent by a background worker instead of synchronously in
	// the request handler. Nil (bare-mode tests) → synchronous send.
	EmailQueue *emailqueue.Queue
	// OnPaid (optional) fans outbound webhooks after a sale is paid.
	OnPaid func(ctx context.Context, accountID, event string, payload map[string]any)
}

// New wires a payments Service.
func New(chipClient *chip.Client, accounts *dbstore.AccountStore, sales *dbstore.SaleStore, subs *dbstore.SubscriptionStore, decryptKey []byte, baseURL string) *Service {
	return &Service{
		Chip: chipClient, Accounts: accounts, Sales: sales, Subs: subs,
		DecryptKey: decryptKey, BaseURL: baseURL,
	}
}

var (
	// ErrNotConfigured: the account has no CHIP keys set up.
	ErrNotConfigured = errors.New("chip belum dikonfigurasi akaun ini")
	// ErrNotFound: sale/reference does not exist.
	ErrNotFound = errors.New("jualan tidak dijumpai")
)

// accountSecrets decrypts the account's stored CHIP credentials.
func (s *Service) accountSecrets(ctx context.Context, accountID string) (brandID, secretKey string, err error) {
	acc, err := s.Accounts.ByID(ctx, accountID)
	if err != nil {
		return "", "", fmt.Errorf("account: %w", err)
	}
	if acc.ChipBrandID == "" || acc.ChipSecretKeyEnc == "" {
		return "", "", ErrNotConfigured
	}
	dec, err := cryptoutil.Decrypt(s.DecryptKey, acc.ChipSecretKeyEnc)
	if err != nil {
		return "", "", fmt.Errorf("decrypt chip key: %w", err)
	}
	return acc.ChipBrandID, string(dec), nil
}

// spaceCurrency resolves the currency of the space that owns an account
// (issue #185). Falls back to the account's own currency when the account has
// no linked space or the lookup fails. Used by the recurring-charge and
// update-card paths, which act on an account rather than a sale.
func (s *Service) spaceCurrency(ctx context.Context, accountID string) string {
	if s.Spaces != nil {
		if cur, err := s.Spaces.SpaceCurrencyByAccount(ctx, accountID); err == nil && cur != "" {
			return cur
		}
	}
	if acc, err := s.Accounts.ByID(ctx, accountID); err == nil && acc.Currency != "" {
		return acc.Currency
	}
	return "MYR"
}

// publicKey cache: CHIP returns a company-wide key per secret; cache by key hash.
var (
	pubKeyCache   = map[string]*rsa.PublicKey{}
	pubKeyCacheMu sync.Mutex
)

// CallbackPublicKey returns the CHIP verification public key for an account,
// fetched via GET /public_key/ and cached.
func (s *Service) CallbackPublicKey(ctx context.Context, accountID string) (*rsa.PublicKey, error) {
	acc, err := s.Accounts.ByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	dec, err := cryptoutil.Decrypt(s.DecryptKey, acc.ChipSecretKeyEnc)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(dec)
	cacheKey := hex.EncodeToString(sum[:])
	pubKeyCacheMu.Lock()
	defer pubKeyCacheMu.Unlock()
	if k, ok := pubKeyCache[cacheKey]; ok {
		return k, nil
	}
	pemStr, err := s.Chip.PublicKey(ctx, string(dec))
	if err != nil {
		return nil, err
	}
	k, err := chip.ParsePublicKey(pemStr)
	if err != nil {
		return nil, err
	}
	pubKeyCache[cacheKey] = k
	return k, nil
}

// FormGetter lets the payments service read per-form settings (redirects).
type FormGetter interface {
	GetByID(ctx context.Context, accountID, id string) (*dbstore.Form, error)
	// FormEmailSettings reads a form's per-form notification email override
	// (issue #177). Empty subject/body = fall back to space defaults.
	FormEmailSettings(ctx context.Context, formID string) (*dbstore.FormEmailSettings, error)
}
