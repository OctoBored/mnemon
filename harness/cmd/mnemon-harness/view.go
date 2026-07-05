package main

// view.go — R4 CLI v2 boundary brief (r4-cli-surface §2): the protocol-layer
// read verb hooks call at lifecycle boundaries, equally usable by hand.
// S2 keeps it a public face over the existing render pipeline; S3 replaces
// the intent fan with the single three-section brief renderer.

import (
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "view",
		Short: "Render the boundary brief for this principal (what hooks inject at lifecycle edges)",
		RunE:  controlRenderCmd.RunE,
	}
	registerRenderFlags(cmd)
	registerConnectionFlags(cmd)
	cmd.GroupID = groupSpine
	rootCmd.AddCommand(cmd)
}
