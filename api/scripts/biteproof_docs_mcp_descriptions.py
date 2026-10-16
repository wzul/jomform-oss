#!/usr/bin/env python3
"""Bite proof for api/internal/mcpsrv/docs_mcp_page_description_test.go (card t_3a0d74d2).

WHAT IT BACKS. The guard this drives pins the rule that a /docs/mcp row carries the
REGISTERED description (the string an MCP client receives over tools/list) verbatim,
because 40 of the 181 rows had drifted from it and nothing checked the prose at all.
A guard whose subject is a hand-copied page can look identical to a working guard
while checking nothing, so this harness sabotages one thing at a time and requires
the guard to go RED for that case's OWN reason — then restores every touched file and
re-checks its sha256 byte for byte.

CASES (7):
  0  CONTROL          the untouched tree is GREEN (so a red below cannot be this tree)
  1  page row revert  archive_product's row back to its pre-fix short text — the page
                      advertises less than the tool does, the shape that shipped
  2  registry revert  get_workspace_settings' registered description back to the text
                      that NEVER listed the three fields its output returns, with the
                      page left corrected — proves the expectation comes from the
                      REGISTRY (read over the protocol), not from a source regex or
                      from the page itself
  3  row deleted      refund_sale's row removed — the page omits a registered tool
  4  row duplicated   list_products' row duplicated — a duplicate could carry the
                      registered text while the row a reader sees does not
  5  unknown escape   a `\\q` planted in purge_form's desc — the TS literal decoder
                      must REFUSE an escape it does not know instead of passing the
                      raw bytes through (which would red a correct row as prose drift)
  6  page missing     page.tsx renamed away — the guard must fail loudly, never skip

Nothing here is a gate. Run it before handing off a change that touches the guard,
the tool registry's descriptions (api/internal/mcpsrv/*.go) or the /docs/mcp page.
"""
import hashlib
import os
import re
import subprocess
import sys
import time

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
API = os.path.join(ROOT, "api")
PAGE = os.path.join(ROOT, "web/src/app/docs/mcp/page.tsx")
TOOLS = os.path.join(API, "internal/mcpsrv/tools.go")
GUARD = "TestDocsMCPPageRowsCarryTheRegisteredDescription"

# The PRE-FIX text of two rows (measured on the card's base, main af72e713).
PRE_FIX_ARCHIVE_PRODUCT = "Nyahaktifkan produk (dulang tanpa padam data)."
PRE_FIX_GET_WORKSPACE_SETTINGS = ("Baca tetapan storefront satu workspace: footer, company_reg, "
                                  "privacy/terms/refund URL, contact, menu.")


def digest(path):
    return hashlib.sha256(open(path, "rb").read()).hexdigest()


def run_guard():
    p = subprocess.run(["go", "test", "./internal/mcpsrv/", "-run", GUARD, "-count=1", "-v"],
                       cwd=API, capture_output=True, text=True)
    return p.returncode, p.stdout + p.stderr


failures = []
bites = 0


def record(label, ok, detail):
    global bites
    if ok:
        bites += 1
        print(f"  BITES      {label}: {detail}")
    else:
        failures.append(label)
        print(f"  *** MISS *** {label}: {detail}")


def with_mutation(label, needle, mutate, wanted=""):
    """mutate() returns {path: original_text}; every path must come back byte-identical."""
    print(f"CASE {label}")
    touched = mutate()
    rc, out = run_guard()
    restored = all(write_back(p, text) for p, text in touched.items())
    named = needle in out
    record(label, rc != 0 and named and restored,
           f"guard rc={rc}, message names {needle!r}={named}, sources restored byte-identical={restored}")
    if rc != 0 and named:
        for line in out.split("\n"):
            if needle in line:
                print(f"    | {line.strip()[:200]}")
                break
    if wanted and wanted not in out:
        record(f"{label} (message shape)", False, f"expected {wanted!r} in the guard's output")
    return out


def write_back(path, text):
    with open(path, "w", encoding="utf-8") as fh:
        fh.write(text)
    return digest(path) == hashlib.sha256(text.encode()).hexdigest()


def main():
    started = time.time()
    page = open(PAGE, encoding="utf-8").read()
    tools = open(TOOLS, encoding="utf-8").read()
    before = {PAGE: digest(PAGE), TOOLS: digest(TOOLS)}
    status_before = subprocess.run(["git", "status", "--porcelain"], cwd=ROOT,
                                   capture_output=True, text=True).stdout

    # 0 — CONTROL
    print("CASE 0 control")
    rc, out = run_guard()
    record("control (untouched tree is GREEN)", rc == 0 and "--- PASS" in out,
           f"guard rc={rc}, printed --- PASS: {('--- PASS' in out)}")

    # 1 — the page stops carrying what the tool says
    def mutate_page_row():
        m = re.search(r'(\{\s*name: "archive_product",\s*desc: ")(?:[^"\\]|\\.)*(")', page)
        assert m, "harness anchor missing: the archive_product row"
        open(PAGE, "w", encoding="utf-8").write(
            page[:m.start()] + m.group(1) + PRE_FIX_ARCHIVE_PRODUCT + m.group(2) + page[m.end():])
        return {PAGE: page}

    with_mutation("1 page row reverts to its pre-fix text (archive_product)",
                  "archive_product", mutate_page_row, wanted="do NOT carry the registered description")

    # 2 — the REGISTRY reverts, the page stays corrected
    def mutate_registry():
        m = re.search(r'(Description: ")([^"]*tetapan storefront satu workspace[^"]*)(")', tools)
        assert m, "harness anchor missing: get_workspace_settings' description"
        open(TOOLS, "w", encoding="utf-8").write(
            tools[:m.start()] + m.group(1) + PRE_FIX_GET_WORKSPACE_SETTINGS + m.group(3) + tools[m.end():])
        return {TOOLS: tools}

    with_mutation("2 the registered description reverts (get_workspace_settings)",
                  "get_workspace_settings", mutate_registry)

    # 3 — a registered tool's row is gone
    def mutate_delete_row():
        new = "\n".join(l for l in page.split("\n") if 'name: "refund_sale"' not in l)
        open(PAGE, "w", encoding="utf-8").write(new)
        return {PAGE: page}

    with_mutation("3 a page row is DELETED (refund_sale)", "omits 1 registered tool(s)",
                  mutate_delete_row)

    # 4 — a row is listed twice
    def mutate_dup_row():
        row = [l for l in page.split("\n") if 'name: "list_products"' in l][0]
        open(PAGE, "w", encoding="utf-8").write(page.replace(row, row + "\n" + row))
        return {PAGE: page}

    with_mutation("4 a page row is DUPLICATED (list_products)", "more than once", mutate_dup_row)

    # 5 — an escape the decoder does not know is refused, not passed through
    def mutate_unknown_escape():
        m = re.search(r'(\{\s*name: "purge_form",\s*desc: ")((?:[^"\\]|\\.)*)(")', page)
        assert m, "harness anchor missing: the purge_form row"
        open(PAGE, "w", encoding="utf-8").write(
            page[:m.start()] + m.group(1) + m.group(2) + "\\qX" + m.group(3) + page[m.end():])
        return {PAGE: page}

    with_mutation("5 an unknown escape in a row is REFUSED (purge_form)", "unknown escape",
                  mutate_unknown_escape)

    # 6 — the page is gone: fail loudly, never skip
    print("CASE 6 the page is MISSING")
    os.rename(PAGE, PAGE + ".biteproof-moved")
    rc, out = run_guard()
    os.remove(PAGE + ".biteproof-moved")
    restored = write_back(PAGE, page)
    named = "read " in out and "no such file" in out
    record("6 the page is MISSING (renamed away)", rc != 0 and named and restored,
           f"guard rc={rc}, names the missing file={named}, page restored byte-identical={restored}")

    after = {PAGE: digest(PAGE), TOOLS: digest(TOOLS)}
    status_after = subprocess.run(["git", "status", "--porcelain"], cwd=ROOT,
                                  capture_output=True, text=True).stdout
    print()
    print(f"sources byte-identical after every case: {before == after}")
    print(f"git status unchanged by the run: {status_before == status_after}")
    print(f"BITES: {bites}/7  MISSES: {len(failures)} {failures}")
    print(f"wall: {time.time() - started:.0f}s")
    if failures or before != after or status_before != status_after:
        print("BITEPROOF FAIL")
        sys.exit(1)
    print("BITEPROOF PASS")
    sys.exit(0)


if __name__ == "__main__":
    main()
