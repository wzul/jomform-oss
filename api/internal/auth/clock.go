package auth

import "time"

// timeNow is swappable for deterministic tests.
var timeNow = time.Now
