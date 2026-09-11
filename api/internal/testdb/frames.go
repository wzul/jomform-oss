package testdb

import (
	"runtime"
	"strings"
)

// repoMarker identifies frames that belong to this repository, so the walk
// ignores standard-library frames such as sync.(*Once).doSlow that sit between
// Open and the test package.
const repoMarker = "/jomform/"

// pkgFromFrames walks the call stack and returns the short name of the first
// frame that belongs to a JomForm package other than testdb. Because
// Open/Connect are called from the test package, that frame is the package
// under test (e.g. "dbstore", "httpapi", "payments") — which is what the
// scratch schema must be named after, since packages run in parallel.
func pkgFromFrames() string {
	pcs := make([]uintptr, 32)
	n := runtime.Callers(2, pcs)
	if n == 0 {
		return "pkg"
	}
	frames := runtime.CallersFrames(pcs[:n])
	for {
		f, more := frames.Next()
		fn := f.Function
		// Only consider frames inside this repo, and skip testdb itself.
		if strings.Contains(fn, repoMarker) && !strings.Contains(fn, "/internal/testdb.") {
			// fn looks like
			// github.com/wzul/jomform/api/internal/dbstore.TestFoo
			// or .../internal/dbstore.init.0.func1
			if i := strings.LastIndex(fn, "/"); i >= 0 {
				fn = fn[i+1:]
			}
			if i := strings.IndexByte(fn, '.'); i > 0 {
				return fn[:i]
			}
		}
		if !more {
			break
		}
	}
	return "pkg"
}
