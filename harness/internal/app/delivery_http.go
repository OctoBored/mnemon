package app

// delivery_http.go — GET /surface/deliveries (r4-multica-adapter-contract
// §1): the Surface Delivery Feed endpoint. Read-only projection of the
// accepted-decision log for one display surface; authorization rides the
// pull verb on the adapter's own binding. v1 has no outbox/lease/ack —
// the adapter's ledger provides idempotency, the cursor provides resume.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/mnemon-dev/mnemon/harness/internal/contract"
	"github.com/mnemon-dev/mnemon/harness/internal/mnemond/access"
	"github.com/mnemon-dev/mnemon/harness/internal/mnemond/presentation"
	"github.com/mnemon-dev/mnemon/harness/internal/runtime"
)

func registerDeliveryHandlers(mux *http.ServeMux, rt *runtime.Runtime, auth access.Authenticator, bindings *access.BindingSet) {
	mux.HandleFunc("/surface/deliveries", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		principal, err := auth.Authenticate(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		if bindings != nil {
			binding, ok := bindings.Binding(principal)
			if !ok {
				http.Error(w, fmt.Sprintf("no channel binding for principal %q", principal), http.StatusForbidden)
				return
			}
			if !binding.Allows(access.VerbPull) {
				http.Error(w, fmt.Sprintf("principal %q is not bound to pull", principal), http.StatusForbidden)
				return
			}
		}
		surface := r.URL.Query().Get("surface")
		if surface == "" {
			http.Error(w, "surface query parameter is required", http.StatusBadRequest)
			return
		}
		cursor := int64(0)
		if raw := r.URL.Query().Get("cursor"); raw != "" {
			parsed, parseErr := strconv.ParseInt(raw, 10, 64)
			if parseErr != nil || parsed < 0 {
				http.Error(w, "cursor must be a non-negative integer", http.StatusBadRequest)
				return
			}
			cursor = parsed
		}
		rows, err := rt.DecisionsAfter(cursor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sources := make([]presentation.AcceptedDecisionSource, 0, len(rows))
		for _, row := range rows {
			if row.Decision.Status != contract.Accepted {
				continue
			}
			sources = append(sources, presentation.AcceptedDecisionSource{
				Cursor:     row.Rowid,
				DecisionID: row.Decision.DecisionID,
				Resources:  row.Decision.NewResources,
			})
		}
		feed := presentation.ReduceDeliveries(sources, surface, cursor)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(feed)
	})
}
