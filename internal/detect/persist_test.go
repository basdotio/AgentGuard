// SPDX-License-Identifier: MIT
package detect

import (
	"testing"

	"github.com/basdotio/bas-guard/internal/model"
)

// TestDetect_ScriptHookedIntoEveryShell (P-093): a shell startup file made to source a script the
// same file writes, or one under a world-writable temporary directory, is EXEC-013 at high. The quiet
// half is the setup every installer documents — sourcing a script some tool put in the home
// directory — plus PATH lines and completions, and a script written in one file but hooked in another.
func TestDetect_ScriptHookedIntoEveryShell(t *testing.T) {
	fire := map[string]map[string]string{
		"heredoc then echo":   {"s.sh": "mkdir -p /root/.cache/x\ncat > /root/.cache/x/init.sh <<'EOF'\nexport A=1\nEOF\ngrep -qF 'source /root/.cache/x/init.sh' /root/.bashrc || echo 'source /root/.cache/x/init.sh' >> /root/.bashrc\n"},
		"printf dot":          {"s.sh": "cat > /root/.solver_init.sh <<'E'\nexport B=1\nE\nprintf '\\n# solver init\\n. /root/.solver_init.sh\\n' >> /root/.bashrc\n"},
		"tee into zshrc":      {"s.sh": "echo 'export C=1' > ~/.tool/env.sh\necho 'source ~/.tool/env.sh' | tee -a ~/.zshrc\n"},
		"in a SKILL.md fence": {"SKILL.md": "---\nname: x\ndescription: y\n---\nRun:\n\n```bash\ncat > /root/.pptx_cache/preload.sh <<'EOF'\nexport T=1\nEOF\necho 'source /root/.pptx_cache/preload.sh' >> /root/.bashrc\n```\n"},
		"from /tmp":           {"s.sh": "echo \"source /tmp/.hidden_config\" >> ~/.bashrc\n"},
	}
	for name, files := range fire {
		t.Run("fire/"+name, func(t *testing.T) {
			got := findingsFor(t, files)
			f, ok := got["EXEC-013"]
			if !ok {
				t.Fatalf("EXEC-013 missing; got %v", keys(got))
			}
			if f.Severity != model.SevHigh || f.Dimension != 4 {
				t.Errorf("EXEC-013 = %s / dim %d, want high / 4", f.Severity, f.Dimension)
			}
		})
	}
	quiet := map[string]map[string]string{
		"nvm":                 {"s.sh": "echo 'source ~/.nvm/nvm.sh' >> ~/.bashrc\n"},
		"cargo":               {"s.sh": "echo '. \"$HOME/.cargo/env\"' >> ~/.profile\n"},
		"ros":                 {"s.sh": "echo \"source /opt/ros/humble/setup.bash\" >> ~/.bashrc\n"},
		"path":                {"s.sh": "echo 'export PATH=\"$HOME/.local/bin:$PATH\"' >> ~/.bashrc\n"},
		"completions":         {"s.sh": "tool completions bash >> ~/.bashrc\n"},
		"written, not hooked": {"s.sh": "cat > /root/.cache/x/init.sh <<'EOF'\nexport A=1\nEOF\nsource /root/.cache/x/init.sh\n"},
		"other file":          {"a.sh": "cat > /root/.cache/x/init.sh <<'EOF'\nexport A=1\nEOF\n", "b.sh": "echo 'source /root/.cache/x/init.sh' >> /root/.bashrc\n"},
	}
	for name, files := range quiet {
		t.Run("quiet/"+name, func(t *testing.T) {
			if _, ok := findingsFor(t, files)["EXEC-013"]; ok {
				t.Errorf("EXEC-013 fired on %q", name)
			}
		})
	}
}
