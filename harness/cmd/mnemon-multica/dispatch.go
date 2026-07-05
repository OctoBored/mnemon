package main

// dispatch.go — `mnemon-multica dispatch`: one deterministic pass of the
// Surface Delivery Feed (adapter contract §1). Reads deliveries after the
// durable cursor, writes display deliveries back through the idempotency
// ledger (comment + metadata + local artifact materialization), lists
// activate deliveries for the carrier path, and advances the cursor.
// It never parses business semantics or schedules providers.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mnemon-dev/mnemon/harness/internal/driver"
	"github.com/mnemon-dev/mnemon/harness/internal/mnemond/presentation"
	multicasurface "github.com/mnemon-dev/mnemon/harness/internal/surface/multica"
	"io"
	"net/http"
)

var (
	dispatchIssueID string
	dispatchRoot    string
)

const dispatchCursorRelPath = ".mnemon/multica/delivery-cursor"

// driverDispatchClient adapts the Multica CLI to the dispatcher's narrow face.
type driverDispatchClient struct{ cli driver.MulticaCLI }

func (d driverDispatchClient) AddIssueComment(ctx context.Context, issueID, content string) (multicasurface.MulticaComment, error) {
	comment, err := d.cli.AddIssueComment(ctx, issueID, content)
	if err != nil {
		return multicasurface.MulticaComment{}, err
	}
	return multicasurface.MulticaComment{ID: comment.ID}, nil
}

func (d driverDispatchClient) SetIssueMetadataMap(ctx context.Context, issueID string, values map[string]string) error {
	return d.cli.SetIssueMetadataMap(ctx, issueID, values)
}

func (d driverDispatchClient) SetIssueStatus(ctx context.Context, issueID, status string) (multicasurface.MulticaIssue, error) {
	issue, err := d.cli.SetIssueStatus(ctx, issueID, status)
	if err != nil {
		return multicasurface.MulticaIssue{}, err
	}
	return dispatchIssueView(issue), nil
}

func (d driverDispatchClient) GetIssue(ctx context.Context, issueID string) (multicasurface.MulticaIssue, error) {
	issue, err := d.cli.GetIssue(ctx, issueID)
	if err != nil {
		return multicasurface.MulticaIssue{}, err
	}
	return dispatchIssueView(issue), nil
}

// dispatchIssueView projects the driver issue onto the dispatcher's view;
// the assignment state comes from the ISSUE's own metadata (GetIssue 实查,
// never a caller flag — the §5 fix).
func dispatchIssueView(issue driver.MulticaIssue) multicasurface.MulticaIssue {
	assigned := ""
	for _, key := range []string{"assignee_id", "assigned_agent_id", "assignee"} {
		if value, ok := issue.Metadata[key].(string); ok && strings.TrimSpace(value) != "" {
			assigned = strings.TrimSpace(value)
			break
		}
	}
	return multicasurface.MulticaIssue{ID: issue.ID, Status: issue.Status, AssignedAgentID: assigned}
}

var multicaDispatchCmd = &cobra.Command{
	Use:   "dispatch",
	Short: "Run one delivery-feed pass: comment writeback through the ledger, cursor advance",
	RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(dispatchIssueID) == "" {
			return fmt.Errorf("--issue-id is required (delivery→issue correlation)")
		}
		client, _, err := multicaLocalClient()
		if err != nil {
			return err
		}
		cursorPath := filepath.Join(dispatchRoot, dispatchCursorRelPath)
		cursor := readDispatchCursor(cursorPath)
		feed, err := fetchSurfaceDeliveries(cursor)
		if err != nil {
			return err
		}
		deps := multicasurface.DispatchDeps{
			Client:      driverDispatchClient{cli: multicaCLI()},
			Ledger:      multicasurface.NewFileSurfaceWriteLedger(multicasurface.SurfaceWriteLedgerPath(dispatchRoot, "")),
			FetchBlob:   client.BlobGet,
			ArtifactDir: filepath.Join(dispatchRoot, ".mnemon", "multica", "artifacts"),
			Surface:     "multica",
		}
		ctx := multicaCommandContext(cmd)
		var results []multicasurface.DispatchResult
		for _, delivery := range feed.Deliveries {
			if delivery.Role != presentation.DeliveryRoleDisplay {
				// activate deliveries ride the carrier path; the dispatcher
				// only reports them (no provider scheduling here)
				fmt.Fprintf(cmd.OutOrStdout(), "activate delivery %s (target %s) — use activation-carrier\n",
					delivery.DeliveryID, delivery.Metadata["mnemon.target_agent_id"])
				continue
			}
			result, dispatchErr := multicasurface.DispatchDelivery(ctx, deps, delivery, strings.TrimSpace(dispatchIssueID))
			if dispatchErr != nil {
				return dispatchErr
			}
			results = append(results, result)
		}
		if err := writeDispatchCursor(cursorPath, feed.NextCursor); err != nil {
			return err
		}
		if multicaJSON {
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(map[string]any{"results": results, "next_cursor": feed.NextCursor})
		}
		for _, result := range results {
			if result.SkippedDuplicate {
				fmt.Fprintf(cmd.OutOrStdout(), "delivery %s skipped_duplicate\n", result.DeliveryID)
				continue
			}
			fmt.Fprintf(cmd.OutOrStdout(), "delivery %s comment=%s redacted=%v\n", result.DeliveryID, result.CommentID, result.Redacted)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "cursor=%d\n", feed.NextCursor)
		return nil
	},
}

// fetchSurfaceDeliveries pulls the feed straight off the node edge — the
// adapter's own HTTP call (contract §1; the core client stays feed-blind).
func fetchSurfaceDeliveries(cursor int64) (presentation.DeliveryFeed, error) {
	addr := strings.TrimRight(strings.TrimSpace(multicaLocalAddr), "/")
	req, err := http.NewRequest(http.MethodGet, addr+"/surface/deliveries?surface=multica&cursor="+strconv.FormatInt(cursor, 10), nil)
	if err != nil {
		return presentation.DeliveryFeed{}, err
	}
	token := strings.TrimSpace(multicaLocalToken)
	if strings.TrimSpace(multicaLocalTokenFile) != "" {
		raw, readErr := os.ReadFile(multicaLocalTokenFile)
		if readErr != nil {
			return presentation.DeliveryFeed{}, fmt.Errorf("read --token-file: %w", readErr)
		}
		token = strings.TrimSpace(string(raw))
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else {
		req.Header.Set("X-Mnemon-Principal", strings.TrimSpace(multicaLocalPrincipal))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return presentation.DeliveryFeed{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return presentation.DeliveryFeed{}, fmt.Errorf("surface deliveries failed: %s: %s", resp.Status, string(body))
	}
	var feed presentation.DeliveryFeed
	if err := json.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return presentation.DeliveryFeed{}, err
	}
	return feed, nil
}

func readDispatchCursor(path string) int64 {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	cursor, _ := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	return cursor
}

func writeDispatchCursor(path string, cursor int64) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.FormatInt(cursor, 10)+"\n"), 0o600)
}

func init() {
	multicaDispatchCmd.Flags().StringVar(&dispatchIssueID, "issue-id", "", "Multica issue receiving display writebacks")
	multicaDispatchCmd.Flags().StringVar(&dispatchRoot, "root", ".", "project root (cursor + ledger + artifacts)")
	multicaDispatchCmd.Flags().StringVar(&multicaLocalAddr, "addr", envDefault("MNEMON_CONTROL_ADDR", "http://127.0.0.1:8787"), "Local Mnemon URL")
	multicaDispatchCmd.Flags().StringVar(&multicaLocalPrincipal, "principal", envDefault("MNEMON_CONTROL_PRINCIPAL", ""), "local principal")
	multicaDispatchCmd.Flags().StringVar(&multicaLocalToken, "token", envDefault("MNEMON_CONTROL_TOKEN", ""), "Local Mnemon bearer token")
	multicaDispatchCmd.Flags().StringVar(&multicaLocalTokenFile, "token-file", envDefault("MNEMON_CONTROL_TOKEN_FILE", ""), "read Local Mnemon bearer token from a file")
	multicaCmd.AddCommand(multicaDispatchCmd)
}
