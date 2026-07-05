package presentation

// brief_presenter.go — the single boundary brief (r4-target-architecture §5:
// 渲染 = 唯一 brief, 分节). Three sections, each omitted when empty:
//   [mnemon:context]  scoped view summary (the old context.packet)
//   [mnemon:handoff]  assignee-facing cues + local artifact paths (E1 fix:
//                     the receiver sees WHERE the content is on this machine)
//   [mnemon:contract] teaching section: the enabled capability dialect
//                     (the old payload.contract, reborn on the R4 face)
// The derived-event cue engine (teamwork_presenter) is unchanged — brief is
// its only registered audience from S3 on.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mnemon-dev/mnemon/harness/internal/mnemond/presentation/view"
)

const IntentBrief = "brief"

type briefPresenter struct{}

func (briefPresenter) Intent() string { return IntentBrief }

func (briefPresenter) Present(req Request, proj view.View, now time.Time) (PresentationBody, error) {
	events := DeriveEventEnvelopes(req, proj, now)
	var sections []string
	if body := BuildContextPacket(req, scopeViewForRequest(req, proj)); body != "" {
		sections = append(sections, section("context", body))
	}
	if body := buildHandoffSection(req, proj, PresentEventEnvelopes(events)); body != "" {
		sections = append(sections, section("handoff", body))
	}
	sections = append(sections, section("contract", BuildContractSection()))
	return PresentationBody{Body: strings.Join(sections, "\n\n"), Events: events}, nil
}

// scopeViewForRequest applies the same session scoping to the context
// section that the cue engine applies to derivations — a multica surface
// render must not resurface another session's items in ANY brief section.
func scopeViewForRequest(req Request, proj view.View) view.View {
	if !multicaHubRender(req) {
		return proj
	}
	sessionID := strings.TrimSpace(req.SessionID)
	currentID := strings.TrimSpace(req.InputDigest)
	if sessionID == "" && currentID == "" {
		return proj
	}
	scoped := proj
	scoped.Content = nil
	for _, content := range proj.Content {
		kind := string(content.Ref.Kind)
		if kind == "agent_profile" {
			scoped.Content = append(scoped.Content, content)
			continue
		}
		var kept []any
		for _, item := range resourceItems(content) {
			if multicaTeamworkItemInScope(kind, item, sessionID, currentID) {
				kept = append(kept, any(item))
			}
		}
		if len(kept) == 0 {
			continue
		}
		filtered := content
		filtered.Fields = map[string]any{"items": kept}
		scoped.Content = append(scoped.Content, filtered)
	}
	return scoped
}

// buildHandoffSection folds the derived cues with the local paths of every
// in-scope artifact digest. Digests alone are dead pointers to a fresh
// receiver; the blob-store path makes the content reachable on this machine.
func buildHandoffSection(_ Request, proj view.View, cues string) string {
	var parts []string
	if strings.TrimSpace(cues) != "" {
		parts = append(parts, cues)
	}
	if paths := artifactPathLines(proj); len(paths) > 0 {
		parts = append(parts, strings.Join(paths, "\n"))
	}
	return strings.Join(parts, "\n\n")
}

func artifactPathLines(proj view.View) []string {
	seen := map[string]bool{}
	for _, content := range proj.Content {
		for _, item := range resourceItems(content) {
			for _, ref := range itemStringList(item, "artifact_refs") {
				ref = strings.TrimSpace(ref)
				if strings.HasPrefix(ref, "sha256:") {
					seen[ref] = true
				}
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	digests := make([]string, 0, len(seen))
	for digest := range seen {
		digests = append(digests, digest)
	}
	sort.Strings(digests)
	lines := make([]string, 0, len(digests))
	for _, digest := range digests {
		hexPart := strings.TrimPrefix(digest, "sha256:")
		lines = append(lines, fmt.Sprintf("artifact %s = .mnemon/harness/blobs/sha256/%s", digest, hexPart))
	}
	return lines
}

// BuildContractSection teaches the enabled dialect on the R4 command face.
// It is the capability package's teaching text (r4-target-architecture §6);
// today teamwork is the flagship tenant, so its five words are taught here.
func BuildContractSection() string {
	return strings.Join([]string{
		"Emit governed events through mnemon-harness commands; do not write canonical state directly.",
		"- teamwork signal: collaboration is needed and should be visible (--scope --statement --why-teamwork --evidence --ttl).",
		"- teamwork assign: hand work over with --assignee --scope --work --feedback --evidence --ttl.",
		"- teamwork report: return an outcome with --outcome progress|result|blocker --summary; attach files with --attach.",
		"- teamwork profile: publish focus, availability, and context advantages when they materially change.",
		"- emit --schema <kind> --rule k=v --narrative k=v: the generic form when a dialect flag is missing.",
		"Payloads keep the rule / narrative / refs zones; the node's registry validates every field.",
	}, "\n")
}
