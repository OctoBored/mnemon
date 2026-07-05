package main

// recall.go — R4 CLI v2 protocol-layer read verb (r4-cli-surface §2,
// 推论 3): a generic keyword query over the principal's OWN scoped store.
// recall belongs to the protocol layer, not to any capability — memory
// tunes its ranking, teamwork uses it for mid-flight lookups, both share
// this one verb. Minimal S2 form: substring match over the scoped
// presentation view's materialized fields.

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mnemon-dev/mnemon/harness/internal/contract"
)

var (
	recallKind string
	recallJSON bool
)

func init() {
	cmd := &cobra.Command{
		Use:   "recall <keyword>",
		Short: "Search this principal's own governed store by keyword",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			keyword := strings.ToLower(strings.TrimSpace(args[0]))
			if keyword == "" {
				return fmt.Errorf("recall requires a non-empty keyword")
			}
			client, err := controlClient()
			if err != nil {
				return err
			}
			proj, err := client.PullPresentationView(contract.ActorID(controlPrincipal), contract.Subscription{Actor: contract.ActorID(controlPrincipal)})
			if err != nil {
				return fmt.Errorf("recall pull failed (service unreachable or unauthorized): %w", err)
			}
			type hit struct {
				Kind    string         `json:"kind"`
				ID      string         `json:"id"`
				Version int64          `json:"version"`
				Fields  map[string]any `json:"fields"`
			}
			var hits []hit
			for _, rc := range proj.Content {
				if recallKind != "" && string(rc.Ref.Kind) != recallKind {
					continue
				}
				blob, err := json.Marshal(rc.Fields)
				if err != nil {
					continue
				}
				if !strings.Contains(strings.ToLower(string(blob)), keyword) {
					continue
				}
				hits = append(hits, hit{Kind: string(rc.Ref.Kind), ID: string(rc.Ref.ID), Version: int64(rc.Version), Fields: rc.Fields})
			}
			if recallJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(hits)
			}
			if len(hits) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "recall: no matches")
				return nil
			}
			for _, h := range hits {
				fields, _ := json.Marshal(h.Fields)
				fmt.Fprintf(cmd.OutOrStdout(), "%s/%s v%d %s\n", h.Kind, h.ID, h.Version, fields)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&recallKind, "kind", "", "restrict matches to one resource kind")
	cmd.Flags().BoolVar(&recallJSON, "json", false, "emit matches as JSON")
	registerConnectionFlags(cmd)
	cmd.GroupID = groupSpine
	rootCmd.AddCommand(cmd)
}
