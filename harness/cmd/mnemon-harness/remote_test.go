package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func remoteSub(t *testing.T, name string) *cobra.Command {
	t.Helper()
	for _, c := range rootSub(t, "remote").Commands() {
		if c.Name() == name {
			return c
		}
	}
	t.Fatalf("remote subcommand %q not registered", name)
	return nil
}

func TestRemoteAddListRemoveRoundTrip(t *testing.T) {
	root := t.TempDir()
	syncRoot = root
	syncRemotesPath = ""
	t.Cleanup(func() { syncRoot = "."; syncRemotesPath = "" })

	remoteAddEndpoint = "https://hub.example.com"
	remoteAddToken = "secret-token"
	remoteAddTokenFile = ""
	remoteAddCAFile = ""
	remoteAddDirection = ""
	add := remoteSub(t, "add")
	var out strings.Builder
	add.SetOut(&out)
	if err := add.RunE(add, []string{"team-hub"}); err != nil {
		t.Fatalf("remote add failed: %v", err)
	}

	// credential written 0600, referenced relatively (sync connect parity)
	credPath := filepath.Join(root, ".mnemon", "harness", "sync", "credentials", "team-hub.token")
	info, err := os.Stat(credPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("credential file must exist with 0600, got %v %v", info, err)
	}

	list := remoteSub(t, "list")
	out.Reset()
	list.SetOut(&out)
	if err := list.RunE(list, nil); err != nil {
		t.Fatalf("remote list failed: %v", err)
	}
	if !strings.Contains(out.String(), "* team-hub") || !strings.Contains(out.String(), "https://hub.example.com") {
		t.Fatalf("list must show the current remote:\n%s", out.String())
	}

	remove := remoteSub(t, "remove")
	out.Reset()
	remove.SetOut(&out)
	if err := remove.RunE(remove, []string{"team-hub"}); err != nil {
		t.Fatalf("remote remove failed: %v", err)
	}
	out.Reset()
	list.SetOut(&out)
	if err := list.RunE(list, nil); err != nil {
		t.Fatalf("remote list after remove failed: %v", err)
	}
	if !strings.Contains(out.String(), "no remotes") {
		t.Fatalf("removed remote must disappear:\n%s", out.String())
	}

	if err := remove.RunE(remove, []string{"ghost"}); err == nil {
		t.Fatal("removing an unknown remote must fail")
	}
}
