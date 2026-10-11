package middleware

import "time"

// timeNow is swappable for deterministic tests (same convention as
// internal/auth/clock.go). RateLimiter reads the clock through it so a test can
// age a bucket past the idle/GC window without sleeping (card t_19d8fd3b).
var timeNow = time.Now
