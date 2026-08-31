package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"io"
)

// randRead and hexEncode isolate crypto imports in a tiny helper file.
func randRead(b []byte) (int, error) { return rand.Read(b) }

func hexEncode(b []byte) string { return hex.EncodeToString(b) }

var _ io.Reader = rand.Reader