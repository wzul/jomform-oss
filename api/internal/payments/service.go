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
)

// Service wires CHIP client + stores for payment operations.
type Service struct {
	Chip       *chip.Client
	Accounts   *dbstore.AccountStore
	Sales      *dbstore.SaleStore
	Subs       *dbstore.SubscriptionStore
	DecryptKey []byte // ENCRYPTION_KEY — used to decrypt per-account CHIP keys
	BaseURL    string // app public base URL for redirects/callbacks
	Receipts   *Receipts
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
