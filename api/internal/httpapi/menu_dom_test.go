package httpapi

// A minimal DOM for the restaurant-menu page's inline cart script.
//
// Why this exists: the menu cart is plain JS inside the served HTML, and the
// defects that matter on this page are STRUCTURAL — which element a variant
// selector resolves to, and which dish a chosen variant is recorded against.
// Asserting the script's SOURCE cannot see any of that (that is what the #665
// tests do), and grepping the served markup only proves the markup. So these
// tests take the script the handler actually served, run it in a real JS engine
// (goja) against a DOM built from that same document, drive the page's own
// listeners, and read the result out of the basket the script persisted.
//
// Selectors go through a real CSS engine (cascadia), so a wrong ancestor in the
// template or in the script is reproduced rather than re-modelled.
//
// Scope, stated plainly: this is NOT a browser. There is no layout, no CSS
// cascade and no real event loop; the shim implements exactly the surface the
// menu page uses and THROWS on anything else, so a script that starts depending
// on a new browser API fails loudly here instead of silently measuring nothing.
// The live-page check on this card covers what a harness cannot.

import (
	"strings"
	"testing"

	"github.com/andybalholm/cascadia"
	"github.com/dop251/goja"
	"golang.org/x/net/html"
)

// menuPageScript pulls the page's own cart <script> out of served HTML.
func menuPageScript(t *testing.T, body string) string {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse served menu HTML: %v", err)
	}
	var script string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "script" {
			var sb strings.Builder
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				sb.WriteString(c.Data)
			}
			if strings.Contains(sb.String(), "menuSheet") {
				script = sb.String()
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	if strings.TrimSpace(script) == "" {
		t.Fatalf("no menu cart script found in the served page")
	}
	return script
}

// menuDOM is the parsed page, a VM running its script, and the browser bits the
// script touches.
type menuDOM struct {
	t   *testing.T
	vm  *goja.Runtime
	doc *html.Node

	nodes []*html.Node
	index map[*html.Node]int
	byID  map[string]*html.Node

	storage  map[string]string
	clock    int64
	timers   []goja.Callable
	fetches  []string
	navigate string
	focused  *html.Node

	// orderResponse is the body the page's checkout POST is answered with, so a
	// test can drive the success path (a checkout_url) as well as the failure
	// path (an empty object) without a server.
	orderResponse map[string]any
}

// newMenuDOMPage serves a rendered menu and returns a DOM running its script.
func newMenuDOMPage(t *testing.T, data workspaceMenuData) (*menuDOM, string) {
	t.Helper()
	body := renderMenu492(t, data)
	d := newMenuDOM(t, body)
	d.run(menuPageScript(t, body))
	return d, body
}

func newMenuDOM(t *testing.T, body string) *menuDOM {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse served menu HTML: %v", err)
	}
	d := &menuDOM{
		t: t, vm: goja.New(), doc: doc,
		index: map[*html.Node]int{}, byID: map[string]*html.Node{},
		storage: map[string]string{}, clock: 1700000000000,
	}
	d.buildIndex(doc)
	d.installHost()
	if _, err := d.vm.RunString(menuDOMShim); err != nil {
		t.Fatalf("install DOM shim: %v", err)
	}
	return d
}

func (d *menuDOM) buildIndex(n *html.Node) {
	// The document node itself is indexed: the shim's root element is the
	// document, so a page-wide querySelector starts above <html> exactly as it
	// does in a browser.
	if n.Type == html.ElementNode || n.Type == html.DocumentNode {
		if _, seen := d.index[n]; !seen {
			d.index[n] = len(d.nodes)
			d.nodes = append(d.nodes, n)
		}
		if id := menuAttr(n, "id"); id != "" {
			if _, taken := d.byID[id]; !taken {
				d.byID[id] = n
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		d.buildIndex(c)
	}
}

// handle converts a node to the int the shim passes around; 0 means "no node".
func (d *menuDOM) handle(n *html.Node) int {
	if n == nil {
		return 0
	}
	i, ok := d.index[n]
	if !ok {
		d.t.Fatalf("node <%s> was not in the parsed document", n.Data)
	}
	return i + 1
}

func (d *menuDOM) node(h int) *html.Node {
	if h == 0 {
		return nil
	}
	if h < 1 || h > len(d.nodes) {
		d.t.Fatalf("DOM handle %d out of range", h)
	}
	return d.nodes[h-1]
}

func (d *menuDOM) value(n *html.Node) goja.Value {
	if n == nil {
		return goja.Null()
	}
	return d.vm.ToValue(d.handle(n))
}

// run executes a script (the page's own, or the shim).
func (d *menuDOM) run(script string) {
	d.t.Helper()
	if _, err := d.vm.RunString(script); err != nil {
		d.t.Fatalf("run the served menu script: %v", err)
	}
}

// dispatch fires an event on `target` and lets it bubble to the document — the
// path the page's own listeners take (both are registered on the document).
func (d *menuDOM) dispatch(eventType string, target *html.Node) {
	d.t.Helper()
	fn, ok := goja.AssertFunction(d.vm.Get("__dispatch"))
	if !ok {
		d.t.Fatal("shim lost __dispatch")
	}
	if _, err := fn(goja.Undefined(), d.vm.ToValue(eventType), d.value(target)); err != nil {
		d.t.Fatalf("dispatch %s on <%s>: %v", eventType, target.Data, err)
	}
}

// flushTimers runs the callbacks the script deferred (the anti-spam wait).
func (d *menuDOM) flushTimers() {
	d.t.Helper()
	for len(d.timers) > 0 {
		next := d.timers[0]
		d.timers = d.timers[1:]
		if _, err := next(goja.Undefined()); err != nil {
			d.t.Fatalf("deferred callback: %v", err)
		}
	}
}

// slug is the menu form slug the page is serving (off #menuSheet).
func (d *menuDOM) slug() string {
	d.t.Helper()
	sheet := d.byID["menuSheet"]
	if sheet == nil {
		d.t.Fatal("the served page has no #menuSheet")
	}
	return menuAttr(sheet, "data-slug")
}

// cart returns the basket the page persisted.
func (d *menuDOM) cart() []map[string]any {
	d.t.Helper()
	raw, ok := d.storage["jf.menu.cart.v2."+d.slug()]
	if !ok || raw == "" {
		return nil
	}
	v, err := d.vm.RunString("(" + raw + ")")
	if err != nil {
		d.t.Fatalf("the page wrote unparsable JSON to localStorage: %v (%s)", err, raw)
	}
	items, ok := v.Export().([]any)
	if !ok {
		d.t.Fatalf("the basket is not an array: %T", v.Export())
	}
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			d.t.Fatalf("basket line is not an object: %T", it)
		}
		out = append(out, m)
	}
	return out
}

// checkoutItems runs the page's own checkout wiring and returns the URLs it
// requested. Nothing is asserted about the network: the shim records the
// request instead of making it.
func (d *menuDOM) checkoutItems() []string {
	d.t.Helper()
	name := d.byID["menuName"]
	email := d.byID["menuEmail"]
	if name == nil || email == nil {
		d.t.Fatal("the served page has no name/email checkout inputs")
	}
	setMenuAttr(name, "value", "Ali")
	setMenuAttr(email, "value", "ali@example.test")
	d.dispatch("submit", d.byID["menuCheckout"])
	d.flushTimers()
	if len(d.fetches) == 0 {
		d.t.Fatal("the page's checkout made no request")
	}
	return d.fetches
}

// postedItems runs the page's own checkout wiring and decodes the JSON body of
// the request it made, returning its items as "product_id|variation_id". Nothing
// is asserted about the network: the shim records the request instead of making
// it, so a rejected line is observable without a server.
func (d *menuDOM) postedItems() []string {
	d.t.Helper()
	body := d.postCheckout()
	v, err := d.vm.RunString("(" + body + ")")
	if err != nil {
		d.t.Fatalf("the checkout body is not valid JSON: %v (%s)", err, body)
	}
	m, ok := v.Export().(map[string]any)
	if !ok {
		d.t.Fatalf("the checkout body is not an object: %T", v.Export())
	}
	raw, ok := m["items"].([]any)
	if !ok {
		d.t.Fatalf("the checkout body has no items array: %v", m)
	}
	out := make([]string, 0, len(raw))
	for _, it := range raw {
		item, ok := it.(map[string]any)
		if !ok {
			d.t.Fatalf("checkout item is not an object: %T", it)
		}
		pid, _ := item["product_id"].(string)
		vid, _ := item["variation_id"].(string)
		out = append(out, pid+"|"+vid)
	}
	return out
}

// postCheckout fills the mandatory buyer fields and submits the page's own
// checkout form, returning the request body it would have sent.
func (d *menuDOM) postCheckout() string {
	d.t.Helper()
	name := d.byID["menuName"]
	email := d.byID["menuEmail"]
	form := d.byID["menuCheckout"]
	if name == nil || email == nil || form == nil {
		d.t.Fatal("the served page has no checkout form")
	}
	setMenuAttr(name, "value", "Ali")
	setMenuAttr(email, "value", "ali@example.test")
	d.dispatch("submit", form)
	d.flushTimers()
	if body, ok := d.storage["__posted_body"]; ok && body != "" {
		return body
	}
	d.t.Fatalf("the page's checkout posted no body (requests: %v, error shown: %q)",
		d.fetches, menuAttr(d.byID["menuErr"], "data-x"))
	return ""
}

// find walks the parsed document for the first element matching a predicate.
func (d *menuDOM) find(match func(*html.Node) bool) *html.Node {
	var out *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if out != nil {
			return
		}
		if n.Type == html.ElementNode && match(n) {
			out = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(d.doc)
	return out
}

// selectorsFor returns every `.varwrap` selector bound to a product id, in
// document order (a dish can legitimately be rendered more than once).
func (d *menuDOM) selectorsFor(productID string) []*html.Node {
	var out []*html.Node
	m := cascadia.MustCompile("select.v")
	cascadia.QueryAll(d.doc, m)
	for _, n := range cascadia.QueryAll(d.doc, m) {
		if menuAttr(n, "data-id") == productID {
			out = append(out, n)
		}
	}
	return out
}

// selectorFor is selectorsFor for the common single-render case.
func (d *menuDOM) selectorFor(productID string) *html.Node {
	sels := d.selectorsFor(productID)
	if len(sels) == 0 {
		return nil
	}
	return sels[0]
}

// buttonFor returns the `.card`/`.row` add button carrying a product id.
func (d *menuDOM) buttonFor(productID string) *html.Node {
	return d.find(func(n *html.Node) bool {
		if n.Data != "button" || menuAttr(n, "data-id") != productID {
			return false
		}
		return menuHasClass(n, "row") || menuHasClass(n, "card")
	})
}

// rowButtonFor returns the list-row add button (`.row[data-id]`) for a product,
// as opposed to the best-seller card (`buttonFor`, which is document-order
// first and is therefore the card when a dish is rendered both ways).
func (d *menuDOM) rowButtonFor(productID string) *html.Node {
	return d.find(func(n *html.Node) bool {
		return n.Data == "button" && menuHasClass(n, "row") && menuAttr(n, "data-id") == productID
	})
}

// cardButtonFor returns the best-seller card button for a product.
func (d *menuDOM) cardButtonFor(productID string) *html.Node {
	return d.find(func(n *html.Node) bool {
		return n.Data == "button" && menuHasClass(n, "card") && menuAttr(n, "data-id") == productID
	})
}

// rowIDsInCategory returns the product ids of the list rows under the category
// container (`.rows`), in document order.
func (d *menuDOM) rowIDsInCategory() []string {
	rows := d.find(func(n *html.Node) bool { return menuHasClass(n, "rows") })
	if rows == nil {
		return nil
	}
	var out []string
	for _, n := range cascadia.QueryAll(rows, cascadia.MustCompile(".row[data-id]")) {
		out = append(out, menuAttr(n, "data-id"))
	}
	return out
}

// chooseVariant marks an option as the buyer's choice, the way clicking it does
// in a browser: the option becomes `selected` and the select takes its value.
// The change event is left to the caller, so a test can fire it explicitly.
func (d *menuDOM) chooseVariant(sel *html.Node, variantID string) {
	d.t.Helper()
	if sel == nil {
		d.t.Fatal("chooseVariant: the selector is missing from the served page")
	}
	found := false
	for c := sel.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode || c.Data != "option" {
			continue
		}
		if menuAttr(c, "value") == variantID {
			found = true
			setMenuAttr(c, "selected", "")
		} else {
			removeMenuAttr(c, "selected")
		}
	}
	if !found {
		d.t.Fatalf("variant %q is not offered by the selector for %q", variantID, menuAttr(sel, "data-id"))
	}
	setMenuAttr(sel, "value", variantID)
}

// ---- DOM attribute helpers (shared with the assertions) ----

func menuAttr(n *html.Node, key string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func menuHasAttr(n *html.Node, key string) bool {
	if n == nil {
		return false
	}
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

func menuHasClass(n *html.Node, class string) bool {
	for _, f := range strings.Fields(menuAttr(n, "class")) {
		if f == class {
			return true
		}
	}
	return false
}

func setMenuAttr(n *html.Node, key, val string) {
	for i, a := range n.Attr {
		if a.Key == key {
			n.Attr[i].Val = val
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: key, Val: val})
}

func removeMenuAttr(n *html.Node, key string) {
	out := n.Attr[:0]
	for _, a := range n.Attr {
		if a.Key != key {
			out = append(out, a)
		}
	}
	n.Attr = out
}

// itemOf returns the nearest ancestor-or-self that is an `.item` wrapper.
func itemOf(n *html.Node) *html.Node {
	for x := n; x != nil; x = x.Parent {
		if x.Type == html.ElementNode && menuHasClass(x, "item") {
			return x
		}
	}
	return nil
}

// hostIn returns the first add button inside a subtree.
func hostIn(n *html.Node, sel string) *html.Node {
	if n == nil {
		return nil
	}
	return cascadia.Query(n, cascadia.MustCompile(sel))
}

// installHost exposes the browser surface as host functions. Each returns a
// goja value; element handles are the ints handle() produces, wrapped into JS
// objects by the shim — so element identity and the property definitions live in
// one place (the shim) and Go stays a tree plus a store.
func (d *menuDOM) installHost() {
	compile := func(sel string) cascadia.Selector {
		m, err := cascadia.Compile(sel)
		if err != nil {
			d.t.Fatalf("bad selector %q from the menu script: %v", sel, err)
		}
		return m
	}
	d.vm.Set("__docHandle", func() int { return d.handle(d.doc) })
	d.vm.Set("__tag", func(h int) string { return d.node(h).Data })
	d.vm.Set("__parent", func(h int) int { return d.handle(d.node(h).Parent) })
	d.vm.Set("__prevElement", func(h int) int {
		for s := d.node(h).PrevSibling; s != nil; s = s.PrevSibling {
			if s.Type == html.ElementNode {
				return d.handle(s)
			}
		}
		return 0
	})
	d.vm.Set("__children", func(h int, tag string) []int {
		var out []int
		for c := d.node(h).FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			if tag != "" && !strings.EqualFold(c.Data, tag) {
				continue
			}
			out = append(out, d.handle(c))
		}
		return out
	})
	d.vm.Set("__attr", func(h int, k string) goja.Value {
		for _, a := range d.node(h).Attr {
			if a.Key == k {
				return d.vm.ToValue(a.Val)
			}
		}
		return goja.Null()
	})
	d.vm.Set("__hasAttr", func(h int, k string) bool { return menuHasAttr(d.node(h), k) })
	d.vm.Set("__setAttr", func(h int, k, v string) { setMenuAttr(d.node(h), k, v) })
	d.vm.Set("__removeAttr", func(h int, k string) { removeMenuAttr(d.node(h), k) })
	d.vm.Set("__query", func(h int, sel string) int {
		n := d.node(h)
		if n == nil {
			return 0
		}
		return d.handle(cascadia.Query(n, compile(sel)))
	})
	d.vm.Set("__queryAll", func(h int, sel string) []int {
		n := d.node(h)
		if n == nil {
			return nil
		}
		var out []int
		for _, hit := range cascadia.QueryAll(n, compile(sel)) {
			out = append(out, d.handle(hit))
		}
		return out
	})
	d.vm.Set("__closest", func(h int, sel string) int {
		m := compile(sel)
		for n := d.node(h); n != nil; n = n.Parent {
			if n.Type == html.ElementNode && m.Match(n) {
				return d.handle(n)
			}
		}
		return 0
	})
	d.vm.Set("__byId", func(id string) int { return d.handle(d.byID[id]) })
	d.vm.Set("__create", func(tag string) int {
		n := &html.Node{Type: html.ElementNode, Data: tag}
		d.index[n] = len(d.nodes)
		d.nodes = append(d.nodes, n)
		return d.handle(n)
	})
	d.vm.Set("__append", func(h, child int) { d.node(h).AppendChild(d.node(child)) })
	d.vm.Set("__removeChild", func(h, child int) { d.node(h).RemoveChild(d.node(child)) })
	d.vm.Set("__text", func(h int) string {
		var sb strings.Builder
		for c := d.node(h).FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode {
				sb.WriteString(c.Data)
			}
		}
		return sb.String()
	})
	d.vm.Set("__setText", func(h int, v string) {
		n := d.node(h)
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			n.RemoveChild(c)
		}
		if v != "" {
			n.AppendChild(&html.Node{Type: html.TextNode, Data: v})
		}
	})
	d.vm.Set("__focus", func(h int) { d.focused = d.node(h) })
	d.vm.Set("__lsGet", func(k string) goja.Value {
		if v, ok := d.storage[k]; ok {
			return d.vm.ToValue(v)
		}
		return goja.Null()
	})
	d.vm.Set("__lsSet", func(k, v string) { d.storage[k] = v })
	d.vm.Set("__lsRemove", func(k string) { delete(d.storage, k) })
	d.vm.Set("__now", func() int64 { d.clock += 1000; return d.clock })
	d.vm.Set("__setTimeout", func(fn goja.Callable) { d.timers = append(d.timers, fn) })
	d.vm.Set("__fetch", func(url string) { d.fetches = append(d.fetches, url) })
	d.vm.Set("__responseBody", func() map[string]any { return d.orderResponse })
	d.vm.Set("__navigate", func(url string) { d.navigate = url })
	d.vm.Set("__alert", func(msg string) { d.t.Fatalf("the menu script alerted: %s", msg) })
}

// menuDOMShim implements the browser surface the menu page's script uses. A
// missing API throws, so new script behaviour cannot quietly pass here.
const menuDOMShim = `
(function(){
  var g = this;
  function unsupported(name){ throw new Error('menu DOM shim does not implement ' + name); }
  var docHandlers = {};
  var cache = {};

  function classes(h){ var c = __attr(h,'class'); return c ? String(c).split(/\s+/) : []; }
  function setClasses(h, parts){
    var clean = [];
    for(var i=0;i<parts.length;i++){ if(parts[i]) clean.push(parts[i]); }
    __setAttr(h, 'class', clean.join(' '));
  }
  function tag(h){ return String(__tag(h)).toUpperCase(); }
  function many(hs){
    var out = [];
    for(var i=0;i<hs.length;i++){ out.push(el(hs[i])); }
    return out;
  }
  function el(h){
    if(!h) return null;
    if(cache[h]) return cache[h];
    cache[h] = build(h);
    return cache[h];
  }
  function fireLocal(h, type, ev){
    var key = '__h_' + type;
    var list = (el(h) || {})[key];
    if(!list) return;
    for(var i=0;i<list.length;i++){ list[i].call(g, ev); }
  }
  g.__dispatch = function(type, target){
    // Go passes the node handle; a real dispatch passes the element itself.
    if(typeof target === 'number'){ target = el(target); }
    var ev = {
      type: type,
      target: target,
      preventDefault: function(){ ev.defaultPrevented = true; },
      stopPropagation: function(){ ev.propagationStopped = true; }
    };
    var h = target && target.__h;
    if(h){ fireLocal(h, type, ev); }
    var list = docHandlers[type] || [];
    for(var i=0;i<list.length;i++){ list[i].call(g, ev); }
    return ev;
  };
  g.__fire = function(type, target){ g.__dispatch(type, target); };
  g.__count = function(type){ return (docHandlers[type]||[]).length; };

  function build(h){
    var o = {
      __h: h,
      getAttribute: function(k){ var v = __attr(h,k); return v===null?null:String(v); },
      setAttribute: function(k,v){ __setAttr(h,k,String(v)); },
      hasAttribute: function(k){ return __hasAttr(h,k); },
      removeAttribute: function(k){ __removeAttr(h,k); },
      querySelector: function(sel){ return el(__query(h,sel)); },
      querySelectorAll: function(sel){ return many(__queryAll(h,sel)); },
      closest: function(sel){ return el(__closest(h,sel)); },
      appendChild: function(child){ __append(h, child.__h); return child; },
      removeChild: function(child){ __removeChild(h, child.__h); return child; },
      focus: function(){ __focus(h); },
      addEventListener: function(type, fn){
        var key = '__h_' + type;
        this[key] = this[key] || [];
        this[key].push(fn);
      },
      classList: {
        add: function(c){ var p=classes(h); if(p.indexOf(c)<0){ p.push(c); setClasses(h,p); } },
        remove: function(c){ setClasses(h, classes(h).filter(function(x){ return x !== c; })); },
        contains: function(c){ return classes(h).indexOf(c) >= 0; },
        toggle: function(c, on){
          if(on === undefined){ on = !this.contains(c); }
          if(on){ this.add(c); } else { this.remove(c); }
          return on;
        }
      },
      style: {},
      getBoundingClientRect: function(){ return unsupported('getBoundingClientRect'); },
      remove: function(){ unsupported('remove'); },
      cloneNode: function(){ return unsupported('cloneNode'); }
    };
    Object.defineProperty(o, 'parentNode', { get: function(){ return el(__parent(h)); } });
    Object.defineProperty(o, 'previousElementSibling', { get: function(){ return el(__prevElement(h)); } });
    Object.defineProperty(o, 'children', { get: function(){ return many(__children(h,'')); } });
    Object.defineProperty(o, 'options', { get: function(){ return many(__children(h,'option')); } });
    Object.defineProperty(o, 'selectedIndex', {
      get: function(){
        var opts = o.options;
        for(var i=0;i<opts.length;i++){ if(opts[i].hasAttribute('selected')) return i; }
        return -1;
      }
    });
    Object.defineProperty(o, 'tagName', { get: function(){ return tag(h); } });
    Object.defineProperty(o, 'nodeName', { get: function(){ return tag(h); } });
    Object.defineProperty(o, 'className', {
      get: function(){ return String(__attr(h,'class')||''); },
      set: function(v){ __setAttr(h,'class',String(v)); }
    });
    Object.defineProperty(o, 'textContent', {
      get: function(){ return __text(h); },
      set: function(v){ __setText(h, String(v)); }
    });
    Object.defineProperty(o, 'innerHTML', {
      get: function(){ return __text(h); },
      set: function(v){ __setText(h, String(v)); }
    });
    Object.defineProperty(o, 'value', {
      get: function(){ return String(__attr(h,'value')||''); },
      set: function(v){ __setAttr(h,'value',String(v)); }
    });
    Object.defineProperty(o, 'type', {
      get: function(){ return String(__attr(h,'type')||''); },
      set: function(v){ __setAttr(h,'type',String(v)); }
    });
    Object.defineProperty(o, 'name', {
      get: function(){ return String(__attr(h,'name')||''); },
      set: function(v){ __setAttr(h,'name',String(v)); }
    });
    Object.defineProperty(o, 'hidden', {
      get: function(){ return __hasAttr(h,'hidden'); },
      set: function(v){ if(v){ __setAttr(h,'hidden',''); } else { __removeAttr(h,'hidden'); } }
    });
    Object.defineProperty(o, 'disabled', {
      get: function(){ return __hasAttr(h,'disabled'); },
      set: function(v){ if(v){ __setAttr(h,'disabled',''); } else { __removeAttr(h,'disabled'); } }
    });
    Object.defineProperty(o, 'checked', {
      get: function(){ return __hasAttr(h,'checked'); },
      set: function(v){ if(v){ __setAttr(h,'checked',''); } else { __removeAttr(h,'checked'); } }
    });
    return o;
  }

  var docNode = el(__docHandle());
  var html = el(__query(__docHandle(), 'html'));
  var document = {
    documentElement: html,
    getElementById: function(id){ return el(__byId(String(id))); },
    createElement: function(t){ return el(__create(String(t))); },
    querySelector: function(sel){ return el(__query(__docHandle(), sel)); },
    querySelectorAll: function(sel){ return many(__queryAll(__docHandle(), sel)); },
    addEventListener: function(type, fn){
      docHandlers[type] = docHandlers[type] || [];
      docHandlers[type].push(fn);
    }
  };
  document.body = html.querySelector('body');

  g.document = document;
  g.window = g;
  g.location = {
    search: '',
    get href(){ return this.__href || ''; },
    set href(v){ this.__href = String(v); __navigate(String(v)); }
  };
  g.localStorage = {
    getItem: function(k){ var v = __lsGet(String(k)); return v===null?null:String(v); },
    setItem: function(k,v){ __lsSet(String(k), String(v)); },
    removeItem: function(k){ __lsRemove(String(k)); },
    clear: function(){ unsupported('localStorage.clear'); }
  };
  g.URLSearchParams = function(qs){
    var params = {};
    String(qs||'').replace(/^\?/,'').split('&').forEach(function(pair){
      if(!pair) return;
      var bits = pair.split('=');
      params[decodeURIComponent(bits[0])] = decodeURIComponent((bits[1]||'').replace(/\+/g,' '));
    });
    return { get: function(k){
      return Object.prototype.hasOwnProperty.call(params,k) ? params[k] : null;
    } };
  };
  g.Date.now = function(){ return __now(); };
  g.setTimeout = function(fn){ __setTimeout(fn); return 0; };
  // A synchronous promise stand-in, so the page's own .then chains run and the
  // request it would have made is observable. The order response is an empty
  // object by default: no checkout_url (so the page takes its failure path and
  // keeps the basket) and no error (so nothing is asserted about prose here).
  function promiseOf(value){
    var p = {
      then: function(fn){
        if(typeof fn !== 'function'){ return p; }
        var out = fn(value);
        if(out && typeof out.then === 'function'){ return out; }
        return promiseOf(out);
      },
      catch: function(){ return p; }
    };
    return p;
  }
  g.fetch = function(url, opts){
    __fetch(String(url));
    if(opts && typeof opts.body === 'string'){ __lsSet('__posted_body', opts.body); }
    var body = __responseBody();
    return promiseOf({
      ok: true,
      status: 200,
      json: function(){ return promiseOf(body); }
    });
  };
  g.alert = function(m){ __alert(String(m)); };
  g.console = { log: function(){}, error: function(){}, warn: function(){} };
  g.__unsupported = unsupported;
})();
`
