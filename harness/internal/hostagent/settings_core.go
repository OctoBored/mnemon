package hostagent

// settings_core.go — the shared settings-writer core (R4 S3: host.json is
// the single source of the lifecycle mapping; the claude/codex writers keep
// only their file-format differences). The L3 timing vocabulary is the
// closed set enter | mid | exit; each host event in the mapping points at
// the boundary script of its class.

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"

	"github.com/mnemon-dev/mnemon/harness/internal/assets"
)

type hookSelection struct {
	Mid  bool
	Exit bool
}

type hostEventHook struct {
	Event  string // host event name, e.g. SessionStart
	Script string // boundary script file, e.g. enter.sh
}

// hostLifecycleMapping reads the boundary→host-events mapping from the
// host's host.json. Fail-closed: a host without a mapping cannot install.
func hostLifecycleMapping(fsys fs.FS, host string) (map[string][]string, error) {
	raw, err := fs.ReadFile(fsys, "hosts/"+host+"/host.json")
	if err != nil {
		return nil, fmt.Errorf("read host.json for host %s: %w", host, err)
	}
	var doc struct {
		LifecycleMapping map[string][]string `json:"lifecycle_mapping"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse host.json for host %s: %w", host, err)
	}
	if len(doc.LifecycleMapping) == 0 {
		return nil, fmt.Errorf("host.json for host %s has no lifecycle_mapping (single source, required)", host)
	}
	for boundary := range doc.LifecycleMapping {
		if !isHookTiming(boundary) {
			return nil, fmt.Errorf("host.json for host %s maps unknown boundary %q (closed set: enter|mid|exit)", host, boundary)
		}
	}
	return doc.LifecycleMapping, nil
}

// managedHookEvents resolves which host events get which boundary script.
// enter is always installed; mid/exit follow the selection.
func managedHookEvents(host string, sel hookSelection) ([]hostEventHook, error) {
	mapping, err := hostLifecycleMapping(assets.FS, host)
	if err != nil {
		return nil, err
	}
	var hooks []hostEventHook
	appendBoundary := func(boundary string) {
		events := append([]string(nil), mapping[boundary]...)
		sort.Strings(events)
		for _, event := range events {
			hooks = append(hooks, hostEventHook{Event: event, Script: boundary + ".sh"})
		}
	}
	appendBoundary("enter")
	if sel.Mid {
		appendBoundary("mid")
	}
	if sel.Exit {
		appendBoundary("exit")
	}
	return hooks, nil
}

// managedHostEvents lists every host event the mapping claims — the removal
// set for unpatch (remove must cover events a narrower selection skipped).
func managedHostEvents(host string) ([]string, error) {
	mapping, err := hostLifecycleMapping(assets.FS, host)
	if err != nil {
		return nil, err
	}
	var events []string
	for _, list := range mapping {
		events = append(events, list...)
	}
	sort.Strings(events)
	return events, nil
}
