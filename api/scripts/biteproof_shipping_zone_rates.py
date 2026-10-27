#!/usr/bin/env python3
"""Bite proof for the shipping-by-zone guards.

WHAT IT BACKS (the card's "tests bite" acceptance):
  * api/internal/shipping/shipping_test.go — the resolver's rules;
  * api/internal/httpapi/shipping_zone_order_test.go — the same rules end to end
    (a submission from a Peninsular address and one from an East Malaysian
    address must be charged DIFFERENT fees, in sales.shipping_sen, with the fee
    out of the line items), plus the static-block regression.

EACH MUTATION is a COMPILING revert of ONE real part of the behaviour, so a red
is attributable to that part and not to a broken build:

  A  MatchZone's state rule neutered (no zone ever matches by state)   -> the
     per-address lookup is gone, so both addresses fall down the no-match path.
  B  The postcode range comparison neutered                            -> a
     postcode exception stops matching (the priority fixture).
  C  The free-shipping threshold disabled                              -> a cart
     over the threshold is charged the full rate.
  D  A fallback zone allowed to match DIRECTLY                         -> an
     on_no_match=reject block charges an address it must refuse.
  E  The zone choice allowed to override a disagreeing address         -> the
     buyer is re-priced behind their back instead of refused.
  F  Weight bands ignored when a weight IS declared                    -> a 6kg
     parcel is charged the 0-2kg rate.
  G  order.go: the zone-sourced block's item is NOT dropped from the cart
     (a one-line revert of `continue`)                                 -> the fee
     is counted twice: once as a line item, once as shipping_sen.
  H  order.go: shipping_sen never set (the pre-feature line restored)  -> the
     fee vanishes from the sale while the total still includes it… i.e. the
     money invariant breaks.

MUTATIONS F, G AND H are the ones a unit test cannot reach on its own: F is a
weight rule (resolver), G and H are the submit path (httpapi). Each case runs the
FULL package, never a single -run filter (a nil-deref can pass in isolation).

Every mutation is restored byte-identically and re-checked by sha256; a signal
(SIGTERM/SIGINT) restores before exiting, so a timeout kill cannot leave the
worktree mutated. A mutation whose anchor does not match exactly once is counted
as a FAILURE naming the harness, never as a bite (a mutation that never landed
measures the real, GREEN file).
"""
import atexit
import hashlib
import os
import signal
import subprocess
import sys

API = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SHIPPING = os.path.join(API, "internal/shipping/shipping.go")
ORDER = os.path.join(API, "internal/httpapi/order.go")
TARGETS = [SHIPPING, ORDER]
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
        "A: MatchZone never matches by state",
        SHIPPING,
        "\t\tif wantState != \"\" {\n\t\t\tfor _, entry := range z.States {\n\t\t\t\tfor _, tok := range stateTokens(entry) {\n\t\t\t\t\tif tok != \"\" && tok == wantState {\n\t\t\t\t\t\treturn z, \"state\", true\n\t\t\t\t\t}\n\t\t\t\t}\n\t\t\t}\n\t\t}\n",
        "\t\tif false && wantState != \"\" {\n\t\t\tfor _, entry := range z.States {\n\t\t\t\tfor _, tok := range stateTokens(entry) {\n\t\t\t\t\tif tok != \"\" && tok == wantState {\n\t\t\t\t\t\treturn z, \"state\", true\n\t\t\t\t\t}\n\t\t\t\t}\n\t\t\t}\n\t\t}\n",
    ),
    (
        "B: the postcode range never matches",
        SHIPPING,
        "\t\tif wantPostcode != \"\" && PostcodeInRange(z.PostcodeFrom, z.PostcodeTo, wantPostcode) {",
        "\t\tif false && wantPostcode != \"\" && PostcodeInRange(z.PostcodeFrom, z.PostcodeTo, wantPostcode) {",
    ),
    (
        "C: the free-shipping threshold is disabled",
        SHIPPING,
        "\tif rate.FreeOverSen > 0 && subtotalSen >= rate.FreeOverSen {",
        "\tif false && rate.FreeOverSen > 0 && subtotalSen >= rate.FreeOverSen {",
    ),
    (
        "D: a fallback zone matches directly (no policy needed)",
        SHIPPING,
        "\tfor _, z := range set.Zones {\n\t\tif z.Fallback {\n\t\t\tcontinue\n\t\t}\n\t\tordered = append(ordered, z)\n\t}",
        "\tfor _, z := range set.Zones {\n\t\tordered = append(ordered, z)\n\t}",
    ),
    (
        "E: a zone choice overrides a disagreeing address",
        SHIPPING,
        "\t\tif chosen.ID != zone.ID {\n\t\t\treturn Quote{}, ErrZoneMismatch\n\t\t}",
        "\t\tif false && chosen.ID != zone.ID {\n\t\t\treturn Quote{}, ErrZoneMismatch\n\t\t}",
    ),
    (
        "F: weight bands ignored when a weight IS declared",
        SHIPPING,
        "\tif weightG <= 0 {\n\t\treturn ordered[0], nil\n\t}",
        "\tif true {\n\t\treturn ordered[0], nil\n\t}",
    ),
    (
        # Card t_2ba64e76 renamed this map (zoneItemIDs -> feeItemIDs) when the
        # courier source arrived: the drop now covers BOTH server-resolved
        # sources, so the phase-1 anchor moved with it. A harness whose anchor
        # stops matching is a HARNESS BUG, not a bite.
        "G: order.go keeps the shipping block's item as a line item (fee counted twice)",
        ORDER,
        "\t\tif feeItemIDs[it.ProductID] {\n\t\t\tcontinue\n\t\t}",
        "\t\tif false && feeItemIDs[it.ProductID] {\n\t\t\tcontinue\n\t\t}",
    ),
    (
        "H: order.go records no shipping_sen (the pre-feature total)",
        ORDER,
        "\tsale.TotalSen = payments.SumItems(subtotal, sale.ShippingSen, 0)",
        "\tsale.ShippingSen = 0\n\tsale.TotalSen = payments.SumItems(subtotal, sale.ShippingSen, 0)",
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
    control = {}
    for pkg in PACKAGES:
        rc, out = run(["go", "test", pkg, "-count=1", "-timeout", "900s"])
        control[pkg] = rc
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

        b, _ = run(["go", "build", "./..."])
        reds = []
        for pkg in PACKAGES:
            rc, out = run(["go", "test", pkg, "-count=1", "-timeout", "900s"])
            reds.append((pkg, rc, out))
        bites = any(rc != 0 for _, rc, _ in reds)
        if not bites:
            not_biting.append(label)
        print("  %-72s build rc=%d  -> %s" % (label, b, "RED (bites)" if bites else "GREEN (DOES NOT BITE)"), flush=True)
        if b != 0:
            print("      NOTE: the mutation also broke the build - a compile error would hide the guard", flush=True)
        for pkg, rc, out in reds:
            if rc == 0:
                continue
            for line in out.strip().splitlines():
                if line.startswith("--- FAIL") or "shipping" in line.lower():
                    print("      %s: %s" % (pkg, line.strip()), flush=True)
                    break

        with open(path, "wb") as fh:
            fh.write(ORIG[path])

    print(flush=True)
    for p in TARGETS:
        after = sha(p)
        print("restored %-14s sha256 %s %s" % (
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
