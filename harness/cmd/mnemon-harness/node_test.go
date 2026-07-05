package main

import (
	"strings"
	"testing"

	"github.com/mnemon-dev/mnemon/harness/internal/nodecli"
)

func TestNodeGroupAbsorbsDaemonLifecycle(t *testing.T) {
	node := rootSub(t, "node")
	got := map[string]bool{}
	for _, c := range node.Commands() {
		got[c.Name()] = true
	}
	for _, want := range []string{"up", "down", "reload", "status", "logs", "doctor", "serve", "wake"} {
		if !got[want] {
			t.Fatalf("node group missing verb %q (has %v)", want, got)
		}
	}
	// `node up` must re-exec its detached child through this binary's face,
	// and usage text must speak the product face, not the absorbed binary.
	if strings.Join(nodecli.ServeChildArgv, " ") != "node serve" {
		t.Fatalf("ServeChildArgv must route through the node face, got %v", nodecli.ServeChildArgv)
	}
	if nodecli.FaceName != "mnemon-harness node" {
		t.Fatalf("FaceName must be the product face, got %q", nodecli.FaceName)
	}
}

func TestNodeStatusRunsOnFreshRoot(t *testing.T) {
	node := rootSub(t, "node")
	var status *strings.Builder
	for _, c := range node.Commands() {
		if c.Name() == "status" {
			status = &strings.Builder{}
			c.SetOut(status)
			if err := c.RunE(c, []string{"--root", t.TempDir()}); err != nil {
				t.Fatalf("node status on a fresh root must not error: %v", err)
			}
		}
	}
	if status == nil || !strings.Contains(status.String(), "node: stopped") {
		t.Fatalf("node status must report the stopped node, got %q", status)
	}
}
