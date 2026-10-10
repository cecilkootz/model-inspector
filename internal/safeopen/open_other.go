//go:build !unix

package safeopen

// extraOpenFlags is empty off Unix, which has no O_NONBLOCK/O_NOCTTY here.
const extraOpenFlags = 0
