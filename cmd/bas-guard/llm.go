// SPDX-License-Identifier: MIT
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/basdotio/bas-guard/internal/config"
	"github.com/basdotio/bas-guard/internal/detect"
	"github.com/basdotio/bas-guard/internal/judge"
	"github.com/basdotio/bas-guard/internal/model"
	"github.com/basdotio/bas-guard/internal/redact"
)

// `aguard llm` — set up, test and inspect the optional LLM judge without editing YAML.
//
// The judge used to be configured by hand: write a file, know an endpoint URL and a model
// name, export an environment variable, pass --config and --llm every run. Each of those is a
// step a non-developer cannot take, and the plugin never mentioned the judge at all, so from
// inside Claude Code there was no path to it. These three subcommands are the primitives the
// plugin's `/aguard-llm` flow drives: `setup` writes the config and the key file, `test` makes
// one call so a wrong key or a renamed model fails now and not at the end of a scan, `status`
// says what is configured without ever printing the key.
//
// What does not change: the judge still runs only with --llm, still sends redacted excerpts
// only, and still cannot move the deterministic score (invariant #4). Setup prints where the
// content will go before anything is sent, because for a hosted model that is the one fact the
// user has to have consciously accepted.

func newLLMCommand(cfgPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "llm",
		Short: "Set up, test or inspect the optional LLM judge (a hosted model you provide)",
	}

	var provider, model, baseURL, keyFile string
	var keyStdin, list bool
	setup := &cobra.Command{
		Use:   "setup",
		Short: "Write the judge config and store the API key (0600); then `aguard scan --llm` just works",
		RunE: func(_ *cobra.Command, _ []string) error {
			if list {
				return printProviders(os.Stdout)
			}
			o := llmSetupOpts{Provider: provider, Model: model, BaseURL: baseURL, KeyFile: keyFile}
			switch {
			case keyStdin:
				o.Key = os.Stdin
			case term.IsTerminal(int(os.Stdin.Fd())):
				o.Prompt = promptHiddenKey
			}
			return runLLMSetup(os.Stdout, *cfgPath, o)
		},
	}
	setup.Flags().StringVar(&provider, "provider", "", "preset name (see --list) or "+config.ProviderGeneric)
	setup.Flags().StringVar(&model, "model", "", "model name; the preset's default when empty")
	setup.Flags().StringVar(&baseURL, "base-url", "", "endpoint root (required for "+config.ProviderGeneric+", otherwise the preset's)")
	setup.Flags().StringVar(&keyFile, "key-file", "", "where to store the key (default: <config dir>/llm.key)")
	setup.Flags().BoolVar(&keyStdin, "key-stdin", false, "read the API key from stdin (first line) — for pipes; in a terminal the key is prompted for with hidden input instead, and without either an existing key file is kept")
	setup.Flags().BoolVar(&list, "list", false, "list the provider presets and exit")

	test := &cobra.Command{
		Use:   "test",
		Short: "Make one call to the configured endpoint and report whether it answered",
		RunE:  func(_ *cobra.Command, _ []string) error { return runLLMTest(os.Stdout, *cfgPath) },
	}
	status := &cobra.Command{
		Use:   "status",
		Short: "Show what the judge would use: config file, endpoint, model, where the key comes from",
		RunE:  func(_ *cobra.Command, _ []string) error { return runLLMStatus(os.Stdout, *cfgPath) },
	}
	cmd.AddCommand(setup, test, status)
	return cmd
}

type llmSetupOpts struct {
	Provider, Model, BaseURL, KeyFile string
	Key                               io.Reader              // piped key (first line); nil when not piped
	Prompt                            func() (string, error) // interactive hidden prompt; nil when stdin is not a terminal
}

// promptHiddenKey reads the key from the terminal without echo — the way `ssh-add` or `sudo`
// take a secret. Typed this way it is in no shell history, on no screen, and in no chat
// transcript, which makes the terminal the recommended route for the one step that handles
// the key. An empty line means "keep the key file I already have".
func promptHiddenKey() (string, error) {
	fmt.Fprint(os.Stderr, "API key (input hidden, Enter alone keeps the existing key file): ")
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func printProviders(w io.Writer) error {
	fmt.Fprintln(w, "Provider presets (endpoint and default model are filled in; --model overrides):")
	for _, n := range config.PresetNames() {
		p := config.Presets[n]
		fmt.Fprintf(w, "  %-10s %s  (%s, default model %s)\n", n, p.Label, p.BaseURL, p.Model)
	}
	fmt.Fprintf(w, "  %-10s any OpenAI-style endpoint: pass --base-url and --model yourself\n", config.ProviderGeneric)
	return nil
}

// runLLMSetup validates the choice, stores the key, writes the config, and says where scanned
// content will be sent. It never prints the key and never sends anything.
func runLLMSetup(w io.Writer, cfgPath string, o llmSetupOpts) error {
	if o.Provider == "" {
		return errors.New("--provider is required; `aguard llm setup --list` shows the presets")
	}
	llm := config.Default().LLM
	llm.Enabled = true
	llm.Provider = o.Provider
	llm.Model = o.Model
	switch o.Provider {
	case config.ProviderGeneric:
		if o.BaseURL == "" || o.Model == "" {
			return fmt.Errorf("provider %s needs --base-url and --model", config.ProviderGeneric)
		}
		llm.BaseURL = o.BaseURL
	default:
		p, ok := config.Presets[o.Provider]
		if !ok {
			return fmt.Errorf("unknown provider %q; want one of %s, or %s", o.Provider, strings.Join(config.PresetNames(), ", "), config.ProviderGeneric)
		}
		llm.BaseURL = p.BaseURL
		if o.BaseURL != "" {
			llm.BaseURL = o.BaseURL
		}
		if llm.Model == "" {
			llm.Model = p.Model
		}
	}

	// Refuse before anything is written: a config that would leak the key is not worth saving.
	if err := llm.CheckEndpoint(); err != nil {
		return err
	}

	keyPath := o.KeyFile
	if keyPath == "" {
		var err error
		if keyPath, err = config.DefaultKeyPath(); err != nil {
			return err
		}
	}
	var key string
	switch {
	case o.Key != nil:
		line, err := bufio.NewReader(o.Key).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		key = line
	case o.Prompt != nil:
		var err error
		if key, err = o.Prompt(); err != nil {
			return err
		}
	}
	switch {
	case strings.TrimSpace(key) != "":
		if err := config.WriteKey(keyPath, key); err != nil {
			return err
		}
		fmt.Fprintf(w, "key stored in %s (mode 0600)\n", keyPath)
	default:
		if _, err := os.Stat(keyPath); err != nil {
			return fmt.Errorf("no key at %s — run this in a terminal to be prompted for it (hidden input), or pipe it with --key-stdin", keyPath)
		}
		fmt.Fprintf(w, "key kept at %s\n", keyPath)
	}
	llm.APIKeyFile = keyPath

	if cfgPath == "" {
		var err error
		if cfgPath, err = config.DefaultPath(); err != nil {
			return err
		}
	}
	if err := config.WriteLLM(cfgPath, llm); err != nil {
		return err
	}
	fmt.Fprintf(w, "config written to %s\n", cfgPath)
	shown := redact.Endpoint(llm.BaseURL)
	fmt.Fprintf(w, "endpoint %s · model %s\n", shown, llm.Model)
	fmt.Fprintf(w, "\nWith `aguard scan --llm`, redacted excerpts of your skills, hooks and config are sent to %s for analysis. "+
		"Raw secrets are redacted first; the judge can only add findings or raise the risk — it never removes a finding or makes a result look safer.\n"+
		"Next: `aguard llm test` to confirm the endpoint answers.\n", shown)
	return nil
}

// runLLMTest resolves everything a real run would and makes one call.
func runLLMTest(w io.Writer, cfgPath string) error {
	cfg, err := config.LoadUser(cfgPath)
	if err != nil {
		return err
	}
	if !cfg.LLM.Enabled {
		return errors.New("the judge is not configured (llm.enabled is false) — run `aguard llm setup`")
	}
	if err := cfg.LLM.CheckEndpoint(); err != nil {
		return err
	}
	key, err := cfg.ResolveAPIKey()
	if err != nil {
		return err
	}
	call, _, err := cfg.LLM.Durations()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), call)
	defer cancel()
	client := judge.NewHTTP(cfg.LLM.BaseURL, key, cfg.LLM.Model, nil)
	took, err := client.Ping(ctx)
	recordJudgeUsage(os.Stderr, "llm test", version, detect.RulesVersion(), []usageBlock{{judge: llmTestUsage(cfg.LLM, client, err)}})
	if err != nil {
		return fmt.Errorf("%s did not answer for model %s: %w", redact.Endpoint(cfg.LLM.BaseURL), cfg.LLM.Model, err)
	}
	fmt.Fprintf(w, "OK · %s answered in %s via %s (%s)\n", cfg.LLM.Model, took.Round(1e6), redact.Endpoint(cfg.LLM.BaseURL), tokensInOut(client.Usage()))
	return nil
}

// llmTestUsage is the judge block `llm test` would have: one call, failed or not, and what the
// endpoint reported for it. No samples — the connectivity check is not a sampled question.
func llmTestUsage(llm config.LLMConfig, client *judge.HTTPClient, err error) *model.JudgeSummary {
	j := judgeConfigured(llm, 0)
	j.Ran, j.Calls, j.Samples = true, 1, 0
	if err != nil {
		j.Failed = 1
	}
	u := client.Usage()
	j.PromptTokens, j.CompletionTokens, j.CachedPromptTokens = u.Prompt, u.Completion, u.CachedPrompt
	j.TemperatureOmitted = client.TemperatureOmitted()
	return j
}

// runLLMStatus prints the effective configuration. The key itself never appears.
func runLLMStatus(w io.Writer, cfgPath string) error {
	shown := cfgPath
	if shown == "" {
		def, err := config.DefaultPath()
		if err != nil {
			return err
		}
		shown = def
		if _, err := os.Stat(def); err != nil {
			shown = def + " (absent — defaults)"
		}
	}
	cfg, err := config.LoadUser(cfgPath)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "config:   %s\n", shown)
	if cfgPath == "" {
		if old, ok := config.IgnoredLegacyConfigDir(); ok {
			// Both the pre-rename and the current directory exist (P-082): say which one is read.
			fmt.Fprintf(w, "note:     %s is ignored; the directory above is the one in use (the tool was renamed to BAS Guard)\n", old)
		}
	}
	fmt.Fprintf(w, "enabled:  %t\n", cfg.LLM.Enabled)
	fmt.Fprintf(w, "provider: %s\n", cfg.LLM.Provider)
	fmt.Fprintf(w, "endpoint: %s\n", redact.Endpoint(cfg.LLM.BaseURL))
	fmt.Fprintf(w, "model:    %s\n", cfg.LLM.Model)
	fmt.Fprintf(w, "key:      %s\n", cfg.KeySource())
	if !cfg.LLM.Enabled {
		fmt.Fprintln(w, "\nNot set up. `aguard llm setup --provider <name> --key-stdin` configures it in one step.")
	}
	return nil
}
