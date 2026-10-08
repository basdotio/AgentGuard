// SPDX-License-Identifier: MIT
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/basdotio/AgentGuard/internal/config"
	"github.com/basdotio/AgentGuard/internal/gate"
	"github.com/basdotio/AgentGuard/internal/model"
	"github.com/spf13/cobra"
)

// gateOptions assembles a gate run from the CLI's own wiring.
//
// The two scanners are the SAME functions `check` and `scan` call — not a lighter variant.
// A gate whose verdict differs from what `aguard check` prints for the same path would make
// both numbers useless, so there is deliberately no fast path here and nothing is trimmed.
//
// The judge is the one thing left out, and it stays out even though `check --llm` exists: the
// gate fires on every load under a deadline and fails open, so a model call would make each
// load slower, cost money nobody asked to spend, and give a verdict that depends on whether an
// endpoint answered. None of these scanOpts — nor approvePath's — may set llm; the gate's
// verdict is what `check` prints without --llm, which is also the deterministic half of what
// it prints with it (TestGateScannerNeverEnablesLLM).
func gateOptions(root, cfgPath string, store *gate.Store, now func() int64) (gate.Options, error) {
	cfg, err := config.LoadUser(cfgPath)
	if err != nil {
		return gate.Options{}, err
	}
	return gate.Options{
		Root:        root,
		Home:        filepath.Dir(root),
		Threshold:   model.Severity(cfg.Gate.FailOn),
		Action:      cfg.Gate.Action,
		ToolVersion: version,
		Now:         now,
		Store:       store,
		Scan: func(path string) (model.ScanResult, error) {
			return checkTarget(path, scanOpts{cfgPath: cfgPath, noReputation: false, quiet: true})
		},
		ScanRoot: func() (model.ScanResult, error) {
			return scanEnv(root, scanOpts{cfgPath: cfgPath, quiet: true})
		},
	}, nil
}

// gateLivenessNote reports a load-time gate that is registered but cannot run.
//
// It is attached to `scan` — not left to `aguard hook status` — because nobody runs a status
// command on a schedule, while `scan` is the command people already run. The state it catches
// looks installed and protects nothing, so leaving it to a command you have to think of asking
// would recreate the exact silence the gate exists to prevent (invariant #5).
//
// A failure to READ the settings file is swallowed on purpose: `scan` has its own, better
// reporting for an unparseable settings file (PARSE-000 from the collector), and a second
// error from a liveness probe would be noise about the same fact.
func gateLivenessNote(root string) []model.Finding {
	exe, err := resolvedExe()
	if err != nil {
		return nil
	}
	st, err := gate.CheckStatus(root, exe)
	if err != nil {
		return nil
	}
	if n := gate.DeadRegistrationNote(st); n != nil {
		return []model.Finding{*n}
	}
	return nil
}

// runHook is the hook runner: read one event on stdin, write one reply on stdout.
//
// It ALWAYS exits 0. Claude Code reads a non-zero exit from a PreToolUse hook as a block, so
// letting an internal failure surface as an exit code would turn every bug in this tool into
// an editor that cannot load skills — the failure mode most certain to get the gate removed
// (invariant #3). Blocking is expressed in the reply JSON and nowhere else; problems are
// expressed as a GATE-000 message the operator can actually read.
// preScanDeadline bounds the approvals-store and config reads that precede gate.Handle.
const preScanDeadline = 10 * time.Second

func runHook(stdin io.Reader, stdout io.Writer, root, cfgPath string) error {
	var ev gate.Event
	if err := json.NewDecoder(stdin).Decode(&ev); err != nil {
		return writeHookOutput(stdout, gate.Output{
			SystemMessage: "AgentGuard [GATE-000] hook payload could not be read; this call was NOT audited.",
		})
	}
	// The approvals store and the config are read BEFORE gate.Handle, so they sat outside
	// every deadline the gate applies to its own scan. Both reads now refuse
	// non-regular files up front (safeio), which removes the FIFO hang; the deadline here is
	// the backstop for a stall no guard can see, and it ends the same way every other gap
	// does — a GATE-000 the operator can read, never silence past the editor's own timeout.
	type loaded struct {
		store *gate.Store
		o     gate.Options
		err   error
	}
	ch := make(chan loaded, 1)
	go func() {
		st := gate.LoadStore(gate.ApprovalsPath(root))
		o, err := gateOptions(root, cfgPath, st, nowUnix)
		ch <- loaded{st, o, err}
	}()
	var store *gate.Store
	var o gate.Options
	select {
	case l := <-ch:
		if l.err != nil {
			return writeHookOutput(stdout, gate.Output{
				SystemMessage: "AgentGuard [GATE-000] config error, this call was NOT audited: " + l.err.Error(),
			})
		}
		store, o = l.store, l.o
	case <-time.After(preScanDeadline):
		return writeHookOutput(stdout, gate.Output{
			SystemMessage: fmt.Sprintf("AgentGuard [GATE-000] reading the approvals store or config did not finish within %s; this call was NOT audited.", preScanDeadline),
		})
	}

	out, dirty := gate.Handle(ev, o)
	if dirty {
		if serr := store.Save(); serr != nil {
			// Saying so matters more than it looks: an unsaved approval means the same
			// prompt returns next time, and an operator who cannot tell why will conclude
			// the gate is broken and switch it off.
			out.SystemMessage = appendLine(out.SystemMessage,
				"AgentGuard [GATE-000] the decision could not be saved ("+serr.Error()+"); it will be asked again.")
		}
	}
	if store.Corrupt != "" {
		out.SystemMessage = appendLine(out.SystemMessage, "AgentGuard [GATE-000] "+store.Corrupt)
	}
	return writeHookOutput(stdout, out)
}

func appendLine(s, line string) string {
	if s == "" {
		return line
	}
	return s + "\n" + line
}

// writeHookOutput emits the reply. An empty reply is emitted as nothing at all rather than
// as "{}", so the common path (already-approved content) costs the editor no parsing and
// shows the operator no message.
func writeHookOutput(w io.Writer, out gate.Output) error {
	if out.HookSpecificOutput == nil && out.SystemMessage == "" {
		return nil
	}
	return json.NewEncoder(w).Encode(out)
}

func newGateCommands(root, cfgPath *string, quiet *bool) []*cobra.Command {
	hookCmd := &cobra.Command{
		Use:   "hook",
		Short: "Hook runner: audit a skill before it loads (reads a Claude Code hook event on stdin)",
		Long: "Reads one hook event on stdin and writes the decision on stdout.\n\n" +
			"One command serves every event the gate uses — it dispatches on hook_event_name — so\n" +
			"registering it needs no shell pipeline in settings.json. Run `aguard hook install` to\n" +
			"register it, or see docs/install-gate.md for the settings block to add by hand.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runHook(os.Stdin, os.Stdout, *root, *cfgPath)
		},
	}

	var dryRun bool
	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Register the load-time gate in <root>/settings.json (merges; backs up first)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return applyHookPlan(*root, dryRun, true)
		},
	}
	installCmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the settings change without writing it")

	uninstallCmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the gate's hook entries from <root>/settings.json (matches on command; leaves other hooks alone)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return applyHookPlan(*root, dryRun, false)
		},
	}
	uninstallCmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the settings change without writing it")

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Is the load-time gate actually wired up? (exit 1 if not, or if it points at a missing binary)",
		Long: "Reports whether settings.json registers this gate and whether the command it names\n" +
			"still exists.\n\n" +
			"The registration bakes in an ABSOLUTE path, and a path can stop resolving — the binary\n" +
			"moves, a build directory is cleaned, dotfiles land on a machine that never had it. Then\n" +
			"Claude Code runs a command that is not there, every skill loads unaudited, and the result\n" +
			"looks exactly like a clean environment. This is where you ask.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			exe, err := resolvedExe()
			if err != nil {
				return err
			}
			st, err := gate.CheckStatus(*root, exe)
			if err != nil {
				return err
			}
			st.Describe(os.Stdout)
			// Same disclosure as install: asking "is the gate wired up?" from a disposable
			// binary deserves the answer that the wiring cannot outlive the binary.
			if w := ephemeralExeWarning(exe); w != "" {
				fmt.Fprintln(os.Stderr, "\n"+w)
			}
			if !st.Active() {
				return &failExit{code: 1}
			}
			return nil
		},
	}
	hookCmd.AddCommand(installCmd, uninstallCmd, statusCmd)

	approveCmd := &cobra.Command{
		Use:   "approve <path>",
		Short: "Trust the exact current contents of a skill/dir/file, so the gate stops asking about it",
		Long: "Scans the target and records its canonical hash as approved.\n\n" +
			"The approval covers those BYTES, not that name: an update, a re-install or an edit\n" +
			"produces a different hash and the gate asks again by itself.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return approvePath(os.Stdout, *root, *cfgPath, args[0])
		},
	}

	approvalsCmd := &cobra.Command{
		Use:   "approvals",
		Short: "List what the load-time gate has been told to trust",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return listApprovals(os.Stdout, *root, *quiet)
		},
	}
	forgetCmd := &cobra.Command{
		Use:   "forget <hash|all>",
		Short: "Withdraw an approval so the gate asks about that content again",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return forgetApproval(os.Stdout, *root, args[0])
		},
	}
	approvalsCmd.AddCommand(forgetCmd)

	return []*cobra.Command{hookCmd, approveCmd, approvalsCmd}
}

// resolvedExe returns this binary's real absolute path.
//
// Symlinks are resolved so the registration survives the link being repointed or removed;
// the path is needed at all because the editor's PATH is not the shell's, and a hook it
// cannot find fails silently — which is a gate that is not there.
func resolvedExe() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot determine this binary's path (needed because the editor's PATH is not the shell's): %w", err)
	}
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		exe = resolved
	}
	return exe, nil
}

// ephemeralSegments are path segments that mean "a package manager put this binary in a cache
// it will later reclaim". `_npx` is npm's per-invocation cache and `_cacache` its content store.
//
// The list is deliberately short. A warning that fires on a legitimate path teaches operators
// to skip warnings, and this is one they need to read.
var ephemeralSegments = []string{"_npx", "_cacache"}

// ephemeralExeWarning reports whether this binary is running from a disposable location, which
// matters only for `hook install`: the registration bakes in an absolute path, so a gate
// installed from `npx …` keeps working exactly until npm reclaims that cache — after which
// Claude Code runs a command that is not there, every skill loads unaudited, and the result is
// indistinguishable from a clean environment. That is the GATE-001 shape, and the npm
// distribution is what makes it easy to reach, so it is named here rather than left to a
// README nobody re-reads.
func ephemeralExeWarning(exe string) string {
	for _, seg := range strings.Split(filepath.ToSlash(exe), "/") {
		if slices.Contains(ephemeralSegments, seg) {
			return "WARNING: this binary is running from a package-manager cache:\n" +
				"  " + exe + "\n" +
				"That path is temporary. A gate registered against it keeps working only until the\n" +
				"cache is reclaimed; after that every skill loads unaudited and the setup still looks\n" +
				"protected (a scan reports it as GATE-001).\n" +
				"Install to a stable path first — `npm i -g`, or a release binary — then run this again."
		}
	}
	return ""
}

func applyHookPlan(root string, dryRun, install bool) error {
	exe, err := resolvedExe()
	if err != nil {
		return err
	}
	// Shown for the real install and the dry run alike: the dry run is where an operator is
	// deciding, which is the moment this changes their mind about how they installed the tool.
	if install {
		if w := ephemeralExeWarning(exe); w != "" {
			fmt.Fprintln(os.Stderr, w)
			fmt.Fprintln(os.Stderr)
		}
	}
	cmdline := gate.HookCommand(exe)

	var plan gate.InstallPlan
	if install {
		plan, err = gate.PlanInstall(root, cmdline)
	} else {
		plan, err = gate.PlanUninstall(root, cmdline)
	}
	if err != nil {
		return err
	}
	if !dryRun {
		if err := plan.Apply(); err != nil {
			return err
		}
	}
	plan.Describe(os.Stdout, !dryRun)
	if install && plan.Changed() && !dryRun {
		fmt.Println("\nThe gate is live in NEW sessions; restart Claude Code to pick it up.")
	}
	return nil
}

// approvePath is the manual half of the gate: it lets an operator clear something ahead of
// time, or accept a risk from the terminal instead of at a prompt.
//
// It scans before recording. There is no way to hand this command a hash — an approval must
// always be backed by bytes this process read (gate invariant #2), or the store stops meaning
// "someone looked at this".
func approvePath(w io.Writer, root, cfgPath, target string) error {
	res, err := checkTarget(target, scanOpts{cfgPath: cfgPath, quiet: true})
	if err != nil {
		return err
	}
	cfg, err := config.LoadUser(cfgPath)
	if err != nil {
		return err
	}
	v, ok := gate.Summarize(res, model.Severity(cfg.Gate.FailOn))
	if !ok {
		return fmt.Errorf("nothing could be collected from %s — refusing to approve a target that was not read", target)
	}
	store := gate.LoadStore(gate.ApprovalsPath(root))
	if store.Corrupt != "" {
		return fmt.Errorf("%s", store.Corrupt)
	}
	verdict := gate.VerdictClean
	if v.Blocking {
		verdict = gate.VerdictAccepted
	}
	store.Approve(gate.Approval{
		Hash: v.Hash, Name: v.Name, Kind: v.Kind, Path: v.Path, Score: v.Score,
		Verdict: verdict, ApprovedAt: nowUnix(), ToolVersion: version,
	})
	if err := store.Save(); err != nil {
		return err
	}
	fmt.Fprintf(w, "approved %s %q (%d/100, %s)\n  hash %s\n  store %s\n",
		v.Kind, v.Name, v.Score, verdict, v.Hash, gate.ApprovalsPath(root))
	if v.Blocking {
		fmt.Fprintf(w, "\nNote: this target has findings at or above %s — you accepted a risk rather than cleared one.\n"+
			"Run `aguard check %q` to see what they are.\n", cfg.Gate.FailOn, v.Path)
	}
	return nil
}

func listApprovals(w io.Writer, root string, quiet bool) error {
	path := gate.ApprovalsPath(root)
	store := gate.LoadStore(path)
	if store.Corrupt != "" {
		return fmt.Errorf("%s", store.Corrupt)
	}
	list := store.List()
	if len(list) == 0 {
		if !quiet {
			fmt.Fprintf(w, "no approvals recorded (%s)\n", path)
		}
		return nil
	}
	fmt.Fprintf(w, "%s — %d approval(s)\n", path, len(list))
	for _, a := range list {
		fmt.Fprintf(w, "  %-16s %-13s %3d/100  %s  %s\n", a.Hash[:min(16, len(a.Hash))], a.Verdict, a.Score, a.Kind, a.Name)
	}
	return nil
}

func forgetApproval(w io.Writer, root, hash string) error {
	path := gate.ApprovalsPath(root)
	store := gate.LoadStore(path)
	if store.Corrupt != "" {
		return fmt.Errorf("%s", store.Corrupt)
	}
	if hash == "all" {
		n := len(store.Approvals)
		store.Approvals = map[string]gate.Approval{}
		if err := store.Save(); err != nil {
			return err
		}
		fmt.Fprintf(w, "forgot %d approval(s); the gate will ask about everything again\n", n)
		return nil
	}
	// A prefix is accepted because that is what the messages print — requiring the full
	// hash would mean the one place an operator can copy it from is the file itself.
	full, err := resolveHashPrefix(store, hash)
	if err != nil {
		return err
	}
	store.Forget(full)
	if err := store.Save(); err != nil {
		return err
	}
	fmt.Fprintf(w, "forgot %s; the gate will ask about that content again\n", full)
	return nil
}

func resolveHashPrefix(store *gate.Store, prefix string) (string, error) {
	var hits []string
	for h := range store.Approvals {
		if len(h) >= len(prefix) && h[:len(prefix)] == prefix {
			hits = append(hits, h)
		}
	}
	switch len(hits) {
	case 0:
		return "", fmt.Errorf("no approval matches %q", prefix)
	case 1:
		return hits[0], nil
	default:
		return "", fmt.Errorf("%q matches %d approvals; use more characters", prefix, len(hits))
	}
}
