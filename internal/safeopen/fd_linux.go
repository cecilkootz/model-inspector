//go:build linux

package safeopen

import (
	"fmt"
	"os"
)

func procFDPath(f *os.File) (string, error) {
	return os.Readlink(fmt.Sprintf("/proc/self/fd/%d", f.Fd()))
}
