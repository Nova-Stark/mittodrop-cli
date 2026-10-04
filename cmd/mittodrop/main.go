package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"mittodrop/cmd/app"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	application := app.NewApp(os.Stdout, os.Stdin)
	if err := application.Run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
