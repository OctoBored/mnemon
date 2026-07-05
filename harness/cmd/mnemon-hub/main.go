// mnemon-hub is the compat-window shim over the absorbed hub boot face (R4
// S2: `mnemon-harness hub serve` is the product face; this binary forwards
// verbatim and is deleted at S6 with the other deprecated stubs).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mnemon-dev/mnemon/harness/internal/mnemonhub/hubcli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := hubcli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "mnemon-hub: %v\n", err)
		os.Exit(1)
	}
}
