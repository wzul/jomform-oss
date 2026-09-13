package assetgate

// Issue #543 regression: ordinary JavaScript must NOT be quarantined, while the
// genuine execution vectors must still hard block.
//
// The scanner previously hard-blocked the bare `function(` keyword as an "active
// code execution marker". `function` is a JavaScript keyword present in virtually
// every non-trivial script, so any merchant asset using an anonymous function,
// callback or IIFE was permanently quarantined — and a quarantine is unrecoverable
// (persisted immediately, required for serving, with no merchant un-quarantine
// action). This test FAILS against the pre-fix code.

import "testing"

// TestIssue543OrdinaryJavascriptIsNotQuarantined is the core guard.
func TestIssue543OrdinaryJavascriptIsNotQuarantined(t *testing.T) {
	legitJS := []string{
		`document.querySelectorAll('.x').forEach(function(el){ el.classList.add('y'); });`,
		`setTimeout(function(){ window.scrollTo(0,0); }, 100);`,
		`addEventListener('click', function(e){ e.preventDefault(); });`,
		`var fns = { ok: function(){ return true; } };`,
		`[1,2,3].map(function(n){ return n * 2; });`,
		`$(document).ready(function(){ $('.carousel').hide(); });`,
		`obj = { run: function() { return 1; } };`,
		`Promise.resolve().then(function(v){ console.log(v); });`,
		`(function(){ var t = document.getElementById('t'); t.textContent = 'Hi'; })();`,
		`function greet(name) { return 'Hello ' + name; }`,
		`setInterval(() => { console.log('tick'); }, 1000);`,
		`var myFunction = () => 1;`, // a lowercase identifier containing "function"
	}
	for _, src := range legitJS {
		v := DeterministicScan([]byte(src), "text/javascript; charset=utf-8")
		if v.Status == StatusQuarantined {
			t.Errorf("issue #543: ordinary JavaScript was QUARANTINED (permanent, no recovery):\n"+
				"  source: %s\n  reasons: %v", src, v.Reasons)
		}
	}
}

// TestIssue543FunctionConstructorStillBlocks proves the fix kept the real vector.
func TestIssue543FunctionConstructorStillBlocks(t *testing.T) {
	attacks := []string{
		`var f = new Function("return document.cookie");`,
		`new Function('a','return a')(1);`,
		`var f = Function("return 1");`,
		`x = new Function ("return 2");`,
	}
	for _, src := range attacks {
		v := DeterministicScan([]byte(src), "text/javascript; charset=utf-8")
		if v.Status != StatusQuarantined {
			t.Errorf("issue #543: the Function constructor must still hard block:\n  source: %s\n"+
				"  got status: %s", src, v.Status)
		}
	}
}

// TestIssue543OtherExecutionVectorsStillBlock pins the rest of the hard-block list.
func TestIssue543OtherExecutionVectorsStillBlock(t *testing.T) {
	for _, src := range []string{
		`eval("alert(1)")`,
		`document.write("<img src=x>")`,
		`x.constructor.constructor("return 1")()`,
		`javascript:alert(1)`,
	} {
		if v := DeterministicScan([]byte(src), "text/javascript; charset=utf-8"); v.Status != StatusQuarantined {
			t.Errorf("issue #543: %q must still hard block, got %s", src, v.Status)
		}
	}
}

// TestIssue543CssExecutionVectorsStillBlock pins the CSS hard blocks.
func TestIssue543CssExecutionVectorsStillBlock(t *testing.T) {
	for _, src := range []string{
		`a{width:expression(alert(1))}`,
		`a{-moz-binding:url(x.xml#y)}`,
		`.x{behavior:url(#default#time2)}`,
	} {
		if v := DeterministicScan([]byte(src), "text/css; charset=utf-8"); v.Status != StatusQuarantined {
			t.Errorf("issue #543: CSS vector %q must still hard block, got %s", src, v.Status)
		}
	}
}

// TestIssue543SvgNamespaceIsNotExternal proves a standard SVG no longer carries a
// spurious external-URL flag from its required xmlns declaration.
func TestIssue543SvgNamespaceIsNotExternal(t *testing.T) {
	svgs := []string{
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M4 4h16v16H4z"/></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"><rect width="9" height="9"/></svg>`,
		`<svg xmlns='http://www.w3.org/2000/svg'><style>.a{fill:red}</style></svg>`,
	}
	for _, src := range svgs {
		v := DeterministicScan([]byte(src), "image/svg+xml")
		for _, r := range v.Reasons {
			if r == "reference to external (non-jomform) URL from JS/HTML" {
				t.Errorf("issue #543: the required xmlns declaration was treated as an external "+
					"reference, so no SVG can reach an automatic ok:\n  %s", src)
			}
		}
		if v.Status == StatusQuarantined {
			t.Errorf("issue #543: a plain SVG must not be quarantined:\n  %s\n  reasons: %v", src, v.Reasons)
		}
	}
}

// TestIssue543RealExternalUrlStillFlagged proves the xmlns exemption did not
// disable external-URL detection generally.
func TestIssue543RealExternalUrlStillFlagged(t *testing.T) {
	if !hasExternalURL(`var s=document.createElement('script'); s.src="https://evil.example.com/x.js";`) {
		t.Error("issue #543: a genuine external script URL must still be flagged")
	}
	if !hasExternalURL(`<script src="http://evil.example.com/x.js"></script>`) {
		t.Error("issue #543: a genuine external script tag must still be flagged")
	}
	if hasExternalURL(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`) {
		t.Error("issue #543: an xmlns namespace declaration must NOT be flagged")
	}
	if hasExternalURL(`var u="https://wan.jomform.com/a.png";`) {
		t.Error("issue #543: a same-origin jomform URL must NOT be flagged (pre-existing behaviour)")
	}
}
