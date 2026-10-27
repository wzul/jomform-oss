#!/usr/bin/env python3
"""Bite proof for the courier provider layer (shipping phase 2).

WHAT IT BACKS (the card's "tests must bite" acceptance):
  * api/internal/shipping/courier_test.go — the adapters' contracts (Lalamove's
    HMAC signing and `{"data": …}` wrapping, EasyParcel's OAuth exchange and ISO
    3166-2:MY subdivision codes), the resolver's rule that a QUOTE WINS and the
    zone table falls back, and the quote cache that keeps a double submission to
    ONE quotation;
  * api/internal/httpapi/shipping_courier_order_test.go — the same rules end to
    end against a real Postgres: the quoted fee lands in sales.shipping_sen +
    sales.total_sen, a failing provider falls back to the zone rate with the
    reason class recorded, and a courier block with nothing to fall back to is a
    400 rather than a silent 0.

EACH MUTATION is a COMPILING revert of ONE real part of the behaviour, so a red
is attributable to that part and not to a broken build:

  A  httpapi: the provider is never quoted (the connection check forced false)
     -> every courier order silently charges the ZONE rate, and the quoted-fee
     test cannot pass.
  B  shipping: a failed quote with no fallback zone charges 0 instead of
     refusing -> the "never a silent 0" rule is gone.
  C  shipping: the zone ESTIMATE the buyer's form showed is not recorded
     -> a disputed fee stops being explainable.
  D  shipping: the quotation cache never hits -> an identical request creates a
     SECOND quotation (and, later, a second booking) for one buyer intent.
  E  shipping: Lalamove's body is sent FLAT instead of wrapped in `data`
     -> the measured contract is reverted (the real API answers 401).
  F  shipping: the HMAC raw string uses \\n instead of \\r\\n -> every signature
     is wrong (the failure mode that reads like a bad key).
  G  shipping: EasyParcel's subdivision codes are dropped from the request
     -> a lane is priced by postcode alone (or refused by the API).
  H  httpapi: the courier block's own item is NOT dropped from the cart
     -> the fee is counted twice: once as a line item, once as shipping_sen.

MUTATIONS A, B, C, D AND H are the ones a unit test cannot reach alone: they are
the submit path and the store, not the pure adapters. Each case runs the FULL
package, never a single -run filter (a nil-deref can pass in isolation).

Every mutation is restored byte-identically and re-checked by sha256; a signal
(SIGTERM/SIGINT) restores before exiting, so a timeout kill cannot leave the
worktree mutated. A mutation whose anchor does not match exactly once is counted
as a FAILURE naming the harness, never as a bite (a mutation that never landed
measures the real, GREEN file).

NO NETWORK. The adapters are driven against httptest stubs in the tests, so this
harness never calls Lalamove or EasyParcel — and never books anything.
"""
import atexit
import hashlib
import os
import signal
import subprocess
import sys

API = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
COURIER = os.path.join(API, "internal/shipping/courier.go")
CACHE = os.path.join(API, "internal/shipping/quotecache.go")
LALAMOVE = os.path.join(API, "internal/shipping/lalamove.go")
EASYPARCEL = os.path.join(API, "internal/shipping/easyparcel.go")
HTTP_COURIER = os.path.join(API, "internal/httpapi/shipping_couriers.go")
ORDER = os.path.join(API, "internal/httpapi/order.go")
TARGETS = [COURIER, CACHE, LALAMOVE, EASYPARCEL, HTTP_COURIER, ORDER]
DONE = False

REQUIRED_ENV = "JOMFORM_TEST_DATABASE_URL"


def sha(path):
    with open(path, "rb") as fh:
        return hashlib.sha256(fh.read()).hexdigest()


ORIG = {p: open(p, "rb").read() for p in TARGETS}
BEFORE = {p: sha(p) for p in TARGETS}


def restore_all():
    global DONE
    for p, data in ORIG.items():
        with open(p, "wb") as fh:
            fh.write(data)
        if sha(p) != BEFORE[p]:
            print("!! RESTORE FAILED for %s" % p, flush=True)
    DONE = True


atexit.register(lambda: None if DONE else restore_all())


def _sig(signum, _frame):
    restore_all()
    print("signal %d - originals restored, exiting" % signum, flush=True)
    sys.exit(130)


signal.signal(signal.SIGTERM, _sig)
signal.signal(signal.SIGINT, _sig)


def run(cmd):
    p = subprocess.run(cmd, cwd=API, capture_output=True, text=True)
    return p.returncode, (p.stdout + p.stderr)


# ── the mutations, each a compiling revert of ONE behaviour ───────────────────
MUTATIONS = [
    (
        "A: the courier is never asked (the connection check forced false)",
        HTTP_COURIER,
        "\t\tconn, ok := h.Couriers.Connected(ctx, tgt.AccountID, spec.Provider)",
        "\t\tconn, ok := h.Couriers.Connected(ctx, tgt.AccountID, spec.Provider)\n\t\t_ = conn\n\t\tok = false",
    ),
    (
        "B: a failed quote with no fallback charges 0 instead of refusing",
        COURIER,
        "\t\tif !haveZone {\n\t\t\treturn CourierQuote{}, ErrCourierNoFallback\n\t\t}",
        "\t\tif !haveZone {\n\t\t\tout.Source = FeeSourceZones\n\t\t\tout.PriceSen = 0\n\t\t\tout.FallbackReason = reason\n\t\t\treturn out, nil\n\t\t}",
    ),
    (
        "C: the zone estimate the buyer was shown is not recorded",
        COURIER,
        "\t\t\tout.EstimatePriceSen = zoneQuote.PriceSen",
        "\t\t\tout.EstimatePriceSen = 0",
    ),
    (
        "D: the quotation cache never hits (a duplicate gets a second quotation)",
        CACHE,
        "\te, ok := c.entries[c.key(req)]\n\tif !ok {\n\t\treturn ProviderQuote{}, false\n\t}",
        "\te, ok := c.entries[c.key(req)]\n\tif !ok || ok {\n\t\treturn ProviderQuote{}, false\n\t}",
    ),
    (
        "E: Lalamove's body is sent flat instead of wrapped in data",
        LALAMOVE,
        "\tpayload := append([]byte(`{\"data\":`), inner...)",
        "\tpayload := append([]byte(``), inner...)",
    ),
    (
        "F: the HMAC raw string uses \\n instead of \\r\\n",
        LALAMOVE,
        "\tmac.Write([]byte(timestamp + \"\\r\\n\" + method + \"\\r\\n\" + path + \"\\r\\n\\r\\n\" + body))",
        "\tmac.Write([]byte(timestamp + \"\\n\" + method + \"\\n\" + path + \"\\n\\n\" + body))",
    ),
    (
        "G: EasyParcel's subdivision codes are dropped from the request",
        EASYPARCEL,
        "\titem.Sender = easyParcelStop{Postcode: originPostcode, SubdivisionCode: originSub, Country: country}\n"
        "\titem.Receiver = easyParcelStop{Postcode: destPostcode, SubdivisionCode: destSub, Country: country}",
        "\titem.Sender = easyParcelStop{Postcode: originPostcode, SubdivisionCode: \"\", Country: country}\n"
        "\titem.Receiver = easyParcelStop{Postcode: destPostcode, SubdivisionCode: \"\", Country: country}",
    ),
    (
        "H: the courier block's item stays a line item (fee counted twice)",
        ORDER,
        "\t\tif feeItemIDs[it.ProductID] {\n\t\t\tcontinue\n\t\t}",
        "\t\tif false && feeItemIDs[it.ProductID] {\n\t\t\tcontinue\n\t\t}",
    ),
]

PACKAGES = ["./internal/shipping/", "./internal/httpapi/"]


def main():
    if not os.environ.get(REQUIRED_ENV):
        print("%s is not set: every guard under proof is DB-backed, and a SKIPPED "
              "guard reads exactly like a green one" % REQUIRED_ENV, flush=True)
        return 2

    run(["go", "build", "./..."])  # prewarm
    brc, bout = run(["go", "build", "./..."])
    if brc != 0:
        print("CONTROL: the tree does not build", flush=True)
        print(bout[-3000:], flush=True)
        return 1

    # The CONTROL: the unmutated tree must be GREEN on the FULL packages, so a red
    # below cannot be this tree.
    for pkg in PACKAGES:
        rc, out = run(["go", "test", pkg, "-count=1", "-timeout", "900s"])
        print("CONTROL (unmutated) %-24s rc=%d" % (pkg, rc), flush=True)
        if rc != 0:
            print(out[-3000:], flush=True)
            print("CONTROL NOT GREEN - fix the guard before proving it bites", flush=True)
            return 1

    not_biting = []
    for label, path, old, new in MUTATIONS:
        data = ORIG[path].decode()
        n = data.count(old)
        if n != 1:
            print("  %-72s HARNESS BUG: anchor matched %d times, expected 1" % (label, n), flush=True)
            not_biting.append(label)
            continue
        with open(path, "wb") as fh:
            fh.write(data.replace(old, new).encode())

        b, bout = run(["go", "build", "./..."])
        reds = []
        for pkg in PACKAGES:
            rc, out = run(["go", "test", pkg, "-count=1", "-timeout", "900s"])
            reds.append((pkg, rc, out))
        bites = any(rc != 0 for _, rc, _ in reds)
        if not bites:
            not_biting.append(label)
        print("  %-72s build rc=%d  -> %s" % (
            label, b, "RED (bites)" if bites else "GREEN (DOES NOT BITE)"), flush=True)
        if b != 0:
            print("      NOTE: the mutation also broke the build - a compile error would hide the guard", flush=True)
            print("      %s" % bout.strip().splitlines()[-1][:200], flush=True)
        for pkg, rc, out in reds:
            if rc == 0:
                continue
            for line in out.strip().splitlines():
                if line.startswith("--- FAIL"):
                    print("      %s: %s" % (pkg, line.strip()), flush=True)
                    break

        with open(path, "wb") as fh:
            fh.write(ORIG[path])

    print(flush=True)
    for p in TARGETS:
        after = sha(p)
        print("restored %-18s sha256 %s %s" % (
            os.path.basename(p), after[:16], "OK" if after == BEFORE[p] else "MISMATCH"), flush=True)

    rc_b, _ = run(["go", "build", "./..."])
    final = {}
    for pkg in PACKAGES:
        rc, _ = run(["go", "test", pkg, "-count=1", "-timeout", "900s"])
        final[pkg] = rc
    print("final unmutated: go build rc=%d, %s" % (
        rc_b, ", ".join("%s rc=%d" % (k, v) for k, v in final.items())), flush=True)
    print("MUTATIONS THAT DID NOT BITE: %d %s" % (len(not_biting), not_biting), flush=True)
    bad = len(not_biting) or rc_b or any(final.values())
    print("RESULT: %s" % ("ALL CASES BIT" if not bad else "A CASE DID NOT BITE"), flush=True)
    return 1 if bad else 0


if __name__ == "__main__":
    code = main()
    DONE = True
    restore_all()
    print("EXIT=%d" % code, flush=True)
    sys.exit(code)
