package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/mnemon-dev/mnemon/harness/internal/blob"
	"github.com/mnemon-dev/mnemon/harness/internal/capsule"
)

func init() {
	verifyCmd := newVerifyCmd()
	verifyCmd.GroupID = groupSpine
	rootCmd.AddCommand(verifyCmd)
}

// newVerifyCmd is the moat's user interface (r4-cli-surface: 协议层 verify):
// offline recomputation of a capsule's signature, canonical id, and blob
// closure — trusting the document, never the presenter.
func newVerifyCmd() *cobra.Command {
	var blobDir string
	var rootDir string
	cmd := &cobra.Command{
		Use:   "verify <capsule.json>",
		Short: "Verify a capsule offline: DSSE signature, canonical id, blob closure",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			var env capsule.Envelope
			if err := json.Unmarshal(raw, &env); err != nil {
				return fmt.Errorf("verify: not a DSSE envelope: %w", err)
			}
			var resolver capsule.BlobResolver
			dir := blobDir
			if dir == "" {
				dir = filepath.Join(rootDir, blob.DefaultDir)
			}
			if store, err := blob.Open(dir); err == nil {
				resolver = store.Resolver()
			}
			res := capsule.Verify(env, resolver)
			out := cmd.OutOrStdout()
			if res.OK() {
				fmt.Fprintf(out, "OK capsule %s\n", res.CapsuleID)
				fmt.Fprintf(out, "   producer %s (key %s), boundary %s, records %d, blobs %d\n",
					res.Document.Header.Producer.Principal, res.Document.Header.Producer.KeyID,
					res.Document.Header.Boundary, len(res.Document.Records), len(res.Document.Blobs))
				return nil
			}
			fmt.Fprintf(out, "FAIL capsule %s\n", res.CapsuleID)
			for _, issue := range res.Issues {
				fmt.Fprintf(out, "   %s\n", issue)
			}
			return fmt.Errorf("verify: %d issue(s)", len(res.Issues))
		},
	}
	cmd.Flags().StringVar(&blobDir, "blobs", "", "blob store directory (defaults to <root>/"+blob.DefaultDir+")")
	cmd.Flags().StringVar(&rootDir, "root", ".", "project root")
	return cmd
}
