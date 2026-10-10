// Command model-inspector inspects local model weights (headers and small
// JSON files only) in a terminal UI or as text or JSON.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mattn/go-isatty"

	"origin.cursor.com/cecilkootz/model-inspector/internal/hfcache"
	"origin.cursor.com/cecilkootz/model-inspector/internal/model"
	"origin.cursor.com/cecilkootz/model-inspector/internal/report"
	"origin.cursor.com/cecilkootz/model-inspector/internal/sanitize"
	"origin.cursor.com/cecilkootz/model-inspector/internal/scan"
	"origin.cursor.com/cecilkootz/model-inspector/internal/tui"
	"origin.cursor.com/cecilkootz/model-inspector/internal/version"
)

const usage = `model-inspector: inspect local model weights without loading them.

Usage:
  model-inspector [tui] (--path <dir> | --repo <id> | <target>)
  model-inspector inspect (--path <dir> | --repo <id> | <target>) [--json]
  model-inspector version

<target> is a model directory or an HF repo id (org/name, optionally @ref
or @commit) resolved through the hub cache.

A bare org/name that is also a directory in the current working directory
is refused: it is not opened as a path and it is not looked up in the hub.
The same check uses the id with @ref removed, so org/name@main is refused
when ./org/name is a directory. A broken symlink named like org/name is
refused the same way. Pass --path to read that directory, or --repo to
read the hub cache.
A path that is absolute, starts with ./ or ../, or has a trailing slash is
always a directory and is never looked up in the hub, even when the hub
has a repo of a similar name. A bare org/name that is not a local directory
is a repo id. A leading ~ is not expanded.

When stdout is not a terminal, the default command prints the text summary
instead of opening the TUI. --json prints JSON instead of opening the TUI.

Flags:
  --path <dir>      read this directory; never a hub lookup
  --repo <id>       read this HF repo id; never the working directory
  --json            print schema_version 1 JSON
  --strict          treat scan warnings as failures (exit 1)
  --no-color        disable color (same as NO_COLOR)
  --hub-dir <dir>   HF hub cache (default: $HF_HUB_CACHE, $HF_HOME/hub, ~/.cache/huggingface/hub)
  --version         print the version and commit
  -h, --help        print this help

--path, --repo, and a positional target are mutually exclusive.

Exit codes:
  0  inspected, and no check failed (warnings are fine unless --strict). The interactive TUI exits 0. When stdout is not a terminal, the default command is the text summary and uses the inspect codes below.
  1  inspected, and a check failed (or a warning under --strict); the summary is still printed
  2  usage: bad flags, a missing or empty target, an empty --path or --repo, an invalid repo id, or an ambiguous path/repo id
  3  the target could not be inspected (missing, not a directory, GGUF-only, or no weights)
`

func main() {
	os.Exit(run(os.Args[1:]))
}

type options struct {
	json    bool
	strict  bool
	noColor bool
	version bool
	hubDir  string
	target  string
	mode    hfcache.TargetMode
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	if args[0] == "version" || args[0] == "--version" || args[0] == "-version" {
		fmt.Println(version.String())
		return 0
	}
	cmd := "tui"
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Print(usage)
		return 0
	case "inspect", "tui":
		cmd, args = args[0], args[1:]
	}
	opts, err := parseArgs(args)
	if err != nil {
		if errors.Is(err, errHelp) {
			fmt.Print(usage)
			return 0
		}
		fail(err)
		return 2
	}
	if opts.version {
		fmt.Println(version.String())
		return 0
	}
	if opts.noColor {
		tui.SetNoColor(true)
	}
	start := time.Now()
	m, fromCache, err := load(opts)
	if err != nil {
		return resolveExit(err)
	}
	res := scan.RunOptions(m, scan.Options{Strict: opts.strict})
	doc := report.Build(m, res, fromCache, time.Since(start))
	// Build can saturate a group total and mark Partial after the scan that
	// produced res. Re-run so the TUI shows the same verdict as doc.Scan.
	if m.Partial && res.Overall != scan.StatusFail {
		res = scan.RunOptions(m, scan.Options{Strict: opts.strict})
	}
	interactive := cmd == "tui" && !opts.json && isatty.IsTerminal(os.Stdout.Fd())
	if interactive {
		if err := tui.Run(m, res); err != nil {
			fail(err)
			return 1
		}
		return 0
	}
	if opts.json {
		b, err := report.Marshal(doc)
		if err != nil {
			fail(err)
			return 3
		}
		if _, err := os.Stdout.Write(b); err != nil {
			fail(err)
			return 3
		}
	} else {
		fmt.Fprint(os.Stdout, report.Text(doc))
	}
	if msg, blocked := report.Blocked(doc); blocked {
		fail(errors.New(msg))
		return 3
	}
	// The document's scan is the source of truth: Build re-runs the checks
	// when a total overflowed while the summary was assembled, so partial
	// and the exit code cannot disagree with the printed result.
	if doc.Scan.Overall == string(scan.StatusFail) {
		return 1
	}
	return 0
}

var errHelp = errors.New("help")

func parseArgs(args []string) (options, error) {
	o := options{hubDir: hfcache.DefaultHubDir(), mode: hfcache.TargetAuto}
	var pos []string
	var path, repo string
	var pathSet, repoSet bool
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--json" || a == "-json":
			o.json = true
		case a == "--strict":
			o.strict = true
		case a == "--no-color":
			o.noColor = true
		case a == "--version" || a == "-version":
			o.version = true
		case a == "-h" || a == "--help" || a == "help":
			return o, errHelp
		case a == "--hub-dir" || a == "-hub-dir":
			v, next, err := takeValue(args, i, "--hub-dir")
			if err != nil {
				return o, err
			}
			i = next
			o.hubDir = v
		case strings.HasPrefix(a, "--hub-dir="):
			o.hubDir = strings.TrimPrefix(a, "--hub-dir=")
		case a == "--path":
			pathSet = true
			v, next, err := takeValue(args, i, "--path")
			if err != nil {
				return o, err
			}
			i = next
			path = v
		case strings.HasPrefix(a, "--path="):
			pathSet = true
			path = strings.TrimPrefix(a, "--path=")
		case a == "--repo":
			repoSet = true
			v, next, err := takeValue(args, i, "--repo")
			if err != nil {
				return o, err
			}
			i = next
			repo = v
		case strings.HasPrefix(a, "--repo="):
			repoSet = true
			repo = strings.TrimPrefix(a, "--repo=")
		case strings.HasPrefix(a, "-"):
			return o, fmt.Errorf("unknown flag %s", a)
		default:
			pos = append(pos, a)
		}
	}
	if pathSet && path == "" {
		return o, errors.New("--path needs a value")
	}
	if repoSet && repo == "" {
		return o, errors.New("--repo needs a value")
	}
	n := 0
	if path != "" {
		n++
	}
	if repo != "" {
		n++
	}
	if len(pos) > 1 {
		return o, fmt.Errorf("expected exactly one model path or repo id, got %d", len(pos))
	}
	if len(pos) == 1 {
		if pos[0] == "" {
			return o, errors.New("missing model path or repo id")
		}
		n++
	}
	if n == 0 {
		return o, errors.New("missing model path or repo id")
	}
	if n > 1 {
		return o, errors.New("pass only one of a positional target, --path, or --repo")
	}
	switch {
	case path != "":
		o.mode = hfcache.TargetPath
		o.target = path
	case repo != "":
		o.mode = hfcache.TargetRepo
		o.target = repo
	default:
		o.target = pos[0]
	}
	return o, nil
}

func takeValue(args []string, i int, name string) (string, int, error) {
	if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
		return "", i, fmt.Errorf("%s needs a value", name)
	}
	return args[i+1], i + 1, nil
}

func load(o options) (*model.Model, bool, error) {
	r, err := hfcache.ResolveTarget(o.target, o.hubDir, o.mode)
	if err != nil {
		return nil, false, err
	}
	m, err := model.Load(r.Dir)
	if err != nil {
		return nil, false, err
	}
	m.RepoID, m.Revision = r.RepoID, r.Revision
	return m, r.FromCache, nil
}

func resolveExit(err error) int {
	msg := err.Error()
	code := 3
	if errors.Is(err, hfcache.ErrAmbiguousTarget) || usageResolve(msg) {
		code = 2
	}
	fail(err)
	if strings.Contains(msg, "not found in HF cache") {
		fmt.Fprintln(os.Stderr, "(try --hub-dir or a local path)")
	}
	return code
}

func usageResolve(msg string) bool {
	return strings.Contains(msg, "neither a directory nor") || strings.Contains(msg, "invalid ref")
}

func fail(err error) {
	writeError(os.Stderr, err)
}

// writeError prints err to w with file-derived controls tokenized.
func writeError(w io.Writer, err error) {
	fmt.Fprintln(w, "model-inspector:", sanitize.From(err.Error()))
}
