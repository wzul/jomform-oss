package httpapi

import "log/slog"

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}
