// SPDX-License-Identifier: MIT
package detect

import "testing"

// TestDetect_AParserIsNotAClient (P-099): `urllib.parse` and a scheme with no host are not the network
// leg of the exfiltration chain; the sending half of urllib and a URL with a host — spelled out or
// templated — still are.
func TestDetect_AParserIsNotAClient(t *testing.T) {
	const key = "import os\nKEY = os.environ[\"ACME_API_KEY\"]\n"
	chain := []struct{ name, body string }{
		{"urllib.request", key + "import urllib.request\nurllib.request.urlopen(req)\n"},
		{"from urllib import request", key + "from urllib import request\nrequest.urlopen(req)\n"},
		{"urllib3", key + "import urllib3\nhttp = urllib3.PoolManager()\n"},
		{"a templated host", key + "import requests\nurl = f\"https://{instance}/api\"\n"},
		{"a spelled-out host", key + "import requests\nURL = \"https://collector.example/c\"\n"},
	}
	for _, tc := range chain {
		t.Run("chain/"+tc.name, func(t *testing.T) {
			if _, ok := findingsFor(t, map[string]string{"s.py": tc.body})["EXFIL-001"]; !ok {
				t.Errorf("EXFIL-001 missing")
			}
		})
	}
	quiet := []struct{ name, body string }{
		{"urllib.parse", key + "from urllib.parse import quote, urlsplit\nq = quote(KEY)\n"},
		{"a scheme with no host", key + "import requests\nif not u.startswith(\"https://\"):\n    raise ValueError(\"URL must start with https://\")\n"},
	}
	for _, tc := range quiet {
		t.Run("quiet/"+tc.name, func(t *testing.T) {
			if _, ok := findingsFor(t, map[string]string{"s.py": tc.body})["EXFIL-001"]; ok {
				t.Errorf("EXFIL-001 fired")
			}
		})
	}
}
