package mcpsrv

// Issue #551 test helpers. The MCP product input structs take price_sen as a
// POINTER so a missing/null value is rejected instead of silently decoding to 0.

// senPtr builds an *int64 price for the MCP product inputs.
func senPtr(v int64) *int64 { return &v }
