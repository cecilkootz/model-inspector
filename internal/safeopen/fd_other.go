//go:build !linux

package safeopen

import (
	"errors"
	"os"
)

func procFDPath(*os.File) (string, error) {
	return "", errors.New("descriptor path is unavailable")
}
