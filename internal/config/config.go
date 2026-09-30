// SPDX-License-Identifier: MIT
// Package config loads AgentGuard's optional config (spec §11). The LLM intent judge
// is OFF by default. The API key comes from an env var or from a
// key file the user owns (mode 0600, see ResolveAPIKey) — the file exists because the
// judge's only realistic endpoints are hosted ones that all need a key, and "set an
// environment variable" was the step non-technical users could not take; the shell
// profile they would have pasted it into is a plaintext file too, without the mode
// check. Missing/invalid config degrades to static-only.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/basdotio/agent-guard/internal/safeio"
	"gopkg.in/yaml.v3"
)

// Config is the on-disk configuration. All fields optional; zero value = static-only.
type Config struct {
	LLM  LLMConfig  `yaml:"llm"`
	Gate GateConfig `yaml:"gate"`
}

// GateConfig configures the load-time gate (internal/gate).
//
// Both knobs are about NOISE, not about capability: the gate reads the same deterministic
// findings `check` does, and neither setting can make it consult a model or reach the
// network. FailOn decides how much has to be wrong before an operator is interrupted, and
// Action decides whether that interruption is a question or a refusal.
type GateConfig struct {
	// FailOn is the severity at or above which a load stops for review. It defaults to the
	// same "high" that `check --fail-on` defaults to, deliberately: a prompt that means one
	// thing in CI and another at load time is a prompt nobody can reason about.
	FailOn string `yaml:"fail_on"`
	// Action is "ask" (default) or "deny". Ask hands the decision to the operator, which is
	// the right default for a static verdict that cannot prove malice. Deny is for machines
	// nobody is sitting at.
	Action string `yaml:"action"`
}

// LLMConfig configures the optional isolated intent judge (spec §5.2/§16.4).
//
// The call-budget knobs exist because a judge run is the only part of AgentGuard that costs
// wall-clock time and (on a paid endpoint) money. They are a COURTESY SELF-LIMIT: this is a
// client-side file the user can edit, so it protects the user's own time and bill — it is
// not, and must never be sold as, a security or cost control (spec §11).
type LLMConfig struct {
	Enabled  bool   `yaml:"enabled"`  // default false — must be true AND `scan --llm`
	Provider string `yaml:"provider"` // "openai_compatible", or a preset name (see Presets)
	BaseURL  string `yaml:"base_url"` // endpoint root; a preset fills it in when left at the default
	Model    string `yaml:"model"`    // a preset fills in its default when empty
	// The key is looked up in this order: the env var named by APIKeyEnv, then the file at
	// APIKeyFile. Never the key itself in this struct — a config file is shared, quoted and
	// committed far more casually than a 0600 key file.
	APIKeyEnv  string `yaml:"api_key_env"`  // env var NAME holding the key
	APIKeyFile string `yaml:"api_key_file"` // path (~ allowed) of a file holding the key; must be 0600

	Concurrency  int    `yaml:"concurrency"`   // in-flight calls; results still merge in a fixed order
	Timeout      string `yaml:"timeout"`       // PER CALL, e.g. "60s" — one slow call can't eat the run
	TotalTimeout string `yaml:"total_timeout"` // whole judge phase, e.g. "10m" — a backstop, not the budget
	MaxCalls     int    `yaml:"max_calls"`     // 0 = unlimited; over-budget calls are skipped and REPORTED
	MaxRetries   int    `yaml:"max_retries"`   // retries per call on 429/5xx (a retry is not a new call)
	// Samples asks each question N times and requires a MAJORITY before a finding may weigh on
	// the effective score. 1 (default) asks once. N costs N times as much, so it is opt-in —
	// and it only means anything with variance, so sampling raises the temperature.
	Samples int `yaml:"samples"`
	// Authority decides whether this endpoint's opinion may have CONSEQUENCES: "advisory"
	// (default) means the judge can only inform, "escalate" additionally permits the
	// --fail-on-llm gate. It does not change what the effective score says — that number must
	// stay readable before you decide, or there is nothing to base the decision on.
	//
	// The declaration is the user's because a self-hosted endpoint is opaque to us: `model` is
	// free text that can say anything, so inferring capability from it would be both unreliable
	// and forgeable. A hosted endpoint would assert its own tier server-side instead — a client
	// must never be able to self-certify.
	Authority string `yaml:"authority"`
}

// Default returns the safe default: LLM disabled, local endpoint.
//
// Defaults are tuned for a REMOTE endpoint (network latency, rate limits) because that is
// what a self-hosted key points at in practice; a local model is simply faster and never
// hits the retry path. MaxCalls stays 0 — truncating coverage by default would contradict
// "no gap is ever silent", and `--llm` is an explicit per-run opt-in, so an unattended bill
// is not reachable by accident.
func Default() Config {
	return Config{Gate: GateConfig{
		FailOn: "high",
		Action: "ask",
	}, LLM: LLMConfig{
		Enabled:      false,
		Provider:     "openai_compatible",
		BaseURL:      "http://localhost:11434/v1",
		APIKeyEnv:    "AGUARD_LLM_KEY",
		Concurrency:  4,
		Timeout:      "60s",
		TotalTimeout: "10m",
		MaxCalls:     0,
		MaxRetries:   2,
		Samples:      1,
		Authority:    AuthorityAdvisory,
	}}
}

// Actions for gate.action. Mirrors the hook protocol's own vocabulary so the config value
// and the decision the gate returns are the same word.
const (
	GateAsk  = "ask"
	GateDeny = "deny"
)

// Authority levels for llm.authority.
const (
	AuthorityAdvisory = "advisory" // the judge informs; no gate may act on it
	AuthorityEscalate = "escalate" // --fail-on-llm is permitted
)

// MayEscalate reports whether this configuration allows the effective score to gate anything.
func (c LLMConfig) MayEscalate() bool { return c.Authority == AuthorityEscalate }

// Durations parses the two duration fields. They are strings in YAML ("60s", "10m") and an
// unparsable value is a config ERROR, not a silent fallback — a typo that quietly restores a
// default is how a "why is this still timing out?" afternoon starts.
func (c LLMConfig) Durations() (call, total time.Duration, err error) {
	if call, err = parseDur("llm.timeout", c.Timeout, 60*time.Second); err != nil {
		return 0, 0, err
	}
	if total, err = parseDur("llm.total_timeout", c.TotalTimeout, 10*time.Minute); err != nil {
		return 0, 0, err
	}
	return call, total, nil
}

func parseDur(field, v string, fallback time.Duration) (time.Duration, error) {
	if strings.TrimSpace(v) == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("config %s %q: %w", field, v, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("config %s %q: must be positive", field, v)
	}
	return d, nil
}

// Load reads config from path. A missing file is not an error — it returns Default()
// so the tool stays usable offline. A present-but-corrupt file IS an error.
func Load(path string) (Config, error) {
	if path == "" {
		return Default(), nil
	}
	b, err := safeio.ReadFile(path, 1<<20)
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}
	// Unmarshal ONTO the defaults: yaml.v3 only overwrites keys that are present, so an
	// omitted knob keeps its default while an explicit `max_retries: 0` still means zero.
	cfg := Default()
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	// Validate at LOAD time so a typo fails before any scanning work is done.
	if _, _, err := cfg.LLM.Durations(); err != nil {
		return Config{}, err
	}
	if err := cfg.LLM.applyPreset(); err != nil {
		return Config{}, err
	}
	switch cfg.LLM.Authority {
	case AuthorityAdvisory, AuthorityEscalate:
	default:
		// Not defaulted silently: an unrecognised value here would otherwise decide, without
		// saying so, whether a gate can fire.
		return Config{}, fmt.Errorf("config llm.authority %q: want %q or %q",
			cfg.LLM.Authority, AuthorityAdvisory, AuthorityEscalate)
	}
	// Same treatment for the gate: an unrecognised value here decides whether a load is
	// interrupted or refused, which is not a thing to resolve by silently picking one.
	switch cfg.Gate.Action {
	case GateAsk, GateDeny:
	default:
		return Config{}, fmt.Errorf("config gate.action %q: want %q or %q", cfg.Gate.Action, GateAsk, GateDeny)
	}
	switch cfg.Gate.FailOn {
	case "low", "medium", "high", "critical":
	default:
		return Config{}, fmt.Errorf("config gate.fail_on %q: want low|medium|high|critical", cfg.Gate.FailOn)
	}
	return cfg, nil
}

// ResolveAPIKey returns the key: the env var named by api_key_env when it is set and non-empty,
// else the contents of api_key_file. An empty result with a nil error means "no key configured",
// which a local endpoint is allowed to run with; a hosted one will get a 401 and say so.
//
// The file is refused unless only its owner can read it. The refusal is an error, not a
// silent fallback: a key file that is world-readable is a leak in progress, and running the
// judge anyway would tell the user everything is fine. The message says the exact chmod to run.
func (c Config) ResolveAPIKey() (string, error) {
	if c.LLM.APIKeyEnv != "" {
		if v := os.Getenv(c.LLM.APIKeyEnv); v != "" {
			return v, nil
		}
	}
	if c.LLM.APIKeyFile == "" {
		return "", nil
	}
	path, err := ExpandHome(c.LLM.APIKeyFile)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("llm.api_key_file %s does not exist — run `aguard llm setup`", path)
		}
		return "", err
	}
	if fi.Mode()&0o077 != 0 {
		return "", fmt.Errorf("llm.api_key_file %s is readable by others (mode %04o); refusing to use it — run: chmod 600 %s", path, fi.Mode().Perm(), path)
	}
	b, err := safeio.ReadFile(path, 64<<10)
	if err != nil {
		return "", err
	}
	key := strings.TrimSpace(string(b))
	if key == "" {
		return "", fmt.Errorf("llm.api_key_file %s is empty — run `aguard llm setup`", path)
	}
	return key, nil
}

// KeySource says where the key would come from, for `aguard llm status` — never the key itself.
func (c Config) KeySource() string {
	if c.LLM.APIKeyEnv != "" && os.Getenv(c.LLM.APIKeyEnv) != "" {
		return "env var " + c.LLM.APIKeyEnv
	}
	if c.LLM.APIKeyFile != "" {
		return "file " + c.LLM.APIKeyFile
	}
	if c.LLM.APIKeyEnv != "" {
		return "env var " + c.LLM.APIKeyEnv + " (unset)"
	}
	return "none"
}

// JudgeReady reports whether the LLM judge should run: enabled + reachable config.
// A cloud endpoint additionally needs a key; a local endpoint may not.
func (c Config) JudgeReady() bool {
	return c.LLM.Enabled && c.LLM.Provider != "" && c.LLM.BaseURL != ""
}

// ---- presets, default paths, writing ------------------------------------------------------

// Preset is a hosted OpenAI-compatible endpoint the user can name instead of spelling out
// base_url and model. Model is a DEFAULT, not a recommendation: vendors rename models, so the
// user's own `model:` always wins and the table is the only place a name lives.
type Preset struct {
	BaseURL string
	Model   string
	Label   string // human name for `aguard llm setup --list`
}

// ProviderGeneric is the escape hatch: any OpenAI-style endpoint, base_url and model spelled out.
const ProviderGeneric = "openai_compatible"

// Presets are hosted endpoints known to speak the OpenAI chat-completions shape the judge
// uses. Adding one is adding a row; nothing else in the tool knows a vendor's name.
var Presets = map[string]Preset{
	"deepseek": {BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-chat", Label: "DeepSeek"},
	"openai":   {BaseURL: "https://api.openai.com/v1", Model: "gpt-4o-mini", Label: "OpenAI"},
	"qwen":     {BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-plus", Label: "Alibaba Qwen (DashScope)"},
}

// PresetNames returns the preset names in a fixed order, for help text and the setup flow.
func PresetNames() []string {
	names := make([]string, 0, len(Presets))
	for n := range Presets {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// applyPreset resolves a named provider into base_url/model, leaving anything the user set
// explicitly alone, and rejects a provider nobody knows: a misspelled preset that silently fell
// back to localhost:11434 would fail with a connection error that points at the wrong problem.
func (c *LLMConfig) applyPreset() error {
	if c.Provider == "" || c.Provider == ProviderGeneric {
		return nil
	}
	p, ok := Presets[c.Provider]
	if !ok {
		return fmt.Errorf("config llm.provider %q: want %s, or one of %s", c.Provider, ProviderGeneric, strings.Join(PresetNames(), ", "))
	}
	if c.BaseURL == "" || c.BaseURL == Default().LLM.BaseURL {
		c.BaseURL = p.BaseURL
	}
	if c.Model == "" {
		c.Model = p.Model
	}
	return nil
}

// ConfigDir is where the user-level config and key live: $XDG_CONFIG_HOME/aguard, else
// ~/.config/aguard. Deliberately OUTSIDE the scan root — a config file inside ~/.claude would
// be something the scanner reads as an unowned loose file and disclose on every run.
func ConfigDir() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "aguard"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "aguard"), nil
}

// DefaultPath is the config file `--config` falls back to; DefaultKeyPath is where
// `aguard llm setup` puts the key.
func DefaultPath() (string, error) {
	d, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.yaml"), nil
}

func DefaultKeyPath() (string, error) {
	d, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "llm.key"), nil
}

// LoadUser is Load with the default location filled in: an explicit path is used as given, an
// empty one means "the user-level config if there is one, else defaults". CLI entry points call
// this; Load stays path-in/config-out for tests and for tooling that knows exactly which file.
func LoadUser(path string) (Config, error) {
	if path != "" {
		return Load(path)
	}
	def, err := DefaultPath()
	if err != nil {
		return Default(), nil
	}
	return Load(def)
}

// ExpandHome turns a leading "~/" into the user's home directory.
func ExpandHome(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, p[1:]), nil
	}
	return p, nil
}

// WriteLLM writes the llm section into the config file at path, creating the file (and its
// directory, 0700) when absent and leaving every other top-level key of an existing file
// untouched. Only the fields that carry a decision are written; knobs at their default are not,
// so the file a user opens is the three lines they chose, not a wall of defaults.
func WriteLLM(path string, llm LLMConfig) error {
	doc := map[string]any{}
	if b, err := safeio.ReadFile(path, 1<<20); err == nil {
		if err := yaml.Unmarshal(b, &doc); err != nil {
			return fmt.Errorf("existing config %s is not valid YAML, not overwriting it: %w", path, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	sec := map[string]any{"enabled": llm.Enabled, "provider": llm.Provider}
	def := Default().LLM
	if llm.BaseURL != "" && (llm.Provider == ProviderGeneric || llm.BaseURL != Presets[llm.Provider].BaseURL) {
		sec["base_url"] = llm.BaseURL
	}
	if llm.Model != "" && (llm.Provider == ProviderGeneric || llm.Model != Presets[llm.Provider].Model) {
		sec["model"] = llm.Model
	}
	if llm.APIKeyFile != "" {
		sec["api_key_file"] = llm.APIKeyFile
	}
	if llm.APIKeyEnv != "" && llm.APIKeyEnv != def.APIKeyEnv {
		sec["api_key_env"] = llm.APIKeyEnv
	}
	doc["llm"] = sec
	out, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o600)
}

// WriteKey stores an API key at path, 0600, directory 0700. It refuses an empty key: a blank
// file would pass every existence check and fail only at the first call, far from its cause.
func WriteKey(path, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("refusing to write an empty API key")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(key+"\n"), 0o600)
}

// CheckEndpoint refuses a base_url that would send the API key in cleartext: anything but https,
// unless the host is this machine (localhost / 127.0.0.1 / ::1), where a local model listens on
// plain http and nothing crosses a wire. Setup, `llm test` and the judge all call it, so the
// refusal happens before the first request, with the reason in hand.
func (c LLMConfig) CheckEndpoint() error {
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("llm.base_url %q is not a valid URL", c.BaseURL)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "localhost" || host == "::1" {
			return nil
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil
		}
		return fmt.Errorf("llm.base_url %s is plain http to a remote host: the API key and the scanned excerpts would cross the network unencrypted; use https", c.BaseURL)
	default:
		return fmt.Errorf("llm.base_url %q: scheme must be https (or http to this machine)", c.BaseURL)
	}
}
