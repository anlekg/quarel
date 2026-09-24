//go:build !windows

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// defaultAdminAddr: reachable from the local network (password protected).
var defaultAdminAddr = ":8081"

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runService(ctx, os.Stderr)
}
