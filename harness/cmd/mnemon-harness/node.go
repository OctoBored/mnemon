package main

// node.go — R4 CLI v2 node lifecycle group (r4-cli-surface §2): absorbs the
// standalone mnemond binary so the product has ONE boot face. Every verb
// delegates to internal/nodecli (the shared implementation the mnemond shim
// also calls); flag parsing stays inside nodecli, so cobra passes args
// through verbatim. `node wake` is the R4 name for the managed-agent drive
// source (`mnemond agent run`).

import (
	"github.com/spf13/cobra"

	"github.com/mnemon-dev/mnemon/harness/internal/nodecli"
)

func init() {
	// `node up` re-execs its detached serve child through THIS binary's face.
	nodecli.ServeChildArgv = []string{"node", "serve"}
	nodecli.FaceName = "mnemon-harness node"

	nodeCmd := &cobra.Command{
		Use:   "node",
		Short: "Local event node lifecycle: up, down, reload, status, logs, doctor, serve, wake",
	}
	type verb struct {
		use, short string
		run        func(cmd *cobra.Command, args []string) error
	}
	verbs := []verb{
		{"up", "Start the local event node in the background", func(cmd *cobra.Command, args []string) error {
			return nodecli.Up(args, cmd.OutOrStdout(), cmd.ErrOrStderr())
		}},
		{"down", "Stop the background local event node", func(cmd *cobra.Command, args []string) error {
			return nodecli.Down(args, cmd.OutOrStdout(), cmd.ErrOrStderr())
		}},
		{"reload", "Restart the background local event node (repacks the capability catalog)", func(cmd *cobra.Command, args []string) error {
			return nodecli.Reload(args, cmd.OutOrStdout(), cmd.ErrOrStderr())
		}},
		{"status", "Show background local event node status", func(cmd *cobra.Command, args []string) error {
			return nodecli.Status(args, cmd.OutOrStdout(), cmd.ErrOrStderr())
		}},
		{"logs", "Show background local event node logs", func(cmd *cobra.Command, args []string) error {
			return nodecli.Logs(args, cmd.OutOrStdout(), cmd.ErrOrStderr())
		}},
		{"doctor", "Check local event node readiness", func(cmd *cobra.Command, args []string) error {
			return nodecli.Doctor(args, cmd.OutOrStdout(), cmd.ErrOrStderr())
		}},
		{"serve", "Run the local event node in the foreground", func(cmd *cobra.Command, args []string) error {
			return nodecli.Run(cmd.Context(), append([]string{"serve"}, args...), cmd.OutOrStdout(), cmd.ErrOrStderr())
		}},
		{"wake", "Run the managed-agent drive source ([mnemon:wake] turns)", func(cmd *cobra.Command, args []string) error {
			return nodecli.Wake(cmd.Context(), args, cmd.OutOrStdout(), cmd.ErrOrStderr())
		}},
	}
	for _, v := range verbs {
		sub := &cobra.Command{
			Use:                v.use,
			Short:              v.short,
			RunE:               v.run,
			DisableFlagParsing: true,
			SilenceUsage:       true,
		}
		nodeCmd.AddCommand(sub)
	}
	nodeCmd.GroupID = groupSpine
	rootCmd.AddCommand(nodeCmd)
}
