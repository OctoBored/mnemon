package main

// registry.go — R4 command registry (r4-registries-and-guards §2): every
// runnable, non-deprecated command answers the audience×edge×endpoint
// triple before it may exist. The registry is DATA; registry_test.go is the
// guard that keeps it and the cobra tree in lockstep (missing row → red,
// zombie row → red, base-command vocabulary purity → red). Adding a command
// starts here: answer the triple, then build the command.

// CommandSpec pins one command's boundary answer.
type CommandSpec struct {
	Path     string // space-joined path under the root, e.g. "teamwork report"
	Audience string // agent | operator | human | debug
	Edge     string // submit | federate | node | none
	Endpoint string // closed set below; "-" = pure local, "*" = escape hatch
}

var commandAudiences = map[string]bool{"agent": true, "operator": true, "human": true, "debug": true}
var commandEdges = map[string]bool{"submit": true, "federate": true, "node": true, "none": true}
var commandEndpoints = map[string]bool{
	"-":                       true,
	"*":                       true,
	"/ingest":                 true,
	"POST /render":            true,
	"POST /presentation-view": true,
	"PUT/GET /blobs/{digest}": true,
	"GET /surface/deliveries": true,
	"GET /status":             true,
	"POST /capsules (hub)":    true,
	"GET /capsules (hub)":     true,
}

var commandRegistry = []CommandSpec{
	// protocol layer — agent-facing verbs
	{Path: "emit", Audience: "agent", Edge: "submit", Endpoint: "/ingest"},
	{Path: "recall", Audience: "agent", Edge: "submit", Endpoint: "POST /presentation-view"},
	{Path: "view", Audience: "agent", Edge: "submit", Endpoint: "POST /render"},
	{Path: "verify", Audience: "human", Edge: "none", Endpoint: "-"},

	// capability dialect — teamwork (business vocabulary legal here)
	{Path: "teamwork signal", Audience: "agent", Edge: "submit", Endpoint: "/ingest"},
	{Path: "teamwork assign", Audience: "agent", Edge: "submit", Endpoint: "/ingest"},
	{Path: "teamwork report", Audience: "agent", Edge: "submit", Endpoint: "/ingest"},
	{Path: "teamwork profile", Audience: "agent", Edge: "submit", Endpoint: "/ingest"},

	// node lifecycle
	{Path: "node up", Audience: "operator", Edge: "node", Endpoint: "-"},
	{Path: "node down", Audience: "operator", Edge: "node", Endpoint: "-"},
	{Path: "node reload", Audience: "operator", Edge: "node", Endpoint: "-"},
	{Path: "node status", Audience: "operator", Edge: "node", Endpoint: "-"},
	{Path: "node logs", Audience: "operator", Edge: "node", Endpoint: "-"},
	{Path: "node doctor", Audience: "operator", Edge: "node", Endpoint: "-"},
	{Path: "node serve", Audience: "operator", Edge: "node", Endpoint: "-"},
	{Path: "node wake", Audience: "operator", Edge: "node", Endpoint: "POST /render"},

	// federation
	{Path: "push", Audience: "operator", Edge: "federate", Endpoint: "POST /capsules (hub)"},
	{Path: "pull", Audience: "operator", Edge: "federate", Endpoint: "GET /capsules (hub)"},
	{Path: "hub serve", Audience: "operator", Edge: "federate", Endpoint: "POST /capsules (hub)"},
	{Path: "hub doctor", Audience: "operator", Edge: "federate", Endpoint: "-"},
	{Path: "remote add", Audience: "operator", Edge: "federate", Endpoint: "-"},
	{Path: "remote list", Audience: "operator", Edge: "federate", Endpoint: "-"},
	{Path: "remote remove", Audience: "operator", Edge: "federate", Endpoint: "-"},

	// operator setup & health
	{Path: "setup", Audience: "operator", Edge: "none", Endpoint: "-"},
	{Path: "setup status", Audience: "operator", Edge: "none", Endpoint: "-"},
	{Path: "setup uninstall", Audience: "operator", Edge: "none", Endpoint: "-"},
	{Path: "config open", Audience: "operator", Edge: "none", Endpoint: "-"},
	{Path: "config validate", Audience: "operator", Edge: "none", Endpoint: "-"},
	{Path: "agent add", Audience: "operator", Edge: "none", Endpoint: "-"},
	{Path: "agent list", Audience: "operator", Edge: "none", Endpoint: "-"},
	{Path: "connect multica", Audience: "operator", Edge: "none", Endpoint: "-"},
	{Path: "connect mnemonhub", Audience: "operator", Edge: "federate", Endpoint: "-"},
	{Path: "doctor", Audience: "operator", Edge: "node", Endpoint: "-"},
	{Path: "status", Audience: "operator", Edge: "node", Endpoint: "GET /status"},

	// human boundary
	{Path: "watch", Audience: "human", Edge: "node", Endpoint: "-"},

	// debug faces (hidden, non-deprecated)
	{Path: "api", Audience: "debug", Edge: "submit", Endpoint: "*"},
	{Path: "local run", Audience: "debug", Edge: "node", Endpoint: "-"},
	{Path: "local status", Audience: "debug", Edge: "node", Endpoint: "-"},
	{Path: "local stop", Audience: "debug", Edge: "node", Endpoint: "-"},
	{Path: "token rotate", Audience: "debug", Edge: "none", Endpoint: "-"},
}
