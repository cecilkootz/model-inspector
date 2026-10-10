package scan

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"origin.cursor.com/cecilkootz/model-inspector/internal/fixture"
	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
)

func timedLoad(t *testing.T, dir string) *model.Model {
	t.Helper()
	done := make(chan struct{})
	var m *model.Model
	var err error
	go func() {
		defer close(done)
		m, err = model.Load(dir)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("Load hung on %s", dir)
	}
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return m
}

func TestQA_DevZeroIndexAndShard(t *testing.T) {
	if _, err := os.Stat("/dev/zero"); err != nil {
		t.Skip("/dev/zero not available")
	}
	good, _ := mustShard(t, []fixture.Tensor{ten("w", "F32", 1)})
	t.Run("index", func(t *testing.T) {
		d := t.TempDir()
		if err := os.WriteFile(filepath.Join(d, "a.safetensors"), good, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("/dev/zero", filepath.Join(d, "model.safetensors.index.json")); err != nil {
			t.Fatal(err)
		}
		m := timedLoad(t, d)
		res := Run(m)
		if res.Overall == StatusPass {
			t.Fatalf("PASS with index -> /dev/zero: %s", res.Checks[0].Summary)
		}
	})
	t.Run("shard", func(t *testing.T) {
		d := t.TempDir()
		if err := os.Symlink("/dev/zero", filepath.Join(d, "model.safetensors")); err != nil {
			t.Fatal(err)
		}
		m := timedLoad(t, d)
		if res := Run(m); res.Overall != StatusFail {
			t.Fatalf("shard -> /dev/zero overall %s", res.Overall)
		}
	})
	t.Run("config", func(t *testing.T) {
		d := t.TempDir()
		if err := os.WriteFile(filepath.Join(d, "a.safetensors"), good, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("/dev/zero", filepath.Join(d, "config.json")); err != nil {
			t.Fatal(err)
		}
		m := timedLoad(t, d)
		if res := Run(m); res.Overall == StatusPass {
			t.Fatalf("PASS with config.json -> /dev/zero")
		}
	})
}

func TestQA_FIFOShardFailsScan(t *testing.T) {
	d := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(d, "model.safetensors"), 0o644); err != nil {
		t.Skip(err)
	}
	m := timedLoad(t, d)
	res := Run(m)
	if res.Overall != StatusFail {
		t.Fatalf("FIFO shard overall %s (%s)", res.Overall, res.Checks[0].Summary)
	}
}

func TestQA_BracketedDirKeepsIndex(t *testing.T) {
	good, n := mustShard(t, []fixture.Tensor{ten("w", "F32", 1)})
	for _, sub := range []string{"plain", "Qwen3 [4bit]", "m[", "m*", "m?", `m\x`} {
		t.Run(sub, func(t *testing.T) {
			base := filepath.Join(t.TempDir(), sub)
			if err := os.MkdirAll(base, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(base, "model-00001-of-00002.safetensors"), good, 0o644); err != nil {
				t.Fatal(err)
			}
			idx := mustIndex(t, n, map[string]string{
				"w": "model-00001-of-00002.safetensors",
				"v": "model-00002-of-00002.safetensors",
			})
			if err := os.WriteFile(filepath.Join(base, "model.safetensors.index.json"), idx, 0o644); err != nil {
				t.Fatal(err)
			}
			m, err := model.Load(base)
			if err != nil {
				t.Fatalf("load %q: %v", sub, err)
			}
			if m.Index == nil {
				t.Fatalf("index lost in %q: err=%v warnings=%v", sub, m.IndexErr, m.Warnings)
			}
			res := Run(m)
			if res.Overall == StatusPass {
				t.Fatalf("%s PASS (%s)", sub, res.Checks[0].Summary)
			}
			// The missing shard is an index finding. Checks[0] is header-parse
			// after the check list was split, so the assertion is pinned to
			// index-shard-consistency rather than searched across every check.
			indexCheck := findCheck(t, res, CheckIndex)
			blob := indexCheck.Summary + "\n" + strings.Join(indexCheck.Details, "\n")
			if strings.Contains(blob, "no index file") {
				t.Fatalf("%s summary claims no index: %s", sub, blob)
			}
			if !strings.Contains(blob, "model-00002-of-00002.safetensors") {
				t.Fatalf("%s did not report the missing shard:\n%s", sub, blob)
			}
			files := findCheck(t, res, CheckFiles)
			if files.Status == StatusPass {
				t.Fatalf("%s file-presence passed: %+v", sub, files)
			}
		})
	}
}

func TestQA_AbsoluteShardName(t *testing.T) {
	good, _ := mustShard(t, []fixture.Tensor{ten("w", "F32", 1)})
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "a.safetensors"), good, 0o644); err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(t.TempDir(), "abs.safetensors")
	idx := []byte(`{"weight_map":{"w":` + strconv.Quote(abs) + `}}`)
	if err := os.WriteFile(filepath.Join(d, "model.safetensors.index.json"), idx, 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := model.Load(d)
	if err != nil {
		t.Fatal(err)
	}
	res := Run(m)
	if res.Overall == StatusPass {
		t.Fatalf("PASS with absolute shard name: %s", res.Checks[0].Summary)
	}
	// Pinned to the index check. Checks[0] is header-parse, which does not
	// carry shard-name findings.
	detailsContain(t, findCheck(t, res, CheckIndex), "not a local file name")
	files := findCheck(t, res, CheckFiles)
	if files.Status == StatusPass {
		t.Fatalf("file-presence passed with an absolute shard name: %+v", files)
	}
}

func TestQA_DuplicateIndexKey(t *testing.T) {
	good, _ := mustShard(t, []fixture.Tensor{ten("w", "F32", 1)})
	m := loadDir(t, map[string][]byte{
		"a.safetensors":                good,
		"model.safetensors.index.json": []byte(`{"weight_map":{"w":"a.safetensors","w":"zz.safetensors"}}`),
	})
	if m.Index != nil {
		t.Fatalf("duplicate key index accepted: %+v", m.Index)
	}
	if m.IndexErr == nil || !strings.Contains(m.IndexErr.Error(), "duplicate") {
		t.Fatalf("IndexErr = %v", m.IndexErr)
	}
	if res := Run(m); res.Overall != StatusFail {
		t.Fatalf("overall %s", res.Overall)
	}
}

func TestQA_WalkCapsDoNotPass(t *testing.T) {
	t.Run("depth", func(t *testing.T) {
		d := t.TempDir()
		p := d
		for i := 0; i < 12; i++ {
			p = filepath.Join(p, "d")
		}
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "x.safetensors"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		m, err := model.Load(d)
		if err != nil {
			t.Fatal(err)
		}
		if !m.Partial {
			t.Fatal("deep tree was not marked partial")
		}
		if res := Run(m); res.Overall == StatusPass {
			t.Fatalf("PASS on a walk past the depth cap: %s", res.Checks[0].Summary)
		}
	})
	t.Run("directories", func(t *testing.T) {
		d := t.TempDir()
		// maxFiles is 20000 and counts directories. One past the cap must not pass.
		const n = 20001
		for i := 0; i < n; i++ {
			if err := os.Mkdir(filepath.Join(d, fmt.Sprintf("d%05d", i)), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		m, err := model.Load(d)
		if err != nil {
			t.Fatal(err)
		}
		if !m.Partial {
			t.Fatalf("20001 empty directories were not capped (files=%d)", len(m.Files))
		}
		if res := Run(m); res.Overall == StatusPass {
			t.Fatal("PASS after the directory walk cap")
		}
	})
	t.Run("files", func(t *testing.T) {
		d := t.TempDir()
		const n = 20001
		for i := 0; i < n; i++ {
			if err := os.WriteFile(filepath.Join(d, fmt.Sprintf("f%05d", i)), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		m, err := model.Load(d)
		if err != nil {
			t.Fatal(err)
		}
		if !m.Partial || len(m.Files) > 20000 {
			t.Fatalf("partial=%v files=%d", m.Partial, len(m.Files))
		}
		if res := Run(m); res.Overall == StatusPass {
			t.Fatal("PASS after the file walk cap")
		}
	})
}

func TestQA_UnreadableDirDoesNotAbort(t *testing.T) {
	good, _ := mustShard(t, []fixture.Tensor{ten("w", "F32", 1)})
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "a.safetensors"), good, 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(d, "x\x1b]0;PWNED\x07")
	if err := os.Mkdir(sub, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sub, 0o755) })
	m := timedLoad(t, d)
	if !m.Partial {
		t.Fatal("unreadable directory did not mark the scan partial")
	}
	res := Run(m)
	if res.Overall == StatusPass {
		t.Fatalf("PASS after a walk error: %s", res.Checks[0].Summary)
	}
	blob := strings.Join(m.Warnings, "\n")
	if !strings.Contains(blob, "PWNED") {
		t.Fatalf("walk warning missing: %q", m.Warnings)
	}
}
