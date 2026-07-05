package presentation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mnemon-dev/mnemon/harness/internal/contract"
	eventmodel "github.com/mnemon-dev/mnemon/harness/internal/event"
	"github.com/mnemon-dev/mnemon/harness/internal/mnemond/presentation/view"
)

type Status string

const (
	StatusOK       Status = "ok"
	StatusFallback Status = "fallback"
	StatusEmpty    Status = "empty"
	StatusDenied   Status = "denied"
)

type Request struct {
	SchemaVersion int
	Principal     contract.ActorID
	Host          string
	Lifecycle     string
	Surface       string
	RenderIntent  string
	SessionID     string
	InputDigest   string
	Budget        Budget
	Client        ClientInfo
}

type Budget struct {
	MaxChars int
}

type ClientInfo struct {
	ShimVersion string
	Supports    []string
}

type Response struct {
	SchemaVersion          int
	Status                 Status
	Body                   string
	Events                 []eventmodel.EventEnvelope
	BodyFormat             string
	BodyDigest             string
	PresentationViewDigest string
	Provenance             Provenance
	AuditID                string
	TTLSeconds             int
}

type Provenance struct {
	Source        string
	CatalogDigest string
	DecisionHead  string
	RenderedAt    string
}

type Renderer struct {
	Now       func() time.Time
	AuditSink AuditSink
}

func (r Renderer) RenderPresentation(ctx context.Context, req Request, proj view.View) (Response, error) {
	now := time.Now().UTC()
	if r.Now != nil {
		now = r.Now().UTC()
	}
	if req.Principal == "" {
		return Response{SchemaVersion: 1, Status: StatusDenied, PresentationViewDigest: proj.Digest}, nil
	}
	body, events := BuildBodyAndEventEnvelopes(req, proj, now)
	truncatedFrom := 0
	if req.Budget.MaxChars > 0 && len(body) > req.Budget.MaxChars {
		truncatedFrom = len(body)
		body = truncateRuneSafe(body, req.Budget.MaxChars)
	}
	resp := Response{
		SchemaVersion:          1,
		Status:                 StatusOK,
		Body:                   body,
		Events:                 events,
		BodyFormat:             "plain_text",
		BodyDigest:             digest(body),
		PresentationViewDigest: proj.Digest,
		Provenance: Provenance{
			Source:        "local-mnemon",
			CatalogDigest: digest("render-intents:v1"),
			DecisionHead:  proj.Ref,
			RenderedAt:    now.Format(time.RFC3339),
		},
		TTLSeconds: 300,
	}
	if strings.TrimSpace(body) == "" {
		resp.Status = StatusEmpty
		resp.BodyDigest = ""
		return resp, nil
	}
	resp.AuditID = auditID(req, resp)
	if r.AuditSink != nil {
		record := AuditRecordFrom(req, resp, presentationSectionCounts(body), eventEnvelopeCounts(events))
		record.TruncatedFromChars = truncatedFrom
		if err := r.AuditSink.WriteRenderAudit(ctx, record); err != nil {
			return Response{}, err
		}
	}
	return resp, nil
}

// truncateRuneSafe cuts body to at most max bytes WITHOUT splitting a UTF-8
// character (C4: a CJK brief must never truncate into replacement bytes).
// The truncation fact rides the render audit — E1-era truncation left no trace.
func truncateRuneSafe(body string, max int) string {
	if max <= 0 || len(body) <= max {
		return body
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(body[cut]) {
		cut--
	}
	return body[:cut]
}

func digest(body string) string {
	sum := sha256.Sum256([]byte(body))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func auditID(req Request, resp Response) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%s|%s", req.Principal, req.Lifecycle, req.RenderIntent, resp.PresentationViewDigest, resp.BodyDigest)))
	return "render_" + hex.EncodeToString(sum[:8])
}
