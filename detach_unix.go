//go:build !windows

package main

import "syscall"

// detachAttr starts the child in its own session, so closing the terminal does not
// send it SIGHUP.
func detachAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }
