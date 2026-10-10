//go:build unix

package safeopen

import "syscall"

// extraOpenFlags lets Open return on a FIFO instead of blocking until a
// writer connects, and keeps a raced device from becoming the controlling
// terminal. Regular files ignore O_NONBLOCK for ordinary reads.
const extraOpenFlags = syscall.O_NONBLOCK | syscall.O_NOCTTY
