package main

// teamwork.go — R4 CLI v2 capability dialect (r4-cli-surface-v2 §capability
// groups): the public teamwork verbs signal · assign · report · profile.
// The dialect shares assembly builders and flag bindings with the deprecated
// `control teamwork` face, so both faces produce byte-identical submissions;
// the wire stays the existing JSON envelope until the S3 schema shrink.
//
// `report` is the R4 name for the progress digest (outcome semantics:
// progress | result | blocker). `--attach <file>` uploads the file to the
// node blob store (PUT /blobs/<digest>, content-addressed) and records the
// digest as an artifact reference — the E1 fix: content accompanies the
// handoff instead of dying as a local path.

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mnemon-dev/mnemon/harness/internal/blob"
	"github.com/mnemon-dev/mnemon/harness/internal/mnemond/access"
)

var (
	teamworkReportOutcome string
	teamworkReportAttach  []string
)

var teamworkCmd = &cobra.Command{
	Use:   "teamwork",
	Short: "Teamwork dialect: signal, assign, report, profile",
}

func init() {
	signal := &cobra.Command{
		Use:   "signal",
		Short: "Raise a teamwork signal (something needs more hands or eyes)",
		RunE:  controlTeamworkSignalCmd.RunE,
	}
	registerSignalFlags(signal)

	assign := &cobra.Command{
		Use:   "assign",
		Short: "Hand work to another principal with expected work and feedback",
		RunE:  controlTeamworkAssignCmd.RunE,
	}
	registerAssignFlags(assign)

	report := &cobra.Command{
		Use:   "report",
		Short: "Report an outcome: progress, result, or blocker (--attach uploads artifacts)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if outcome := strings.TrimSpace(teamworkReportOutcome); outcome != "" {
				controlTeamworkProgressFeedbackKind = outcome
			}
			for _, path := range cleanStrings(teamworkReportAttach) {
				digest, err := teamworkAttachUpload(cmd, path)
				if err != nil {
					return err
				}
				controlTeamworkProgressArtifacts = append(controlTeamworkProgressArtifacts, digest)
			}
			return controlTeamworkProgressCmd.RunE(cmd, args)
		},
	}
	registerProgressFlags(report)
	report.Flags().StringVar(&teamworkReportOutcome, "outcome", "", "outcome kind: progress, result, or blocker (R4 name for --feedback-kind)")
	report.Flags().StringArrayVar(&teamworkReportAttach, "attach", nil, "file to upload as a content-addressed artifact; may be repeated")

	profile := &cobra.Command{
		Use:   "profile",
		Short: "Publish this principal's working profile (focus, availability, advantages)",
		RunE:  controlProfileUpdateCmd.RunE,
	}
	registerProfileFlags(profile)

	teamworkCmd.AddCommand(signal, assign, report, profile)
	teamworkCmd.GroupID = groupSpine
	rootCmd.AddCommand(teamworkCmd)
}

// teamworkAttachUpload pushes one file into the node blob store and returns
// its digest. Content addressing makes the upload idempotent (204 on re-put).
func teamworkAttachUpload(cmd *cobra.Command, path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("attach %s: %w", path, err)
	}
	digest := blob.Digest(data)
	req, err := http.NewRequest(http.MethodPut, strings.TrimRight(controlAddr, "/")+"/blobs/"+digest, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	token := controlToken
	if controlTokenFile != "" {
		raw, err := os.ReadFile(controlTokenFile)
		if err != nil {
			return "", fmt.Errorf("read --token-file: %w", err)
		}
		token = strings.TrimSpace(string(raw))
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else {
		req.Header.Set(access.PrincipalHeader, controlPrincipal)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("attach %s: blob upload failed (node unreachable): %w", path, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusCreated, http.StatusNoContent:
		fmt.Fprintf(cmd.OutOrStdout(), "attached %s %s (%d bytes)\n", filepath.Base(path), digest, len(data))
		return digest, nil
	default:
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("attach %s: blob upload rejected: %s: %s", path, resp.Status, strings.TrimSpace(string(body)))
	}
}
