// SPDX-License-Identifier: MIT
package redact

import (
	"strings"
	"testing"
)

// TestQuotedValues pins P-043: a value a flag, `-u user:` or a credential key announces, written in quotes
// the way a shell user writes a password with a space or a symbol in it, is forgotten between its quotes.
// The patterns could not start a value at a quote, so after a flag or `-u` the value went out whole — to
// the snippet, the judge and the content hash — and after a key it kept everything past its first bare
// word (`"<REDACTED> horse"`). The announcer, the quotes and the user stay; so does a carrier word inside
// the quotes. An unterminated quote runs to the end of its line, never past it. The values are made up.
func TestQuotedValues(t *testing.T) {
	values := []string{"correct horse", "hunter2xyz", "P@ss!w0rd", "pass word #1"}
	announcers := []string{
		"tool --password ", "tool --token ", "tool --api-key ", "tool --key ", "tool --db-pass ",
		"tool --password=", "tool --key=", "TOOL --PASSWORD ",
		"API_TOKEN=", `"password": `, "password: ", "export DB_PASSWORD=", "client_secret = ",
	}
	for _, a := range announcers {
		for _, v := range values {
			for _, q := range []string{`"`, `'`} {
				in := a + q + v + q + " --verbose"
				want := a + q + marker + q + " --verbose"
				checkQuoted(t, in, want, v)
			}
			// Unterminated: to the end of the line, and the next line is untouched.
			checkQuoted(t, a+`"`+v+"\nnext line", a+`"`+marker+"\nnext line", v)
		}
	}
	for _, c := range []struct{ in, want, secret string }{
		{`tool --password "ab\"cd ef" -v`, `tool --password "<REDACTED>" -v`, `cd ef`}, // an escaped quote does not end it
		{`curl -u "admin:correct horse" https://api.example`, `curl -u "admin:<REDACTED>" https://api.example`, "horse"},
		{`curl --user='admin:P@ss!w0rd' x`, `curl --user='admin:<REDACTED>' x`, "w0rd"},
		{`curl -u 'admin:pass word'`, `curl -u 'admin:<REDACTED>'`, "word"},
		{`"Authorization": "Bearer correct horse"`, `"Authorization": "Bearer <REDACTED>"`, "horse"},
		{`Authorization: 'Basic YWxhZGRpbjpvcGVu c2VzYW1l'`, `Authorization: 'Basic <REDACTED>'`, "c2VzYW1l"},
		{"tool --password\n\"correct horse\"", "tool --password\n\"<REDACTED>\"", "horse"}, // a named flag reads across a line
		{`Bash(mytool --token 'correct horse' *)`, `Bash(mytool --token '<REDACTED>' *)`, "horse"},
	} {
		checkQuoted(t, c.in, c.want, c.secret)
	}
}

func checkQuoted(t *testing.T, in, want, secret string) {
	t.Helper()
	for name, f := range map[string]func(string) string{"Secrets": Secrets, "Credentials": Credentials} {
		got := f(in)
		if got != want {
			t.Errorf("%s(%q)\n  = %q\n want %q", name, in, got, want)
		}
		if strings.Contains(got, secret) {
			t.Errorf("%s(%q) = %q: a fragment of the value survived", name, in, got)
		}
	}
	if got := Secrets(in); Secrets(got) != got {
		t.Errorf("Secrets(%q) is not a fixed point: %q -> %q", in, got, Secrets(got))
	}
}

// TestQuotedValues_Reverse is the reverse assertion of P-043, each row a shape measured on the corpus and a
// real ~/.claude: Secrets returns these as it did before. A quoted body holding `$` or a backtick is a
// reference, not a secret — the name is the evidence; a quote followed by a space or `, ; ) ] } + .` is the
// far side of a string being built; a body with a space or an invisible character at either end is not a
// value either (two INJ-004 test lines quote nothing but zero-width characters, and that is their
// evidence). The floors stay: four characters and no literal after a key; a whitespace-only key and a
// flag no pattern names get no quoted form at all; a widened flag's exemption (P-040) reads the body.
func TestQuotedValues_Reverse(t *testing.T) {
	for _, s := range []string{
		// expansions
		`tool --password "$PW"`, `tool --password "${PW}"`, `tool --password "$(op read op://v/pw)"`,
		"tool --password \"`cat ~/.pw`\"", `tool --token "Bearer $TOKEN"`, `curl -u "$U:$P" https://x.example`,
		`"Authorization": "Bearer $TOKEN"`, `headers = {'Authorization': 'Bearer $token'}`, `API_KEY="${API_KEY:-}"`,
		`export DB_PASSWORD='$(cat pw)'`,
		// a quote that closes a string
		`args = ["--password=" + pw]`, `print("token:", t)`, `headers = {'Authorization': 'Bearer ' + token}`,
		`log("password:", user.password);`, `date -u '+%Y-%m-%dT%H:%M:%SZ'`, `url = "?token=" + tok + "&x=1"`,
		// edges
		`tool --password " lead"`, `tool --password "trail "`, "tool --password \"\u200bx\"",
		"process.env.API_TOKEN = '\u200b\ufeff\u00a0\u200b';", `tool --password ""`,
		// floors and literals after a key
		`password: ""`, `token: "yes"`, `password: "pw1"`, `"auth": "true"`, `"token": "Bearer"`,
		// no quoted form
		`the token "abcdefghijklmnop"`, `tool --author "Ada Lovelace"`, `tool --message "pass the token"`,
		// P-040's exemption and line rule, asked of the body
		`tool --private-key "~/my keys/id.pem"`, `tool --key "/bucket/a b"`, `tool --credentials "https://c.example/x"`,
		"tool --key\n\"correct horse\"",
		// JSON-escaped quotes in raw JSON text are not read (out of scope, P-043)
		`"cmd": "API_TOKEN=\"correct horse\" run"`,
	} {
		if got := Secrets(s); got != s {
			t.Errorf("Secrets(%q) = %q, want it unchanged", s, got)
		}
	}
}
