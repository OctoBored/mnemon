package mnemonhub

import (
	"crypto/subtle"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mnemon-dev/mnemon/harness/internal/contract"
)

// maxSyncBodyBytes caps a sync request body so an oversize batch is rejected at the edge rather
// than buffered into memory (mirrors the channel's ingest cap; a 100-event pull page fits easily).
const maxSyncBodyBytes = 8 << 20

// Authenticator resolves the authenticated principal from a request. mnemonhub carries its OWN
// seam (not channel's) so the standalone hub never imports channel; mnemon-hub plugs in
// BearerAuthenticator, tests may plug fakes.
type Authenticator interface {
	Authenticate(r *http.Request) (contract.ActorID, error)
}

// BearerAuthenticator resolves the principal from a static bearer-token map — the mnemon-hub
// authenticator built from replicas.json credential_refs. A missing, empty, or unknown token is
// rejected; the request body never names identity.
type BearerAuthenticator struct {
	Tokens map[string]contract.ActorID
}

func (a BearerAuthenticator) Authenticate(r *http.Request) (contract.ActorID, error) {
	tok := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if tok == "" {
		return "", fmt.Errorf("unrecognized bearer token")
	}
	// Constant-time compare against the (small) registered token set: tokens are high-entropy so
	// this is hardening, but a map-lookup-only check leaks a length/match timing signal. Scan ALL
	// entries and accumulate the match (no early return on a hit) so the work is token-independent.
	tokBytes := []byte(tok)
	var matched contract.ActorID
	for known, principal := range a.Tokens {
		if subtle.ConstantTimeCompare(tokBytes, []byte(known)) == 1 && principal != "" {
			matched = principal
		}
	}
	if matched != "" {
		return matched, nil
	}
	return "", fmt.Errorf("unrecognized bearer token")
}

func auditLine(audit io.Writer, principal, verb, result string) {
	fmt.Fprintf(audit, "%s principal=%s verb=%s result=%s\n",
		time.Now().UTC().Format(time.RFC3339), principal, verb, result)
}
