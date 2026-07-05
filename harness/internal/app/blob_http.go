package app

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/mnemon-dev/mnemon/harness/internal/blob"
	"github.com/mnemon-dev/mnemon/harness/internal/mnemond/access"
)

// blobMaxBytes bounds a single artifact upload (hub protocol spec §4).
const blobMaxBytes = 64 << 20

// registerBlobHandlers wires the node-local content-addressed artifact lane:
// PUT stores by declared digest (fail-closed on mismatch), GET serves immutable
// bytes. Writes require the observe verb, reads the pull verb — the same
// binding that authorizes governed submissions authorizes the content that
// accompanies them.
func registerBlobHandlers(mux *http.ServeMux, auth access.Authenticator, bindings *access.BindingSet, blobs *blob.Store) {
	mux.HandleFunc("/blobs/", func(w http.ResponseWriter, r *http.Request) {
		if blobs == nil {
			http.Error(w, "blob store unavailable", http.StatusNotFound)
			return
		}
		principal, err := auth.Authenticate(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		digest := strings.TrimPrefix(r.URL.Path, "/blobs/")
		switch r.Method {
		case http.MethodPut:
			if bindings != nil {
				b, ok := bindings.Binding(principal)
				if !ok || !b.Allows(access.VerbObserve) {
					http.Error(w, fmt.Sprintf("principal %q is not bound to observe", principal), http.StatusForbidden)
					return
				}
			}
			data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, blobMaxBytes))
			if err != nil {
				http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
				return
			}
			if blobs.Has(digest) {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if err := blobs.PutExpect(digest, data); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusCreated)
		case http.MethodGet, http.MethodHead:
			if bindings != nil {
				b, ok := bindings.Binding(principal)
				if !ok || !b.Allows(access.VerbPull) {
					http.Error(w, fmt.Sprintf("principal %q is not bound to pull", principal), http.StatusForbidden)
					return
				}
			}
			data, err := blobs.Get(digest)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			w.Header().Set("ETag", `"`+digest+`"`)
			if r.Method == http.MethodHead {
				return
			}
			_, _ = w.Write(data)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}
