package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/mnemon-dev/mnemon/harness/internal/contract"
	"github.com/mnemon-dev/mnemon/harness/internal/mnemonhub/exchange"
	"github.com/mnemon-dev/mnemon/harness/internal/mnemonhub/hubcli"
	"github.com/spf13/cobra"
)

var (
	hubRoot       string
	hubConfigPath string
)

var hubCmd = &cobra.Command{
	Use:   "hub",
	Short: "Manage harness MnemonHub connections",
}

var hubDoctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Report harness MnemonHub connection readiness",
	RunE:  runHubDoctor,
}

// hubServeCmd absorbs the mnemon-hub binary (R4 S2): the self-hosted hub
// boot face as a subcommand. Flag parsing stays inside hubcli, so args pass
// through verbatim; the hub remains its own trust domain (hubcli's import
// boundary is pinned by the mnemonhub boundary test).
var hubServeCmd = &cobra.Command{
	Use:                "serve",
	Short:              "Serve a self-hosted MnemonHub (Remote Workspace exchange) in the foreground",
	DisableFlagParsing: true,
	SilenceUsage:       true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return hubcli.Run(cmd.Context(), args, cmd.OutOrStdout(), cmd.ErrOrStderr())
	},
}

type commandRunner func(context.Context, commandInvocation) (commandResult, error)

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type commandInvocation struct {
	Dir    string
	Env    []string
	Name   string
	Args   []string
	Stdin  string
	Redact []string
}

type commandResult struct {
	Stdout string
	Stderr string
}

func init() {
	hubCmd.PersistentFlags().StringVar(&hubRoot, "root", ".", "project root")
	hubCmd.PersistentFlags().StringVar(&hubConfigPath, "config", "", "harness product config path")
	_ = hubCmd.PersistentFlags().MarkHidden("config")
	hubCmd.AddCommand(hubDoctorCmd, hubServeCmd)
	hubCmd.GroupID = groupSpine
	rootCmd.AddCommand(hubCmd)
}

func runHubDoctor(cmd *cobra.Command, args []string) error {
	root := strings.TrimSpace(hubRoot)
	if root == "" {
		root = "."
	}
	cfg, status, detail := doctorProductConfig(root)
	fmt.Fprintln(cmd.OutOrStdout(), "MnemonHub doctor")
	fmt.Fprintf(cmd.OutOrStdout(), "- Product config: %s", status)
	if detail != "" {
		fmt.Fprintf(cmd.OutOrStdout(), " (%s)", detail)
	}
	fmt.Fprintln(cmd.OutOrStdout())
	if cfg.Connections.Mnemonhub.Enabled {
		fmt.Fprintf(cmd.OutOrStdout(), "- Endpoint: %s\n", cfg.Connections.Mnemonhub.Endpoint)
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "- Endpoint: missing")
	}
	remotesPath := filepath.Join(root, ".mnemon", "harness", "sync", "remotes.json")
	if remote, err := exchange.LoadRemoteEntry(remotesPath, "default"); err == nil {
		fmt.Fprintf(cmd.OutOrStdout(), "- Remote Workspace: %s backend=%s credential_ref=%s\n", remote.ID, remote.NormalizedBackend(), remote.CredentialRef)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "- Remote Workspace: missing (%v)\n", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "- Free-plan estimate: 5 agents at 30s sync interval ~= 14,400 pulls/day before push/status overhead")
	return nil
}

func syncPushDiagnostic(push contract.SyncPushResponse) string {
	parts := []string{fmt.Sprintf("accepted=%d rejected=%d conflicts=%d", len(push.Accepted), len(push.Rejected), len(push.Conflicts))}
	for _, item := range push.Rejected {
		parts = append(parts, fmt.Sprintf("rejected event=%s subject=%s diagnostic=%q", item.EventID, item.Subject, item.Diagnostic))
	}
	for _, item := range push.Conflicts {
		parts = append(parts, fmt.Sprintf("conflict event=%s subject=%s diagnostic=%q", item.EventID, item.Subject, item.Diagnostic))
	}
	return strings.Join(parts, "; ")
}

func resolveWranglerCommand() (string, []string) {
	if path, err := exec.LookPath("wrangler"); err == nil {
		return path, nil
	}
	if runtime.GOOS == "windows" {
		return "npx.cmd", []string{"--yes", "wrangler"}
	}
	return "npx", []string{"--yes", "wrangler"}
}

func defaultCommandRunner(ctx context.Context, inv commandInvocation) (commandResult, error) {
	cmd := exec.CommandContext(ctx, inv.Name, inv.Args...)
	cmd.Dir = inv.Dir
	cmd.Env = inv.Env
	if inv.Stdin != "" {
		cmd.Stdin = strings.NewReader(inv.Stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := commandResult{Stdout: redact(stdout.String(), inv.Redact), Stderr: redact(stderr.String(), inv.Redact)}
	if err != nil {
		return result, fmt.Errorf("%s %s failed: %w\n%s", filepath.Base(inv.Name), strings.Join(inv.Args, " "), err, result.Stderr)
	}
	return result, nil
}

func parseScopeRefs(values []string) ([]contract.ResourceRef, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("at least one --scope kind/id is required")
	}
	seen := map[string]bool{}
	var out []contract.ResourceRef
	for _, value := range values {
		kind, id, ok := strings.Cut(strings.TrimSpace(value), "/")
		if !ok || strings.TrimSpace(kind) == "" || strings.TrimSpace(id) == "" {
			return nil, fmt.Errorf("scope %q must be kind/id", value)
		}
		ref := contract.ResourceRef{Kind: contract.ResourceKind(strings.TrimSpace(kind)), ID: contract.ResourceID(strings.TrimSpace(id))}
		key := string(ref.Kind) + "/" + string(ref.ID)
		if !seen[key] {
			out = append(out, ref)
			seen[key] = true
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return string(out[i].Kind)+"/"+string(out[i].ID) < string(out[j].Kind)+"/"+string(out[j].ID)
	})
	return out, nil
}

func randomHex(bytesLen int) (string, error) {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func randomSuffix(bytesLen int) string {
	value, err := randomHex(bytesLen)
	if err != nil {
		return "unknown"
	}
	return value
}

func redact(text string, secrets []string) string {
	for _, secret := range secrets {
		if strings.TrimSpace(secret) != "" {
			text = strings.ReplaceAll(text, secret, "<redacted>")
		}
	}
	return text
}

func expandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func repoRootForHarness() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func okWord(ok bool) string {
	if ok {
		return "ok"
	}
	return "skipped"
}

func validateEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("endpoint must be http or https")
	}
	if u.Host == "" {
		return fmt.Errorf("endpoint host is required")
	}
	return nil
}
