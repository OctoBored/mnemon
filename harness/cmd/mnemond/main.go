// mnemond is the compat-window shim over the absorbed node CLI (R4 S2:
// `mnemon-harness node` is the product face; this binary forwards verbatim
// and is deleted at S6 with the other deprecated stubs).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mnemon-dev/mnemon/harness/internal/nodecli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := nodecli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "mnemond: %v\n", err)
		os.Exit(1)
	}
}
