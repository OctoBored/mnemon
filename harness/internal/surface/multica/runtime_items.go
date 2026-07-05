package multica

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
)

type RuntimeRPCMessage struct {
	Method string
	Params map[string]any
}

type RuntimeInput struct {
	Text                string
	IssueIdentity       string
	IssueIdentitySource string
}

func RuntimeAgentMessageMessages(threadID, turnID, itemID, text, phase string, now time.Time) []RuntimeRPCMessage {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	phase = strings.TrimSpace(phase)
	if phase == "" {
		phase = "commentary"
	}
	if strings.TrimSpace(itemID) == "" {
		itemID = runtimeTextDigestID("msg", text)
	}
	nowMs := now.UTC().UnixMilli()
	return []RuntimeRPCMessage{
		{
			Method: "item/started",
			Params: RuntimeItemParams(threadID, turnID, runtimeAgentMessage(itemID, "", phase), "startedAtMs", nowMs),
		},
		runtimeAgentDelta(threadID, turnID, itemID, text),
		{
			Method: "item/completed",
			Params: RuntimeItemParams(threadID, turnID, runtimeAgentMessage(itemID, text, phase), "completedAtMs", nowMs),
		},
	}
}

func RuntimeItemParams(threadID, turnID string, item map[string]any, timeKey string, timestampMs int64) map[string]any {
	params := map[string]any{
		"threadId": threadID,
		"turnId":   turnID,
		"item":     item,
	}
	if timeKey != "" && timestampMs > 0 {
		params[timeKey] = timestampMs
	}
	return params
}

func RuntimeUserMessage(text string) map[string]any {
	return map[string]any{
		"type":     "userMessage",
		"id":       runtimeTextDigestID("user", text),
		"clientId": nil,
		"content": []any{map[string]any{
			"type":          "text",
			"text":          text,
			"text_elements": []any{},
		}},
	}
}

func RuntimeInputMaterial(params map[string]any) RuntimeInput {
	input, ok := params["input"].([]any)
	if !ok {
		return RuntimeInput{}
	}
	var parts []string
	structuredIssue := ""
	structuredSource := ""
	textIssue := ""
	textSource := ""
	for _, item := range input {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if structuredIssue == "" {
			structuredIssue = runtimeStructuredIssueIdentity(obj)
			if structuredIssue != "" {
				structuredSource = RuntimeIssueSourceInput
			}
		}
		if text, _ := obj["text"].(string); strings.TrimSpace(text) != "" {
			parts = append(parts, text)
			if textIssue == "" {
				textIssue = ExtractIssueIdentity(text)
				if textIssue != "" {
					textSource = RuntimeIssueSourceInputText
				}
			}
		}
	}
	issueIdentity := firstNonEmptyString(structuredIssue, textIssue)
	issueSource := ""
	switch issueIdentity {
	case structuredIssue:
		issueSource = structuredSource
	case textIssue:
		issueSource = textSource
	}
	return RuntimeInput{
		Text:                strings.Join(parts, "\n"),
		IssueIdentity:       issueIdentity,
		IssueIdentitySource: issueSource,
	}
}

func RuntimeRef(kind, id string) string {
	kind = strings.TrimSpace(kind)
	id = strings.TrimSpace(id)
	if kind == "" || id == "" {
		return ""
	}
	return "multica:" + kind + ":" + id
}

func runtimeStructuredIssueIdentity(raw any) string {
	switch value := raw.(type) {
	case nil:
		return ""
	case string:
		return ExtractIssueIdentity(value)
	case []any:
		for _, item := range value {
			if issue := runtimeStructuredIssueIdentity(item); issue != "" {
				return issue
			}
		}
	case map[string]any:
		for _, key := range []string{"issue_id", "issueId", "issueID", "target_issue_id", "targetIssueId"} {
			if issue := cleanIssueIdentity(anyString(value[key])); issue != "" {
				return issue
			}
		}
		for _, key := range []string{"url", "href", "uri", "ref", "reference"} {
			if issue := ExtractIssueIdentity(anyString(value[key])); issue != "" {
				return issue
			}
		}
		if runtimeMapLooksLikeIssueRef(value) {
			for _, key := range []string{"id", "target_id", "targetId", "resource_id", "resourceId"} {
				if issue := cleanIssueIdentity(anyString(value[key])); issue != "" {
					return issue
				}
			}
		}
		for _, key := range []string{"identifier", "issue_identifier", "issueIdentifier", "label", "tag"} {
			if issue := runtimeIssueIdentifierTag(anyString(value[key])); issue != "" {
				return issue
			}
		}
		for _, key := range []string{"issue", "target", "resource", "mention", "mentions", "entities", "text_elements", "references", "tags"} {
			if issue := runtimeStructuredIssueIdentity(value[key]); issue != "" {
				return issue
			}
		}
		for _, item := range value {
			switch item.(type) {
			case map[string]any, []any:
				if issue := runtimeStructuredIssueIdentity(item); issue != "" {
					return issue
				}
			}
		}
	}
	return ""
}

func runtimeMapLooksLikeIssueRef(value map[string]any) bool {
	for _, key := range []string{"type", "kind", "resource", "resource_type", "resourceType", "entity", "entity_type", "entityType", "target_type", "targetType"} {
		if strings.Contains(strings.ToLower(anyString(value[key])), "issue") {
			return true
		}
	}
	if _, ok := value["issue"]; ok {
		return true
	}
	if issue := runtimeIssueIdentifierTag(anyString(value["identifier"])); issue != "" {
		return true
	}
	return false
}

func runtimeIssueIdentifierTag(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if issue := ExtractIssueIdentity(value); issue != "" {
		return issue
	}
	value = strings.TrimLeft(value, "@#")
	if value == "" {
		return ""
	}
	return ExtractIssueIdentity("@" + value)
}

func cloneRuntimeAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, item := range typed {
			out[key] = cloneRuntimeAny(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneRuntimeAny(item))
		}
		return out
	default:
		return typed
	}
}

func runtimeAgentDelta(threadID, turnID, itemID, delta string) RuntimeRPCMessage {
	return RuntimeRPCMessage{
		Method: "item/agentMessage/delta",
		Params: map[string]any{
			"threadId": threadID,
			"turnId":   turnID,
			"itemId":   itemID,
			"delta":    delta,
		},
	}
}

func runtimeAgentMessage(id, text, phase string) map[string]any {
	id = strings.TrimSpace(id)
	if id == "" {
		id = runtimeTextDigestID("msg", text)
	}
	phase = strings.TrimSpace(phase)
	if phase == "" {
		phase = "commentary"
	}
	return map[string]any{
		"type":           "agentMessage",
		"id":             id,
		"text":           text,
		"phase":          phase,
		"memoryCitation": nil,
	}
}

func runtimeTextDigestID(prefix, text string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "item"
	}
	sum := sha256.Sum256([]byte(text))
	digest := fmt.Sprintf("%x", sum[:])[:24]
	return prefix + "_" + digest
}
