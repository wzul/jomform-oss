// Command menu-fixture serves a NON-PROD restaurant-menu checkout whose sheet
// really carries a radio option group and an image-choice option group, so a
// verification lane can measure the page a buyer gets without inventing a
// harness and without writing anything to production.
//
// It owns a scratch schema of the shared test database, applies the real
// migrations, seeds a workspace + two products + a published menu form with a
// published order form attached, and serves the real public handler on a
// loopback listener. Requests are pinned to the fixture's workspace subdomain,
// so the URL it prints works as-is in a browser — no /etc/hosts entry, no DNS.
//
// Usage (from api/):
//
//	go run ./cmd/menu-fixture                 # JSON line on stdout, then serve
//	go run ./cmd/menu-fixture -addr 127.0.0.1:8099
//	go run ./cmd/menu-fixture -keep           # keep the scratch schema on exit
//
// The first line on stdout is a single JSON object describing the fixture (URL,
// slugs, block ids, products). Everything else goes to stderr, so a caller can
// read the URL with `head -1`.
//
// It needs JOMFORM_TEST_DATABASE_URL (or -dsn). Nothing else: no CHIP
// credentials, no object storage, no mailer. An order submitted with
// `payment_method:"bank_direct"` is accepted and stores the option answers; one
// with `"chip"` fails at a dead loopback port, which is the payfail path.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wzul/jomform/api/internal/menufixture"
)

func main() {
	var (
		addr   = flag.String("addr", "127.0.0.1:0", "listen address (loopback only; :0 picks a free port)")
		dsn    = flag.String("dsn", "", "test database URL (default $"+menufixture.DSNEnv+")")
		schema = flag.String("schema", "", "scratch schema to own (default "+menufixture.DefaultSchema()+")")
		keep   = flag.Bool("keep", false, "keep the scratch schema on exit (default: drop it)")
		quiet  = flag.Bool("quiet", false, "suppress the migration log on stderr")
	)
	flag.Parse()

	// A fixture is a short-lived instrument: it must not sit on a scratch schema
	// for hours because a lane forgot to close it.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fail("listen %s: %v", *addr, err)
	}
	base := "http://" + ln.Addr().String()

	log := slog.New(slog.DiscardHandler)
	if !*quiet {
		log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	}

	openCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	f, err := menufixture.Open(openCtx, menufixture.Options{DSN: *dsn, Schema: *schema, BaseURL: base, Log: log})
	if err != nil {
		_ = ln.Close()
		fail("seed the fixture: %v", err)
	}

	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
		"url":                base + f.MenuPath(),
		"origin":             base,
		"menu_path":          f.MenuPath(),
		"host":               f.Host,
		"workspace":          f.Workspace,
		"menu_slug":          f.MenuSlug,
		"order_slug":         f.OrderSlug,
		"order_url":          base + "/" + f.MenuSlug + "/order",
		"radio_block":        f.RadioBlock,
		"image_choice_block": f.ImageChoiceBlock,
		"product_ids":        f.ProductIDs,
		"schema":             f.Schema,
		"keep_schema":        *keep,
	}); err != nil {
		_ = f.Close(context.Background())
		_ = ln.Close()
		fail("write the fixture line: %v", err)
	}
	os.Stdout.Sync()

	srv := &http.Server{Handler: f.Handler(), ReadHeaderTimeout: 10 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	fmt.Fprintf(os.Stderr, "menu-fixture: serving %s (schema %s)\n", base+f.MenuPath(), f.Schema)

	select {
	case <-ctx.Done():
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "menu-fixture: serve: %v\n", err)
		}
	}

	shutCtx, cancelShut := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShut()
	_ = srv.Shutdown(shutCtx)
	if *keep {
		fmt.Fprintf(os.Stderr, "menu-fixture: keeping schema %s (-keep)\n", f.Schema)
	} else if err := f.Close(shutCtx); err != nil {
		fmt.Fprintf(os.Stderr, "menu-fixture: teardown: %v\n", err)
		os.Exit(1)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "menu-fixture: "+format+"\n", args...)
	os.Exit(1)
}
