package multica

import (
	"strings"
)

type RuntimeResultSummary struct {
	IssueID    string
	Identifier string
	Title      string
	Principal  string
	Status     string
	Err        string
}

func FormatRuntimeFinalAnswer(result RuntimeResultSummary) string {
	// §7 裁决→RPC 映射: 终答是操作者可见文本 — 中文自然语言, 不携带
	// principal、协议字段或 Go error 原文(细节走 metadata/本地日志)。
	var b strings.Builder
	label := strings.TrimSpace(result.Identifier)
	if label == "" {
		label = strings.TrimSpace(result.IssueID)
	}
	title := strings.TrimSpace(result.Title)
	switch {
	case label == "":
		b.WriteString("操作未完成: 未收到 Multica issue 标识。")
	default:
		switch result.Status {
		case "recorded":
			b.WriteString("已记录并接受: ")
			if title != "" {
				b.WriteString(title)
			} else {
				b.WriteString(label)
			}
			b.WriteString("。")
		case "deferred":
			b.WriteString("已提交, 等待治理裁决: ")
			b.WriteString(label)
			b.WriteString("。")
		case "rejected":
			b.WriteString("未被接受: ")
			if reason := strings.TrimSpace(result.Err); reason != "" {
				b.WriteString(reason)
			} else {
				b.WriteString(label)
			}
			b.WriteString("。")
		case "skipped":
			b.WriteString("本回合未提交治理事件(节点未接入)。")
		default:
			b.WriteString("操作未完成: 提交未成功, 详情见本地日志。")
		}
	}
	return strings.TrimSpace(b.String())
}

func RuntimePrincipalLabel(principal string) string {
	principal = strings.TrimSpace(principal)
	if principal == "" {
		return "the resolved principal"
	}
	return principal
}

func RuntimeIssueLabel(id, identifier, title string) string {
	if strings.TrimSpace(identifier) != "" && strings.TrimSpace(title) != "" {
		return strings.TrimSpace(identifier) + " (" + strings.TrimSpace(title) + ")"
	}
	if strings.TrimSpace(identifier) != "" {
		return strings.TrimSpace(identifier)
	}
	if strings.TrimSpace(id) != "" {
		return strings.TrimSpace(id)
	}
	return "the Multica issue"
}
