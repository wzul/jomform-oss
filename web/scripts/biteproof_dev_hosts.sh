#!/usr/bin/env bash
# Bite proof for web/src/lib/devHosts.test.ts.
#
# Each case SABOTAGES the config in a different way, runs the guard, and asserts
# it goes RED. A guard that stays green through all three is not protecting
# anything. The config is restored after each case, and the file's sha256 is
# asserted unchanged at the end so a failed run cannot leave the tree edited.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2
CFG=next.config.mjs
GUARD=src/lib/devHosts.test.ts
export PATH="/root/.hermes/tools/node-26.7.0-linux-x64/bin:$PATH"

BEFORE=$(sha256sum "$CFG" | cut -d' ' -f1)
cp "$CFG" /tmp/devHosts.cfg.orig

run_guard() { node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON "$GUARD" 2>&1; }
restore()  { cp /tmp/devHosts.cfg.orig "$CFG"; }

fail=0
expect_red() { # $1 = case name, $2 = output
  if printf '%s' "$2" | grep -qE '^ℹ fail [1-9]'; then
    echo "  ✅ BITES — $1 turned the guard RED"
  else
    echo "  ❌ DID NOT BITE — $1 left the guard GREEN (the guard is not protecting this)"
    fail=1
  fi
}

echo "case 0: baseline (the unsabotaged config must be GREEN)"
out=$(run_guard); echo "$out" | grep -E '^ℹ (pass|fail)'
if printf '%s' "$out" | grep -qE '^ℹ fail 0'; then echo "  ✅ baseline green"; else echo "  ❌ baseline not green"; fail=1; fi

echo "case 1: drop allowedDevOrigins entirely (the fix removed)"
python3 - "$CFG" <<'PY'
import re,sys
p=sys.argv[1]; s=open(p).read()
s=s.replace("  allowedDevOrigins: DEV_HOSTS,\n","")
open(p,'w').write(s)
PY
out=$(run_guard); expect_red "no allowedDevOrigins" "$out"; restore

echo "case 2: dev header forced back to immutable (the caching defect restored)"
python3 - "$CFG" <<'PY'
import sys
p=sys.argv[1]; s=open(p).read()
s=s.replace('process.env.NODE_ENV === "development"','false')
open(p,'w').write(s)
PY
out=$(run_guard); expect_red "immutable in dev" "$out"; restore

echo "case 3: drop the hostname validation (the silent no-op restored)"
python3 - "$CFG" <<'PY'
import re,sys
p=sys.argv[1]; s=open(p).read()
s=re.sub(r'for \(const host of DEV_HOSTS\) \{.*?\n\}\n', '', s, flags=re.S)
open(p,'w').write(s)
PY
out=$(run_guard); expect_red "no validation" "$out"; restore

echo "case 4: bake the staging hostname in as a literal default (the per-env drift restored)"
python3 - "$CFG" <<'PY'
import sys
p=sys.argv[1]; s=open(p).read()
s=s.replace('(process.env.JOMFORM_DEV_HOSTS || "")','(process.env.JOMFORM_DEV_HOSTS || "jomform-staging.example.com")')
open(p,'w').write(s)
PY
out=$(run_guard); expect_red "baked-in literal" "$out"; restore

AFTER=$(sha256sum "$CFG" | cut -d' ' -f1)
echo
if [ "$BEFORE" = "$AFTER" ]; then echo "config restored intact ($AFTER)"; else echo "❌ CONFIG NOT RESTORED"; fail=1; fi
rm -f /tmp/devHosts.cfg.orig
exit $fail
