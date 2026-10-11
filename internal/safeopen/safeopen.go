// Package safeopen reads untrusted filesystem paths.
//
// Open Lstats (and Stats through a symlink) and refuses anything that is
// not a regular file before the descriptor exists. It then opens that path,
// fstats the same fd, and requires a regular file again: the pre-check is
// not a substitute for the post-open check. Reads go through
// io.LimitReader(max+1) so a file past the cap is detected instead of
// silently truncated.
package safeopen

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/cecilkootz/model-inspector/internal/fileerr"
)

// File is an opened regular file. Read stops after max+1 bytes.
type File struct {
	f    *os.File
	r    io.Reader
	size int64
	max  int64
	name string
}

// Open opens path and requires the opened descriptor to be a regular file.
// Reads are limited to max+1 bytes.
func Open(path string, max int64) (*File, error) {
	return OpenUnder(path, max)
}

// OpenUnder is Open plus a containment check. When roots is non-empty, the
// resolved path of the opened descriptor must sit inside one of them (the
// model directory, or an HF blobs directory). Symlinks are followed by
// open; the check uses the descriptor, not a prior path stat.
func OpenUnder(path string, max int64, roots ...string) (*File, error) {
	f, err := openUnder(path, max, roots...)
	if err != nil {
		return nil, fileerr.Error(err)
	}
	return f, nil
}

func openUnder(path string, max int64, roots ...string) (*File, error) {
	if max < 0 {
		return nil, fmt.Errorf("safeopen: negative read cap")
	}
	if err := rejectNonRegular(path); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|extraOpenFlags, 0)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !st.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("%s: not a regular file", path)
	}
	if len(roots) > 0 {
		real, err := resolvedPath(f, path)
		if err != nil {
			f.Close()
			return nil, err
		}
		if !withinAny(real, roots) {
			f.Close()
			return nil, fmt.Errorf("%s: resolved path %s is outside the allowed directories", path, real)
		}
	}
	return &File{
		f:    f,
		r:    io.LimitReader(f, max+1),
		size: st.Size(),
		max:  max,
		name: path,
	}, nil
}

// rejectNonRegular refuses devices, FIFOs, and other non-files before open.
// A symlink is followed with Stat; the target must be a regular file.
// The opened descriptor is still fstat'd by the caller.
func rejectNonRegular(path string) error {
	lst, err := os.Lstat(path)
	if err != nil {
		return err
	}
	mode := lst.Mode()
	if mode&os.ModeSymlink != 0 {
		st, err := os.Stat(path)
		if err != nil {
			return err
		}
		mode = st.Mode()
	}
	if !mode.IsRegular() {
		return fmt.Errorf("%s: not a regular file", path)
	}
	return nil
}

func resolvedPath(f *os.File, fallback string) (string, error) {
	if p, err := procFDPath(f); err == nil && p != "" {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			return real, nil
		}
		return filepath.Clean(p), nil
	}
	return filepath.EvalSymlinks(fallback)
}

// Read reads at most max+1 bytes from the file.
func (f *File) Read(p []byte) (int, error) { return f.r.Read(p) }

// Close closes the descriptor.
func (f *File) Close() error { return f.f.Close() }

// Size is the size reported by fstat on the opened descriptor.
func (f *File) Size() int64 { return f.size }

// Name is the path passed to Open.
func (f *File) Name() string { return f.name }

// ReadAll reads a regular file that must be at most max bytes.
// A longer file, or a read that yields max+1 bytes, returns an error and
// no truncated buffer.
func ReadAll(path string, max int64, roots ...string) ([]byte, error) {
	b, err := readAll(path, max, roots...)
	return b, fileerr.Error(err)
}

func readAll(path string, max int64, roots ...string) ([]byte, error) {
	f, err := OpenUnder(path, max, roots...)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if f.size > max {
		return nil, fmt.Errorf("%s is %d bytes; refusing to read more than %d", filepath.Base(path), f.size, max)
	}
	buf, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	if int64(len(buf)) > max {
		return nil, fmt.Errorf("%s exceeds %d-byte cap", filepath.Base(path), max)
	}
	return buf, nil
}

// Within reports whether path is root or a relative descendant of root.
func Within(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || filepath.IsLocal(rel)
}

func withinAny(path string, roots []string) bool {
	for _, root := range roots {
		real := root
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			real = resolved
		}
		if Within(real, path) {
			return true
		}
	}
	return false
}
