package hfcache

import "testing"

func FuzzTarget(f *testing.F) {
	f.Add("org/name")
	f.Add("org/name@main")
	f.Add("org/name@0123456789abcdef0123456789abcdef01234567")
	f.Add("./org/name")
	f.Add("../org/name")
	f.Add("~/org/name")
	f.Add("")
	f.Add("org/name/")
	f.Fuzz(func(t *testing.T, arg string) {
		if len(arg) > 256 {
			return
		}
		hub := t.TempDir()
		for _, mode := range []TargetMode{TargetAuto, TargetPath, TargetRepo} {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("ResolveTarget mode %d arg %q panicked: %v", mode, arg, r)
					}
				}()
				_, _ = ResolveTarget(arg, hub, mode)
			}()
		}
	})
}
