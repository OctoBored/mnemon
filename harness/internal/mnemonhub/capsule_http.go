package mnemonhub

// capsule_http.go — the capsule-native hub wire (r4-hub-protocol-v1 §1-§5).
// Endpoint/status/problem vocabularies are spec closed sets; nothing here
// improvises. The legacy /sync three-verb wire is mounted separately and
// dies at S4d.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/mnemon-dev/mnemon/harness/internal/blob"
	"github.com/mnemon-dev/mnemon/harness/internal/capsule"
	"github.com/mnemon-dev/mnemon/harness/internal/contract"
)

const (
	HubProtocolHeader  = "X-Mnemon-Hub-Protocol"
	HubProtocolVersion = "v1"
	maxCapsuleBytes    = 1 << 20  // 1 MiB capsule body
	maxHubBlobBytes    = 64 << 20 // 64 MiB per blob
)

func writeProblem(w http.ResponseWriter, p *Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// RegisterCapsuleHandlers mounts the v1 capsule wire onto mux.
func RegisterCapsuleHandlers(mux *http.ServeMux, hub *Server, auth Authenticator, blobs *blob.Store, audit io.Writer) {
	if audit == nil {
		audit = io.Discard
	}
	mux.HandleFunc("/capsules", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(HubProtocolHeader, HubProtocolVersion)
		principal, err := auth.Authenticate(r)
		if err != nil {
			auditLine(audit, "-", "capsules", "unauthorized")
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case http.MethodPost:
			r.Body = http.MaxBytesReader(w, r.Body, maxCapsuleBytes)
			raw, readErr := io.ReadAll(r.Body)
			if readErr != nil {
				auditLine(audit, string(principal), "capsules.push", "bad_request")
				writeProblem(w, &Problem{Type: ProblemEnvelopeMalformed, Title: "body unreadable or over 1 MiB", Status: http.StatusRequestEntityTooLarge, Detail: readErr.Error()})
				return
			}
			env, decodeErr := capsule.DecodeEnvelope(raw)
			if decodeErr != nil {
				auditLine(audit, string(principal), "capsules.push", "bad_request")
				writeProblem(w, &Problem{Type: ProblemEnvelopeMalformed, Title: "not a DSSE envelope", Status: 422, Detail: decodeErr.Error()})
				return
			}
			result, problem := hub.PushCapsule(principal, raw, env, blobs)
			if problem != nil {
				auditLine(audit, string(principal), "capsules.push", "rejected")
				writeProblem(w, problem)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if result.Replayed {
				w.Header().Set("Idempotency-Replayed", "true")
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusCreated)
			}
			auditLine(audit, string(principal), "capsules.push", "ok")
			_ = json.NewEncoder(w).Encode(map[string]any{"accepted": result})
		case http.MethodGet:
			cursor, limit, parseProblem := feedParams(r)
			if parseProblem != nil {
				auditLine(audit, string(principal), "capsules.pull", "bad_request")
				writeProblem(w, parseProblem)
				return
			}
			feed, problem := hub.PullCapsules(principal, cursor, limit, func(capsuleID string) {
				auditLine(audit, string(principal), "capsules.clamp", capsuleID)
			})
			if problem != nil {
				auditLine(audit, string(principal), "capsules.pull", "denied")
				writeProblem(w, problem)
				return
			}
			etag := capsuleFeedETag(principal, feed)
			if match := strings.TrimSpace(r.Header.Get("If-None-Match")); match != "" && match == etag {
				w.Header().Set("ETag", etag)
				w.WriteHeader(http.StatusNotModified)
				auditLine(audit, string(principal), "capsules.pull", "not_modified")
				return
			}
			w.Header().Set("ETag", etag)
			w.Header().Set("Content-Type", "application/json")
			auditLine(audit, string(principal), "capsules.pull", "ok")
			// items are raw envelope documents; re-encode them as JSON array members verbatim
			raw := make([]json.RawMessage, 0, len(feed.Items))
			for _, item := range feed.Items {
				raw = append(raw, json.RawMessage(item))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": raw, "next_cursor": feed.NextCursor})
		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/capsules/rejected", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(HubProtocolHeader, HubProtocolVersion)
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		principal, err := auth.Authenticate(r)
		if err != nil {
			auditLine(audit, "-", "capsules.rejected", "unauthorized")
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		cursor, limit, parseProblem := feedParams(r)
		if parseProblem != nil {
			writeProblem(w, parseProblem)
			return
		}
		feed, problem := hub.PullRejected(principal, cursor, limit)
		if problem != nil {
			auditLine(audit, string(principal), "capsules.rejected", "denied")
			writeProblem(w, problem)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		auditLine(audit, string(principal), "capsules.rejected", "ok")
		_ = json.NewEncoder(w).Encode(feed)
	})
	mux.HandleFunc("/blobs/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(HubProtocolHeader, HubProtocolVersion)
		principal, err := auth.Authenticate(r)
		if err != nil {
			auditLine(audit, "-", "blobs", "unauthorized")
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		digest := strings.TrimPrefix(r.URL.Path, "/blobs/")
		switch r.Method {
		case http.MethodPut:
			if _, ok := hub.grants.Grant(principal, contract.SyncVerbPush); !ok {
				auditLine(audit, string(principal), "blobs.put", "denied")
				http.Error(w, "no push grant", http.StatusForbidden)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxHubBlobBytes)
			data, readErr := io.ReadAll(r.Body)
			if readErr != nil {
				http.Error(w, readErr.Error(), http.StatusRequestEntityTooLarge)
				return
			}
			if blobs.Has(digest) {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if err := blobs.PutExpect(digest, data); err != nil {
				auditLine(audit, string(principal), "blobs.put", "digest_mismatch")
				writeProblem(w, &Problem{Type: ProblemBlobDigestMismatch, Title: "blob bytes do not match digest", Status: http.StatusBadRequest, Detail: err.Error()})
				return
			}
			auditLine(audit, string(principal), "blobs.put", "ok")
			w.WriteHeader(http.StatusCreated)
		case http.MethodGet, http.MethodHead:
			if _, ok := hub.grants.Grant(principal, contract.SyncVerbPull); !ok {
				auditLine(audit, string(principal), "blobs.get", "denied")
				http.Error(w, "no pull grant", http.StatusForbidden)
				return
			}
			data, getErr := blobs.Get(digest)
			if getErr != nil {
				http.Error(w, "blob not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			w.Header().Set("ETag", `"`+digest+`"`)
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			if r.Method == http.MethodHead {
				return
			}
			auditLine(audit, string(principal), "blobs.get", "ok")
			_, _ = w.Write(data)
		default:
			w.Header().Set("Allow", "GET, HEAD, PUT")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead && r.URL.Path == "/" {
			w.Header().Set(HubProtocolHeader, HubProtocolVersion)
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	})
}

func feedParams(r *http.Request) (int64, int, *Problem) {
	cursor := int64(0)
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			return 0, 0, &Problem{Type: ProblemEnvelopeMalformed, Title: "cursor must be a non-negative integer", Status: http.StatusBadRequest, Detail: fmt.Sprintf("cursor=%q", raw)}
		}
		cursor = parsed
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return 0, 0, &Problem{Type: ProblemEnvelopeMalformed, Title: "limit must be a positive integer", Status: http.StatusBadRequest, Detail: fmt.Sprintf("limit=%q", raw)}
		}
		limit = parsed
	}
	return cursor, limit, nil
}

// capsuleFeedETag folds the cursor and the served set into a strong ETag.
func capsuleFeedETag(principal contract.ActorID, feed CapsuleFeed) string {
	h := sha256.New()
	fmt.Fprintf(h, "%v|%d|", principal, feed.NextCursor)
	for _, item := range feed.Items {
		fmt.Fprintf(h, "%d:", len(item))
		io.WriteString(h, item)
	}
	return `"` + hex.EncodeToString(h.Sum(nil))[:32] + `"`
}

// NewProtocolHandler mounts the full hub wire: the v1 capsule protocol plus
// (until S4d removes it) the legacy /sync three-verb wire.
func NewProtocolHandler(hub *Server, auth Authenticator, blobs *blob.Store, audit io.Writer) http.Handler {
	mux := http.NewServeMux()
	RegisterCapsuleHandlers(mux, hub, auth, blobs, audit)
	mux.Handle("/sync/", NewHTTPHandler(hub, auth, audit))
	return mux
}
