package main

// remote.go — R4 CLI v2 federation registry verbs (r4-cli-surface §2, git
// vocabulary): remote add / list / remove manage the local remotes.json the
// sync worker and manual verbs already read. add reuses the exact upsert
// (and credential write) `sync connect` performs, so the two faces can
// never drift; the sync group itself is a deprecated alias from S2 on.

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mnemon-dev/mnemon/harness/internal/mnemonhub/exchange"
)

var (
	remoteAddEndpoint  string
	remoteAddToken     string
	remoteAddTokenFile string
	remoteAddCAFile    string
	remoteAddDirection string
)

func init() {
	remoteCmd := &cobra.Command{
		Use:   "remote",
		Short: "Manage federation remotes: add, list, remove",
	}

	add := &cobra.Command{
		Use:   "add <id>",
		Short: "Register a Remote Workspace endpoint under a short name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			if id == "" {
				return fmt.Errorf("remote add requires a non-empty id")
			}
			if strings.TrimSpace(remoteAddEndpoint) == "" {
				return fmt.Errorf("--endpoint is required")
			}
			path := resolvedSyncRemotesPath()
			if err := upsertSyncRemote(path, syncProjectRoot(), id, exchange.RemoteBackendHTTP, remoteAddDirection, strings.TrimSpace(remoteAddEndpoint), remoteAddToken, remoteAddTokenFile, remoteAddCAFile); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "remote %s added (%s)\n", id, strings.TrimSpace(remoteAddEndpoint))
			return nil
		},
	}
	add.Flags().StringVar(&remoteAddEndpoint, "endpoint", "", "remote base URL")
	add.Flags().StringVar(&remoteAddToken, "token", "", "bearer token (written to a 0600 credential file)")
	add.Flags().StringVar(&remoteAddTokenFile, "token-file", "", "existing token file to reference instead of --token")
	add.Flags().StringVar(&remoteAddCAFile, "ca-file", "", "pinned TLS root (PEM); empty trusts system roots")
	add.Flags().StringVar(&remoteAddDirection, "direction", "", "bidirectional (default), publish, or subscribe")

	list := &cobra.Command{
		Use:   "list",
		Short: "List registered remotes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			doc, err := readRemotesDoc(resolvedSyncRemotesPath())
			if err != nil {
				return err
			}
			if len(doc.Remotes) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no remotes")
				return nil
			}
			for _, entry := range doc.Remotes {
				marker := " "
				if entry.ID == doc.Current {
					marker = "*"
				}
				direction := entry.Direction
				if direction == "" {
					direction = exchange.RemoteDirectionBidirectional
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s\t%s\t%s\n", marker, entry.ID, entry.Endpoint, direction)
			}
			return nil
		},
	}

	remove := &cobra.Command{
		Use:   "remove <id>",
		Short: "Remove a registered remote",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			path := resolvedSyncRemotesPath()
			doc, err := readRemotesDoc(path)
			if err != nil {
				return err
			}
			kept := doc.Remotes[:0]
			found := false
			for _, entry := range doc.Remotes {
				if entry.ID == id {
					found = true
					continue
				}
				kept = append(kept, entry)
			}
			if !found {
				return fmt.Errorf("remote %q not found", id)
			}
			doc.Remotes = kept
			if doc.Current == id {
				doc.Current = ""
				if len(kept) == 1 {
					doc.Current = kept[0].ID
				}
			}
			data, err := json.MarshalIndent(doc, "", "  ")
			if err != nil {
				return err
			}
			if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "remote %s removed\n", id)
			return nil
		},
	}

	for _, c := range []*cobra.Command{add, list, remove} {
		c.Flags().StringVar(&syncRoot, "root", ".", "project root")
	}
	remoteCmd.AddCommand(add, list, remove)
	remoteCmd.GroupID = groupSpine
	rootCmd.AddCommand(remoteCmd)
}

func readRemotesDoc(path string) (exchange.RemotesDoc, error) {
	doc := exchange.RemotesDoc{SchemaVersion: 1}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return doc, nil
	}
	if err != nil {
		return doc, fmt.Errorf("read Remote Workspace config: %w", err)
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return doc, fmt.Errorf("parse Remote Workspace config: %w", err)
	}
	if doc.SchemaVersion != 1 {
		return doc, fmt.Errorf("Remote Workspace config schema_version %d unsupported (want 1)", doc.SchemaVersion)
	}
	return doc, nil
}
