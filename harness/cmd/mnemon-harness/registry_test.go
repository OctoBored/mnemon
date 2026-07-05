package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// baseCommandBusinessTerms is the protocol-purity wordlist (r4-registries
// §2, extending the coreguard vocabulary rule to the CLI): base command
// paths and short help must never speak capability vocabulary. Capability
// dialect groups (registered dialect prefixes) are exempt.
var baseCommandBusinessTerms = []string{
	"signal",
	"assignment",
	"progress_digest",
	"agent_profile",
	"teamwork",
}

var dialectPathPrefixes = []string{"teamwork"}

func runnableCommandPaths(t *testing.T) map[string]*cobra.Command {
	t.Helper()
	paths := map[string]*cobra.Command{}
	var walk func(c *cobra.Command, path string, deprecatedAncestor bool)
	walk = func(c *cobra.Command, path string, deprecatedAncestor bool) {
		dep := deprecatedAncestor || c.Deprecated != ""
		if !dep && (c.Run != nil || c.RunE != nil) {
			paths[path] = c
		}
		for _, sub := range c.Commands() {
			if sub.Name() == "help" || sub.Name() == "completion" {
				continue
			}
			walk(sub, strings.TrimSpace(path+" "+sub.Name()), dep)
		}
	}
	for _, c := range rootCmd.Commands() {
		if c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		walk(c, c.Name(), false)
	}
	return paths
}

func TestEveryRunnableCommandAnswersTheTriple(t *testing.T) {
	runnable := runnableCommandPaths(t)
	rows := map[string]CommandSpec{}
	for _, spec := range commandRegistry {
		if _, dup := rows[spec.Path]; dup {
			t.Errorf("duplicate registry row %q", spec.Path)
		}
		rows[spec.Path] = spec
	}
	for path := range runnable {
		if _, ok := rows[path]; !ok {
			t.Errorf("command %q exists but has no registry row: answer audience×edge×endpoint before it may exist", path)
		}
	}
	for path, spec := range rows {
		if _, ok := runnable[path]; !ok {
			t.Errorf("registry row %q names no runnable command (zombie row)", path)
		}
		if !commandAudiences[spec.Audience] {
			t.Errorf("row %q: audience %q outside the closed set", path, spec.Audience)
		}
		if !commandEdges[spec.Edge] {
			t.Errorf("row %q: edge %q outside the closed set", path, spec.Edge)
		}
		if !commandEndpoints[spec.Endpoint] {
			t.Errorf("row %q: endpoint %q outside the closed set", path, spec.Endpoint)
		}
	}
}

func TestBaseCommandsKeepProtocolVocabularyPure(t *testing.T) {
	runnable := runnableCommandPaths(t)
	for path, cmd := range runnable {
		dialect := false
		for _, prefix := range dialectPathPrefixes {
			if path == prefix || strings.HasPrefix(path, prefix+" ") {
				dialect = true
			}
		}
		if dialect {
			continue
		}
		haystack := strings.ToLower(path + " " + cmd.Short)
		for _, term := range baseCommandBusinessTerms {
			if strings.Contains(haystack, term) {
				t.Errorf("base command %q leaks capability vocabulary %q in its path/help (%q); capability words live in dialect groups only", path, term, cmd.Short)
			}
		}
	}
}
