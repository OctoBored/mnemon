package coreguard

// Test layout guard (r4 plan §8 布局约定): every _test.go file in harness/
// takes one of three shapes — 1:1 companion (foo_test.go beside foo.go),
// special form (*_gate_test.go / *_guard_test.go / *_e2e_test.go), or
// package helpers (test_helpers_test.go). Files predating the convention
// live in the grandfather whitelist below, which ONLY SHRINKS: an entry
// that now conforms or no longer exists must be removed, and no new entry
// may be added — new tests conform instead.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var grandfatheredTestFiles = map[string]bool{
	"cmd/mnemon-harness/config_daemon_test.go":              true,
	"cmd/mnemon-harness/sync_probe_test.go":                 true,
	"cmd/mnemon-harness/sync_security_test.go":              true,
	"internal/app/coordination_test.go":                     true,
	"internal/app/cutover_parity_test.go":                   true,
	"internal/app/driver_wiring_test.go":                    true,
	"internal/app/external_catalog_test.go":                 true,
	"internal/app/item_dedup_sync_test.go":                  true,
	"internal/app/loop_add_test.go":                         true,
	"internal/app/risk_operator_test.go":                    true,
	"internal/app/setup_additive_test.go":                   true,
	"internal/app/setup_token_test.go":                      true,
	"internal/app/sync_event_test.go":                       true,
	"internal/app/sync_import_test.go":                      true,
	"internal/app/sync_remote_diagnostic_test.go":           true,
	"internal/app/sync_skipped_test.go":                     true,
	"internal/app/teamwork_loop_test.go":                    true,
	"internal/app/tower_write_test.go":                      true,
	"internal/app/uninstall_noclobber_test.go":              true,
	"internal/assembler/assemble_test.go":                   true,
	"internal/contract/budget_test.go":                      true,
	"internal/contract/clamp_test.go":                       true,
	"internal/contract/event_envelope_test.go":              true,
	"internal/hostagent/claude_settings_noclobber_test.go":  true,
	"internal/mnemond/access/auth_test.go":                  true,
	"internal/mnemond/access/bindingwrite_test.go":          true,
	"internal/mnemond/access/clamp_delegation_test.go":      true,
	"internal/mnemond/access/clamp_test.go":                 true,
	"internal/mnemond/access/scope_intake_test.go":          true,
	"internal/mnemond/admission/audit_test.go":              true,
	"internal/mnemond/admission/authz_catalog_test.go":      true,
	"internal/mnemond/admission/conflict_harness_test.go":   true,
	"internal/mnemond/admission/determinism_test.go":        true,
	"internal/mnemond/admission/empty_correlation_test.go":  true,
	"internal/mnemond/admission/escalation_modes_test.go":   true,
	"internal/mnemond/admission/escalation_restart_test.go": true,
	"internal/mnemond/admission/liveness_test.go":           true,
	"internal/mnemond/admission/malformed_test.go":          true,
	"internal/mnemond/admission/mode_behavior_test.go":      true,
	"internal/mnemond/admission/origin_test.go":             true,
	"internal/mnemond/admission/restart_test.go":            true,
	"internal/mnemond/admission/routing_test.go":            true,
	"internal/mnemond/policy/external_test.go":              true,
	"internal/mnemond/policy/limits_test.go":                true,
	"internal/mnemond/policy/parity_test.go":                true,
	"internal/mnemond/presentation/view/scoped_test.go":     true,
	"internal/mnemond/state/apply_distinct_writes_test.go":  true,
	"internal/mnemond/state/cursor_test.go":                 true,
	"internal/mnemond/state/event_envelope_test.go":         true,
	"internal/mnemond/state/guard_test.go":                  true,
	"internal/mnemond/state/inbox_test.go":                  true,
	"internal/mnemond/state/ingest_errors_test.go":          true,
	"internal/mnemond/state/kind_catalog_test.go":           true,
	"internal/mnemond/state/lease_budget_test.go":           true,
	"internal/mnemond/state/migration_test.go":              true,
	"internal/mnemond/state/outbox_test.go":                 true,
	"internal/mnemond/state/store_read_test.go":             true,
	"internal/mnemonhub/boundary_test.go":                   true,
	"internal/mnemonhub/exchange/perm_test.go":              true,
	"internal/mnemonhub/exchange/probe_test.go":             true,
	"internal/mnemonhub/security_test.go":                   true,
	"internal/mnemonhub/sync_abi_fixture_test.go":           true,
	"internal/replay/modes_test.go":                         true,
	"internal/replay/shadow_test.go":                        true,
	"internal/runtime/attribution_test.go":                  true,
	"internal/runtime/binding_test.go":                      true,
	"internal/runtime/bindingauth_test.go":                  true,
	"internal/runtime/bindingboot_test.go":                  true,
	"internal/runtime/bindingscope_test.go":                 true,
	"internal/runtime/bodycap_test.go":                      true,
	"internal/runtime/decision_ledger_test.go":              true,
	"internal/runtime/diagnostic_test.go":                   true,
	"internal/runtime/drain_test.go":                        true,
	"internal/runtime/event_envelope_test.go":               true,
	"internal/runtime/forged_proposed_test.go":              true,
	"internal/runtime/intake_test.go":                       true,
	"internal/runtime/local_config_test.go":                 true,
	"internal/runtime/local_event_test.go":                  true,
	"internal/runtime/local_progress_test.go":               true,
	"internal/runtime/multimachine_test.go":                 true,
	"internal/runtime/p2gate_test.go":                       true,
	"internal/runtime/p3hardening_test.go":                  true,
	"internal/runtime/readback_test.go":                     true,
	"internal/runtime/statusevidence_test.go":               true,
	"internal/runtime/sync_state_test.go":                   true,
	"internal/runtime/unknown_verdict_test.go":              true,
	"internal/surface/multica/surface_r3_test.go":           true,
	"internal/ui/tower_vocab_test.go":                       true,
}

func testFileConforms(dir, name string) bool {
	for _, suffix := range []string{"_gate_test.go", "_guard_test.go", "_e2e_test.go"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	if name == "test_helpers_test.go" || strings.HasSuffix(name, "_test_helpers_test.go") {
		return true
	}
	companion := strings.TrimSuffix(name, "_test.go") + ".go"
	_, err := os.Stat(filepath.Join(dir, companion))
	return err == nil
}

func TestHarnessTestFilesFollowLayoutConvention(t *testing.T) {
	root := filepath.Join("..", "..")
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		seen[rel] = true
		conforms := testFileConforms(filepath.Dir(path), entry.Name())
		if conforms {
			if grandfatheredTestFiles[rel] {
				t.Errorf("whitelist entry %s now conforms — remove it (the list only shrinks)", rel)
			}
			return nil
		}
		if !grandfatheredTestFiles[rel] {
			t.Errorf("test file %s matches none of the three layout shapes (1:1 companion, *_gate/_guard/_e2e_test.go, test_helpers_test.go) and is not grandfathered; new tests must conform", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk harness: %v", err)
	}
	for rel := range grandfatheredTestFiles {
		if !seen[rel] {
			t.Errorf("whitelist entry %s no longer exists — remove it (the list only shrinks)", rel)
		}
	}
}
