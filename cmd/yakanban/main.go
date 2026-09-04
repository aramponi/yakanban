// Command yakanban drives a Kanban board hosted in a real ticket tracker.
//
// The same binary doubles as a GitHub CLI extension: installed as
// `gh-yakanban`, it answers to `gh yakanban ...`.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/aramponi/yakanban/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(cli.Execute(ctx, os.Args[1:]))
}
