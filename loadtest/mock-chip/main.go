// Command mock-chip is a minimal CHIP Collect API stub for load testing.
//
// It implements just enough of the CHIP API for JomForm's payment
// create-purchase path so a load test can exercise the full JomForm flow
// (sale insert + CHIP purchase request + checkout URL attach) without
// hitting the live CHIP sandbox:
//
//   POST /purchases/   -> 201 {"id","checkout_url","status":"created",...}
//   GET  /public_key/  -> 200 {"public_key":"..."} (used by credential checks)
//
// Point the API at it with CHIP_API_BASE_URL=http://localhost:8090.
//
// Run:
//   go run ./loadtest/mock-chip
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync/atomic"
)

var purchaseSeq atomic.Int64

func main() {
	addr := os.Getenv("MOCK_CHIP_ADDR")
	if addr == "" {
		addr = ":8090"
	}

	mux := http.NewServeMux()

	// POST /purchases/ — create a purchase. Returns a minimal Purchase.
	mux.HandleFunc("POST /purchases/", func(w http.ResponseWriter, r *http.Request) {
		// Drain the body so the request is fully read (mirrors real CHIP).
		_ = json.NewDecoder(r.Body).Decode(&map[string]any{})
		id := fmt.Sprintf("mock_purchase_%d", purchaseSeq.Add(1))
		resp := map[string]any{
			"id":            id,
			"status":        "created",
			"checkout_url":  "https://checkout.chip-in.test/" + id,
			"reference":     "mock-ref",
			"total":         100,
			"currency":      "MYR",
			"is_recurring_token": false,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(resp)
	})

	// GET /public_key/ — used by ValidateCredentials / chip status probes.
	mux.HandleFunc("GET /public_key/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"public_key": "mock-public-key"})
	})

	log.Printf("mock-chip listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
