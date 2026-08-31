// Package auth — session management backed by Valkey.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const SessionTTL = 14 * 24 * time.Hour

// Sessions stores browser sessions in Valkey.
type Sessions struct {
	rdb *goredis.Client
}

// NewSessions wires a Valkey client. go-redis only accepts redis:// /
// rediss:// URL schemes, so a valkey:// prefix (RESP-compatible, same wire
// protocol) is transparently rewritten before parsing.
func NewSessions(valkeyURL string) (*Sessions, error) {
	normalized := strings.TrimPrefix(valkeyURL, "valkey://")
	normalized = "redis://" + normalized
	opts, err := goredis.ParseURL(normalized)
	if err != nil {
		return nil, fmt.Errorf("parse valkey url: %w", err)
	}
	return &Sessions{rdb: goredis.NewClient(opts)}, nil
}

// sessionKey namespaces session tokens in the keyspace.
func sessionKey(token string) string { return "jf:session:" + token }

// Create stores userID under a fresh opaque token and returns it.
func (s *Sessions) Create(ctx context.Context, userID string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	if err := s.rdb.Set(ctx, sessionKey(token), userID, SessionTTL).Err(); err != nil {
		return "", fmt.Errorf("session create: %w", err)
	}
	return token, nil
}

// Get resolves a session token to its userID.
func (s *Sessions) Get(ctx context.Context, token string) (string, error) {
	userID, err := s.rdb.Get(ctx, sessionKey(token)).Result()
	if err != nil {
		return "", err
	}
	return userID, nil
}

// Destroy removes a session (logout).
func (s *Sessions) Destroy(ctx context.Context, token string) error {
	return s.rdb.Del(ctx, sessionKey(token)).Err()
}

// Ping verifies the Valkey connection for readiness probes.
func (s *Sessions) Ping(ctx context.Context) error {
	return s.rdb.Ping(ctx).Err()
}

// Close releases the connection pool.
func (s *Sessions) Close() error { return s.rdb.Close() }
