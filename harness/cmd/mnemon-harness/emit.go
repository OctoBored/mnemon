package main

// emit.go — R4 CLI v2 protocol-layer submission verb (r4-cli-surface §2).
// emit is the generic two-zone submitter every capability dialect is sugar
// for: --schema picks the registered capability kind (the registration
// gate), repeated --rule flags fill the rule zone, and the narrative zone
// arrives as a JSON object on stdin or via repeated --narrative flags.
// Internally it assembles the existing JSON envelope and speaks the
// existing /ingest endpoint — the wire is untouched until S3.
//
// Purity rule (r4-cli-surface 推论 1): this file knows schema NAMES only,
// never capability semantics — no required-field logic, no defaults. The
// node's policy registry stays the single validator.

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	eventmodel "github.com/mnemon-dev/mnemon/harness/internal/event"
)

// emitSchemaCatalog is the CLI-side registration gate: schema id →
// (event type, external-id prefix). One row per capability kind the node's
// policy registry accepts; the S2 command-registry guard keeps this table
// and the policy registry in lockstep.
var emitSchemaCatalog = map[string]struct {
	EventType string
	IDPrefix  string
}{
	"teamwork/signal":     {"teamwork_signal.write_candidate.observed", "teamwork-signal"},
	"teamwork/assignment": {"assignment.write_candidate.observed", "assignment"},
	"teamwork/report":     {"progress_digest.write_candidate.observed", "progress-digest"},
	"teamwork/profile":    {"agent_profile.write_candidate.observed", "agent-profile"},
}

var (
	emitSchema     string
	emitRulePairs  []string
	emitRefPairs   []string
	emitNarrPairs  []string
	emitExternalID string
)

func init() {
	cmd := &cobra.Command{
		Use:   "emit",
		Short: "Submit a governed event: --schema picks the kind, --rule fills the rule zone, stdin JSON is the narrative",
		RunE: func(cmd *cobra.Command, args []string) error {
			entry, ok := emitSchemaCatalog[emitSchema]
			if !ok {
				// A bare kind name (no capability prefix) submits that kind
				// directly — external event packages register kinds the CLI
				// cannot know statically; the node's policy registry stays
				// the validator and fails closed on unknown kinds.
				if kind := strings.TrimSpace(emitSchema); kind != "" && !strings.Contains(kind, "/") {
					entry.EventType = kind + ".write_candidate.observed"
					entry.IDPrefix = strings.ReplaceAll(kind, "_", "-")
				} else {
					return fmt.Errorf("emit: unknown --schema %q (registered: %s; or pass a bare kind name)", emitSchema, strings.Join(emitSchemaNames(), ", "))
				}
			}
			rule, err := emitZone(emitRulePairs, false)
			if err != nil {
				return fmt.Errorf("--rule: %w", err)
			}
			refs, err := emitZone(emitRefPairs, true)
			if err != nil {
				return fmt.Errorf("--ref: %w", err)
			}
			narrative, err := emitNarrative(cmd)
			if err != nil {
				return err
			}
			if emitExternalID != "" {
				controlExtID = emitExternalID
			}
			return controlShortObserve(cmd, entry.EventType, entry.IDPrefix, eventmodel.BuildPayload(rule, narrative, refs))
		},
	}
	cmd.Flags().StringVar(&emitSchema, "schema", "", "capability schema id, e.g. teamwork/signal")
	cmd.Flags().StringArrayVar(&emitRulePairs, "rule", nil, "rule-zone field as key=value; repeat a key to build a list")
	cmd.Flags().StringArrayVar(&emitRefPairs, "ref", nil, "refs-zone entry as key=value; repeated keys accumulate")
	cmd.Flags().StringArrayVar(&emitNarrPairs, "narrative", nil, "narrative field as key=value (alternative to stdin JSON)")
	cmd.Flags().StringVar(&emitExternalID, "external-id", "", "idempotency external id")
	_ = cmd.MarkFlagRequired("schema")
	registerConnectionFlags(cmd)
	cmd.GroupID = groupSpine
	rootCmd.AddCommand(cmd)
}

func emitSchemaNames() []string {
	names := make([]string, 0, len(emitSchemaCatalog))
	for name := range emitSchemaCatalog {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// emitZone parses repeated key=value pairs. Repeated keys become a list;
// listAlways forces every value into a list (the refs zone is list-typed).
func emitZone(pairs []string, listAlways bool) (map[string]any, error) {
	zone := map[string]any{}
	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("malformed pair %q (want key=value)", pair)
		}
		switch existing := zone[key].(type) {
		case nil:
			if listAlways {
				zone[key] = []string{value}
			} else {
				zone[key] = value
			}
		case string:
			zone[key] = []string{existing, value}
		case []string:
			zone[key] = append(existing, value)
		}
	}
	return zone, nil
}

// emitNarrative merges the stdin JSON object (when piped) with --narrative
// pairs; an explicit flag wins over the piped document.
func emitNarrative(cmd *cobra.Command) (map[string]any, error) {
	narrative := map[string]any{}
	if stat, err := os.Stdin.Stat(); err == nil && (stat.Mode()&os.ModeCharDevice) == 0 {
		raw, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return nil, fmt.Errorf("read stdin narrative: %w", err)
		}
		if body := strings.TrimSpace(string(raw)); body != "" {
			if err := json.Unmarshal([]byte(body), &narrative); err != nil {
				return nil, fmt.Errorf("stdin narrative must be a JSON object: %w", err)
			}
		}
	}
	for _, pair := range emitNarrPairs {
		key, value, ok := strings.Cut(pair, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("--narrative: malformed pair %q (want key=value)", pair)
		}
		narrative[key] = value
	}
	return narrative, nil
}
