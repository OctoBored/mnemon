package main

import (
	"testing"

	"github.com/mnemon-dev/mnemon/harness/internal/contract"
)

func mustParseScopesForTest(t *testing.T, values []string) []contract.ResourceRef {
	t.Helper()
	scopes, err := parseScopeRefs(values)
	if err != nil {
		t.Fatal(err)
	}
	return scopes
}

func TestHubServeAbsorbsStandaloneBinary(t *testing.T) {
	for _, c := range hubCmd.Commands() {
		if c.Name() == "serve" {
			if !c.DisableFlagParsing {
				t.Fatal("hub serve must pass flags through to hubcli verbatim")
			}
			return
		}
	}
	t.Fatal("hub group must expose the absorbed serve subcommand")
}
