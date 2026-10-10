package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"origin.cursor.com/cecilkootz/model-inspector/internal/fixture"
	"origin.cursor.com/cecilkootz/model-inspector/internal/report"
	"origin.cursor.com/cecilkootz/model-inspector/internal/version"
)

func TestInspectJSONOnHubFixture(t *testing.T) {
	files, tensorBytes, err := fixture.TinyQwen()
	if err != nil {
		t.Fatal(err)
	}
	const (
		repo   = "Qwen/Tiny-Next"
		commit = "0123456789abcdef0123456789abcdef01234567"
	)
	hub := t.TempDir()
	if _, err := fixture.MaterializeHub(hub, repo, commit, files); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := capture(t, func() int {
		return run([]string{"inspect", repo, "--hub-dir", hub, "--json"})
	})
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	var doc report.Document
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout)
	}
	if doc.SchemaVersion != 1 || doc.Tool.Name != version.Name || doc.Tool.Version != version.Version {
		t.Errorf("tool = %+v schema %d", doc.Tool, doc.SchemaVersion)
	}
	if doc.Source.RepoID == nil || *doc.Source.RepoID != repo || doc.Source.Revision == nil || *doc.Source.Revision != commit {
		t.Errorf("source = %+v", doc.Source)
	}
	if doc.Source.RevisionSource != "hf_cache_ref" {
		t.Errorf("revision_source = %s", doc.Source.RevisionSource)
	}
	if doc.Format.Name != "safetensors" || doc.Format.Variant != "mlx" || !doc.Format.Supported {
		t.Errorf("format = %+v", doc.Format)
	}
	if doc.Files.ShardCount != 2 {
		t.Errorf("shards = %d", doc.Files.ShardCount)
	}
	if doc.Architecture.LayerCount != 2 || doc.Architecture.LayersWithRoutedExperts != 1 {
		t.Errorf("architecture = %+v", doc.Architecture)
	}
	if doc.Tensors.Count != 25 || doc.Tensors.IndexTotalSizeBytes == nil || *doc.Tensors.IndexTotalSizeBytes != tensorBytes {
		t.Errorf("tensors = %+v, payload %d", doc.Tensors, tensorBytes)
	}
	if doc.Scan.Overall != "pass" {
		t.Errorf("scan = %s", doc.Scan.Overall)
	}
	if doc.Quantization == nil || doc.Quantization.Bits != 4 || doc.Quantization.GroupSize != 32 {
		t.Errorf("quant = %+v", doc.Quantization)
	}
	if len(doc.LoadWarnings) != 0 {
		t.Errorf("warnings = %v", doc.LoadWarnings)
	}
	if !strings.Contains(stdout, `"code": null`) {
		t.Errorf("code should be null:\n%s", stdout)
	}
	got := map[string]bool{}
	for _, g := range doc.Groups {
		got[g.Key] = true
		if g.RoutedExperts != nil {
			t.Errorf("group %s routed_experts = %d", g.Key, *g.RoutedExperts)
		}
	}
	for _, name := range []string{"embeddings", "ngram", "norms", "hyper_connection", "lm_head", "mtp", "vision"} {
		if !got[name] {
			t.Errorf("missing group %s in %+v", name, got)
		}
	}
	if got["other"] {
		t.Errorf("mixer tensors fell through to other: %+v", got)
	}
}

func TestInspectTextSanitizesFormat(t *testing.T) {
	hdr := `{"__metadata__":{"format":"mlx\u001b]0;pwned\u0007"},"w":{"dtype":"F32","shape":[1],"data_offsets":[0,4]}}`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "model.safetensors"), fixture.EncodeHeader([]byte(hdr), []byte{0, 0, 0, 0}), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := capture(t, func() int {
		return run([]string{"inspect", dir})
	})
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	if strings.Contains(stdout, "\x1b") {
		t.Fatalf("text output contains ESC:\n%s", stdout)
	}
	if !strings.Contains(stdout, "<U+001B>") {
		t.Fatalf("expected a visible token:\n%s", stdout)
	}
	if strings.Contains(stdout, " @ ") {
		t.Fatalf("empty revision rendered as @:\n%s", stdout)
	}
	if !strings.Contains(stdout, "revision") || !strings.Contains(stdout, "—") {
		t.Fatalf("expected an explicit empty revision:\n%s", stdout)
	}
}

func TestInspectJSONEscapesControls(t *testing.T) {
	hdr := `{"__metadata__":{"format":"mlx\u001b]0;pwned\u0007"},"w":{"dtype":"F32","shape":[1],"data_offsets":[0,4]}}`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "model.safetensors"), fixture.EncodeHeader([]byte(hdr), []byte{0, 0, 0, 0}), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := capture(t, func() int {
		return run([]string{"inspect", "--path", dir, "--json"})
	})
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	if bytes.Contains([]byte(stdout), []byte{0x1b}) {
		t.Fatalf("json bytes contain a raw ESC")
	}
	if !strings.Contains(stdout, `\u001b`) {
		t.Fatalf("expected \\u001b in json:\n%s", stdout)
	}
}

func TestInspectMissingRepo(t *testing.T) {
	stdout, stderr, code := capture(t, func() int {
		return run([]string{"inspect", "Qwen/Missing", "--hub-dir", t.TempDir(), "--json"})
	})
	if code != 3 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q", stdout)
	}
	if !strings.Contains(stderr, "not found in HF cache") || !strings.Contains(stderr, "--hub-dir") {
		t.Fatalf("stderr = %s", stderr)
	}
}

func TestAmbiguousTargetRefused(t *testing.T) {
	files, err := fixture.TiedEmbed()
	if err != nil {
		t.Fatal(err)
	}
	for i := range files {
		if files[i].Name == "config.json" {
			files[i].Data = []byte(`{"model_type":"localshadow","tie_word_embeddings":true}`)
		}
	}
	const (
		repo   = "Org/Model-4bit"
		commit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	hub := t.TempDir()
	hubFiles, _, err := fixture.TinyQwen()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.MaterializeHub(hub, repo, commit, hubFiles); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	local := filepath.Join(cwd, "Org", "Model-4bit")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := fixture.Materialize(local, files); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)

	stdout, stderr, code := capture(t, func() int {
		return run([]string{"inspect", repo, "--hub-dir", hub, "--json"})
	})
	if code != 2 {
		t.Fatalf("bare exit %d\n%s\n%s", code, stderr, stdout)
	}
	if stdout != "" {
		t.Fatalf("ambiguous target wrote a summary:\n%s", stdout)
	}
	if !strings.Contains(stderr, "--path") || !strings.Contains(stderr, "--repo") {
		t.Fatalf("stderr = %s", stderr)
	}

	repoOut, repoErr, repoCode := capture(t, func() int {
		return run([]string{"inspect", "--repo", repo, "--hub-dir", hub, "--json"})
	})
	if repoCode != 0 {
		t.Fatalf("repo exit %d\n%s", repoCode, repoErr)
	}
	if !strings.Contains(repoOut, "qwen3_5") || strings.Contains(repoOut, "localshadow") {
		t.Fatalf("--repo did not read the hub:\n%s", repoOut)
	}

	pathOut, pathErr, pathCode := capture(t, func() int {
		return run([]string{"inspect", "--path", repo, "--hub-dir", hub, "--json"})
	})
	if pathCode != 0 {
		t.Fatalf("path exit %d\n%s", pathCode, pathErr)
	}
	if !strings.Contains(pathOut, "localshadow") || strings.Contains(pathOut, "qwen3_5") {
		t.Fatalf("--path did not read the local directory:\n%s", pathOut)
	}

	dotOut, dotErr, dotCode := capture(t, func() int {
		return run([]string{"inspect", "./" + repo, "--hub-dir", hub, "--json"})
	})
	if dotCode != 0 {
		t.Fatalf("./ exit %d\n%s", dotCode, dotErr)
	}
	if !strings.Contains(dotOut, "localshadow") {
		t.Fatalf("./ prefix did not stay a path:\n%s", dotOut)
	}
}

func TestExplicitPathDoesNotFallThroughToHub(t *testing.T) {
	const (
		repo   = "Org/Present"
		commit = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	hub := t.TempDir()
	files, _, err := fixture.TinyQwen()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.MaterializeHub(hub, repo, commit, files); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	stdout, stderr, code := capture(t, func() int {
		return run([]string{"inspect", "./" + repo, "--hub-dir", hub})
	})
	if code != 3 {
		t.Fatalf("exit %d\n%s\n%s", code, stderr, stdout)
	}
	if strings.Contains(stdout, "qwen3_5") {
		t.Fatalf("./Org/Present fell through to the hub:\n%s", stdout)
	}
}

func TestStrictPromotesWarnToExit1(t *testing.T) {
	files, err := fixture.CodeExec()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := fixture.Materialize(dir, files); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := capture(t, func() int {
		return run([]string{"inspect", "--path", dir, "--json"})
	})
	if code != 0 {
		t.Fatalf("warn exit %d\n%s", code, stderr)
	}
	var doc report.Document
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Scan.Overall != "warn" || doc.Scan.Counts.Warn == 0 || doc.Scan.Counts.Fail != 0 {
		t.Fatalf("scan = %+v counts %+v", doc.Scan.Overall, doc.Scan.Counts)
	}
	var raw, eff string
	for _, c := range doc.Scan.Checks {
		if c.ID == "code-exec-formats" {
			raw, eff = c.Status, c.EffectiveStatus
		}
	}
	if raw != "warn" || eff != "warn" {
		t.Fatalf("code-exec status %s effective %s", raw, eff)
	}

	stdout, stderr, code = capture(t, func() int {
		return run([]string{"inspect", "--path", dir, "--json", "--strict"})
	})
	if code != 1 {
		t.Fatalf("strict exit %d\n%s", code, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Scan.Overall != "fail" || !doc.Scan.Strict {
		t.Fatalf("strict overall %s strict %v", doc.Scan.Overall, doc.Scan.Strict)
	}
	if doc.Scan.Counts.Warn == 0 || doc.Scan.Counts.Fail != 0 {
		t.Fatalf("strict changed raw counts: %+v", doc.Scan.Counts)
	}
	for _, c := range doc.Scan.Checks {
		if c.ID == "code-exec-formats" && (c.Status != "warn" || c.EffectiveStatus != "fail") {
			t.Fatalf("strict check = %+v", c)
		}
	}
}

func TestGGUFExits3AfterSummary(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "model.gguf"), []byte("GGUF\x00not-a-real-header"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := capture(t, func() int {
		return run([]string{"inspect", "--path", dir})
	})
	if code != 3 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "gguf") || !strings.Contains(stderr, "not supported") {
		t.Fatalf("stdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestVersionAndUsage(t *testing.T) {
	want := version.String() + "\n"
	for _, arg := range []string{"version", "--version", "-version"} {
		stdout, _, code := capture(t, func() int { return run([]string{arg}) })
		if code != 0 || stdout != want {
			t.Fatalf("%s exit %d out %q", arg, code, stdout)
		}
	}
	stdout, stderr, code := capture(t, func() int { return run(nil) })
	if code != 2 || !strings.Contains(stderr, "Usage:") {
		t.Fatalf("no args exit %d\n%s", code, stderr)
	}
	_, stderr, code = capture(t, func() int { return run([]string{"inspect", "--nope", "x"}) })
	if code != 2 || !strings.Contains(stderr, "unknown flag") {
		t.Fatalf("flag exit %d\n%s", code, stderr)
	}
	_, stderr, code = capture(t, func() int {
		return run([]string{"inspect", "--path", "a", "--repo", "Org/Name"})
	})
	if code != 2 {
		t.Fatalf("both flags exit %d\n%s", code, stderr)
	}
	stdout, _, code = capture(t, func() int { return run([]string{"--help"}) })
	if code != 0 || !strings.Contains(stdout, "--path") || !strings.Contains(stdout, "--repo") {
		t.Fatalf("help exit %d\n%s", code, stdout)
	}
}

func TestNonTTYTUIFallsBackToText(t *testing.T) {
	files, _, err := fixture.TinyQwen()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := fixture.Materialize(dir, files); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := capture(t, func() int {
		return run([]string{"tui", "--path", dir, "--no-color"})
	})
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	if strings.Contains(stdout, "\x1b") {
		t.Fatalf("fallback text contains ESC")
	}
	if !strings.Contains(stdout, "safetensors") || !strings.Contains(stdout, "checks") {
		t.Fatalf("expected a text summary:\n%s", stdout)
	}
}

func TestStderrSanitizesPath(t *testing.T) {
	stdout, stderr, code := capture(t, func() int {
		return run([]string{"inspect", "--path", "bad\x1b[2Jname"})
	})
	if code != 3 {
		t.Fatalf("exit %d", code)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q", stdout)
	}
	if strings.Contains(stderr, "\x1b") || !strings.Contains(stderr, "<U+001B>") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func capture(t *testing.T, fn func() int) (stdout, stderr string, code int) {
	t.Helper()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	var outBuf, errBuf bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(&outBuf, outR)
	}()
	errDone := make(chan struct{})
	go func() {
		defer close(errDone)
		_, _ = io.Copy(&errBuf, errR)
	}()
	defer func() {
		_ = outW.Close()
		_ = errW.Close()
		os.Stdout, os.Stderr = origOut, origErr
		<-done
		<-errDone
		stdout = outBuf.String()
		stderr = errBuf.String()
	}()
	code = fn()
	return stdout, stderr, code
}
