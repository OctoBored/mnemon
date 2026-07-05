// Package nodecli is the Local Mnemon event node's lifecycle CLI, shared by
// the R4 `mnemon-harness node` group and (through the compat window) the thin
// mnemond binary. It owns the daemon verbs (up/down/reload/status/logs), the
// doctor, the foreground serve, and the managed-agent wake source. One daemon
// per project store (the store's single-writer flock enforces it).
package nodecli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/mnemon-dev/mnemon/harness/internal/app"
)

// ServeChildArgv is the argv prefix `up` re-execs its detached serve child
// with. The mnemond shim keeps the default; the mnemon-harness node group
// overrides it to {"node", "serve"} so the child re-enters the right face.
var ServeChildArgv = []string{"serve"}

// FaceName is the command face shown in usage text; the mnemond shim keeps
// the default, the mnemon-harness node group sets "mnemon-harness node".
var FaceName = "mnemond"

// (bare flags, or an explicit `serve` — what `up` re-execs as the detached child). Keeping bare flags
// = foreground serve preserves the `local run` alias contract and the boot/T1 smoke tests.
// Run dispatches the node lifecycle verbs; bare flags foreground-serve.
func Run(ctx context.Context, args []string, out, errw io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "up":
			if err := Up(args[1:], out, errw); err != nil {
				if errors.Is(err, flag.ErrHelp) {
					return nil
				}
				return err
			}
			return nil
		case "down":
			if err := Down(args[1:], out, errw); err != nil {
				if errors.Is(err, flag.ErrHelp) {
					return nil
				}
				return err
			}
			return nil
		case "reload":
			if err := Reload(args[1:], out, errw); err != nil {
				if errors.Is(err, flag.ErrHelp) {
					return nil
				}
				return err
			}
			return nil
		case "status":
			if err := Status(args[1:], out, errw); err != nil {
				if errors.Is(err, flag.ErrHelp) {
					return nil
				}
				return err
			}
			return nil
		case "doctor":
			if err := Doctor(args[1:], out, errw); err != nil {
				if errors.Is(err, flag.ErrHelp) {
					return nil
				}
				return err
			}
			return nil
		case "logs":
			if err := Logs(args[1:], out, errw); err != nil {
				if errors.Is(err, flag.ErrHelp) {
					return nil
				}
				return err
			}
			return nil
		case "agent":
			if err := RunAgent(ctx, args[1:], out, errw); err != nil {
				if errors.Is(err, flag.ErrHelp) {
					return nil
				}
				return err
			}
			return nil
		case "help":
			if _, err := parseServe([]string{"--help"}, errw); err != nil && !errors.Is(err, flag.ErrHelp) {
				return err
			}
			return nil
		case "serve":
			args = args[1:]
		}
	}
	cfg, err := parseServe(args, errw)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	return serveForeground(ctx, cfg, out)
}

// serveConfig is the resolved foreground-serve plan, shared by the foreground path and the `up`
// pre-flight (so `up` reports setup/T1 errors in the foreground before it detaches).
type serveConfig struct {
	projectRoot         string
	listenAddr          string
	boot                app.LocalBoot
	ignoreExternal      bool
	allowInsecureRemote bool
	syncInterval        time.Duration
}

// parseServe parses the `local run`-equivalent flag face and resolves the SAME boot chain
// (ResolveLocalBoot, endpoint-derived listen address, T1 loopback validation), returning the plan or
// the first boot/validation error — the seam both `serve` and `up` share.
func parseServe(args []string, errw io.Writer) (serveConfig, error) {
	fs := flag.NewFlagSet(FaceName, flag.ContinueOnError)
	fs.SetOutput(errw)
	root := fs.String("root", ".", "project root")
	addr := fs.String("addr", "127.0.0.1:8787", "listen address")
	syncInterval := fs.Duration("sync-interval", 0, "sync worker cadence (0 = default 30s)")
	allowNonLoopback := fs.Bool("allow-nonloopback", false, "explicitly allow listening on a non-loopback address (T1: loopback-only by default)")
	ignoreExternal := fs.Bool("ignore-external", false, "boot the embedded-only capability catalog, ignoring external packages under .mnemon/loops (each ignored package is named on stderr)")
	allowInsecureRemote := fs.Bool("allow-insecure-remote", false, "let the background sync worker use a plaintext http:// Remote Workspace endpoint with a non-loopback host (T2: fail-closed by default)")
	fs.Usage = func() {
		writeMnemondHelp(errw, fs)
	}
	if err := fs.Parse(args); err != nil {
		return serveConfig{}, err
	}
	projectRoot := "."
	if *root != "" {
		projectRoot = filepath.Clean(*root)
	}
	boot, err := app.ResolveLocalBoot(projectRoot, "", "")
	if err != nil {
		return serveConfig{}, err
	}
	listenAddr := *addr
	addrChanged := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "addr" {
			addrChanged = true
		}
	})
	if !addrChanged {
		listenAddr = app.ListenAddrFromEndpoint(boot.Config.Endpoint, *addr)
	}
	if err := app.ValidateListenAddr(listenAddr, *allowNonLoopback); err != nil {
		return serveConfig{}, err
	}
	return serveConfig{
		projectRoot:         projectRoot,
		listenAddr:          listenAddr,
		boot:                boot,
		ignoreExternal:      *ignoreExternal,
		allowInsecureRemote: *allowInsecureRemote,
		syncInterval:        *syncInterval,
	}, nil
}

func writeMnemondHelp(errw io.Writer, fs *flag.FlagSet) {
	fmt.Fprintln(errw, FaceName+" is the Local Mnemon event node: it serves local event API, admission, state, presentation, and drive candidates.")
	fmt.Fprintln(errw)
	fmt.Fprintln(errw, "Usage:")
	fmt.Fprintln(errw, "  "+FaceName+" serve [flags]")
	fmt.Fprintln(errw, "  "+FaceName+" [flags]")
	fmt.Fprintln(errw, "  "+FaceName+" up|down|reload|status|doctor|logs [flags]")
	fmt.Fprintln(errw, "  "+FaceName+" agent run [flags]")
	fmt.Fprintln(errw)
	fmt.Fprintln(errw, "Commands:")
	fmt.Fprintln(errw, "  serve       Run the local event node in the foreground")
	fmt.Fprintln(errw, "  up          Start the local event node in the background")
	fmt.Fprintln(errw, "  down        Stop the background local event node")
	fmt.Fprintln(errw, "  reload      Restart the background local event node")
	fmt.Fprintln(errw, "  status      Show background local event node status")
	fmt.Fprintln(errw, "  doctor      Check local event node readiness")
	fmt.Fprintln(errw, "  logs        Show background local event node logs")
	fmt.Fprintln(errw, "  agent run   Local managed-agent drive source using [mnemon:wake]")
	fmt.Fprintln(errw)
	fmt.Fprintln(errw, "Flags:")
	if fs != nil {
		fs.PrintDefaults()
	}
}

// serveForeground runs the governed HTTP server in the foreground until ctx cancels — the body of
// `mnemond serve` and the process the daemon child runs.
func serveForeground(ctx context.Context, cfg serveConfig, out io.Writer) error {
	fmt.Fprintln(out, "Local Mnemon: ready")
	fmt.Fprintln(out, "Remote Workspace: "+app.RemoteWorkspaceStatus(cfg.projectRoot))
	return app.RunLocalHTTPServerWithBindings(ctx, cfg.listenAddr, cfg.boot.StorePath, cfg.boot.Loaded, app.ServeOptions{
		Loops:               cfg.boot.Config.Loops,
		ProjectRoot:         cfg.projectRoot,
		IgnoreExternal:      cfg.ignoreExternal,
		AllowInsecureRemote: cfg.allowInsecureRemote,
		SyncInterval:        cfg.syncInterval,
	}, io.Discard)
}
