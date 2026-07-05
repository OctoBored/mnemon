package main

// api.go — R4 CLI v2 escape hatch (r4-cli-surface §2, the only hidden
// command): `mnemon-harness api <method> <path>` is credential-managed curl
// against the node edge, in the gh-api mold. Anything the porcelain cannot
// say yet goes through here instead of growing a one-off command.

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mnemon-dev/mnemon/harness/internal/mnemond/access"
)

func init() {
	cmd := &cobra.Command{
		Use:    "api <method> <path>",
		Short:  "Escape hatch: send a raw request to the node edge with managed credentials",
		Hidden: true,
		Args:   cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			method := strings.ToUpper(strings.TrimSpace(args[0]))
			path := args[1]
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			var body io.Reader
			if stat, err := os.Stdin.Stat(); err == nil && (stat.Mode()&os.ModeCharDevice) == 0 {
				body = cmd.InOrStdin()
			}
			req, err := http.NewRequest(method, strings.TrimRight(controlAddr, "/")+path, body)
			if err != nil {
				return err
			}
			token := controlToken
			if controlTokenFile != "" {
				raw, err := os.ReadFile(controlTokenFile)
				if err != nil {
					return fmt.Errorf("read --token-file: %w", err)
				}
				token = strings.TrimSpace(string(raw))
			}
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			} else {
				req.Header.Set(access.PrincipalHeader, controlPrincipal)
			}
			if body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("api: node unreachable: %w", err)
			}
			defer resp.Body.Close()
			payload, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), string(payload))
			if resp.StatusCode >= 400 {
				return fmt.Errorf("api: %s", resp.Status)
			}
			return nil
		},
	}
	registerConnectionFlags(cmd)
	rootCmd.AddCommand(cmd)
}
