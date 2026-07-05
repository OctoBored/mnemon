package main

// push.go — R4 protocol-layer federation verbs (r4-cli-surface §2, git
// vocabulary): push and pull run ONE manual capsule pass over the
// configured remotes, through exactly the sync worker's code path.

import (
	"github.com/spf13/cobra"

	"github.com/mnemon-dev/mnemon/harness/internal/app"
)

var pushPullRoot string

func init() {
	push := &cobra.Command{
		Use:   "push",
		Short: "Push pending accepted state to the configured remotes as signed capsules",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return app.SyncOnce(pushPullRoot, "push", cmd.ErrOrStderr())
		},
	}
	pull := &cobra.Command{
		Use:   "pull",
		Short: "Pull and re-govern remote capsules from the configured remotes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return app.SyncOnce(pushPullRoot, "pull", cmd.ErrOrStderr())
		},
	}
	for _, c := range []*cobra.Command{push, pull} {
		c.Flags().StringVar(&pushPullRoot, "root", ".", "project root")
		c.GroupID = groupSpine
		rootCmd.AddCommand(c)
	}
}
