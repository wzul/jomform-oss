// Package-level metrics accessors (issue #100).
//
// The Server owns the *metrics.Metrics instance; NewServer registers it here
// so the auth/payment/upload/SES handlers can record counters without each
// handler struct needing a metrics field. All accessors are nil-safe.
package httpapi

import "github.com/wzul/jomform/api/internal/metrics"

var globalMetrics *metrics.Metrics

func setGlobalMetrics(m *metrics.Metrics) { globalMetrics = m }

func recordAuthFailure(reason string) {
	if globalMetrics != nil {
		globalMetrics.IncAuthFailure(reason)
	}
}

func recordError(class string) {
	if globalMetrics != nil {
		globalMetrics.IncError(class)
	}
}

func recordPaymentFailure(op string) {
	if globalMetrics != nil {
		globalMetrics.IncPaymentFailure(op)
	}
}

func recordUploadFailure(reason string) {
	if globalMetrics != nil {
		globalMetrics.IncUploadFailure(reason)
	}
}

func recordSESSend(kind string) {
	if globalMetrics != nil {
		globalMetrics.IncSESSend(kind)
	}
}

func recordSESSuppression(reason string) {
	if globalMetrics != nil {
		globalMetrics.IncSESSuppression(reason)
	}
}

// recordSESUnroutable counts a send refused before the mailer because the
// recipient's domain can never receive mail (card t_641d62d5). It is a counter
// rather than a log line about the address: the board can see how often this
// happens without an address being kept anywhere.
func recordSESUnroutable(reason string) {
	if globalMetrics != nil {
		globalMetrics.IncSESUnroutable(reason)
	}
}
