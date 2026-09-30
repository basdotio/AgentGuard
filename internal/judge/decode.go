// SPDX-License-Identifier: MIT
package judge

import (
	"encoding/base64"
	"encoding/hex"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/basdotio/AgentGuard/internal/collect"
	"github.com/basdotio/AgentGuard/internal/detect"
)

// Deobfuscation caps: decoding is DECODING, never execution (§16.1). Bounded so a huge file
// of blobs can't blow up the prompt.
const (
	maxDecodedPayloads = 8
	maxDecodedBytes    = 800     // per decoded payload sent to the model
	maxDecodeScanBytes = 8 << 20 // total raw bytes scanned for blobs across the whole skill
)

var (
	base64BlobRE = regexp.MustCompile(`[A-Za-z0-9+/]{32,}={0,2}`)
	base64URLRE  = regexp.MustCompile(`[A-Za-z0-9_-]{32,}`)
	hexBlobRE    = regexp.MustCompile(`(?:[0-9a-fA-F]{2}){24,}`)
)

// decodedPayloads walks a skill dir over RAW bytes (redaction would hide the blobs), finds
// base64/hex-looking runs, decodes them WITHOUT executing anything, keeps those that decode
// to mostly-printable text, and returns each decoded string REDACTED, with the file and line
// the blob sat on. This surfaces "what an obfuscated payload actually says" — a stated static
// blind spot. Bounded and dedup'd.
func decodedPayloads(dir string) []sourceUnit {
	seen := map[string]bool{}
	var out []sourceUnit
	scanned := 0 // total raw bytes read across the skill — a total-work bound so a hostile
	// many-file skill (thousands of blob-free files) can't force unbounded reads/regex passes.
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if collect.ExcludedFromScan(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if !behaviorExts[strings.ToLower(filepath.Ext(p))] {
			return nil
		}
		if len(out) >= maxDecodedPayloads || scanned >= maxDecodeScanBytes {
			return filepath.SkipAll
		}
		raw := readAtMost(p, maxFileRead)
		if raw == nil {
			return nil
		}
		scanned += len(raw)
		rel, _ := filepath.Rel(dir, p)
		for _, dec := range extractDecodable(string(raw)) {
			if len(out) >= maxDecodedPayloads {
				break
			}
			if seen[dec.text] {
				continue
			}
			seen[dec.text] = true
			red := detect.Redact(dec.text)
			if len(red) > maxDecodedBytes {
				red = red[:maxDecodedBytes]
			}
			// collapsed: the decoded text never existed in the file as lines — it was one
			// encoded run on one line, so that is the only honest position to cite.
			out = append(out, sourceUnit{
				file:      detect.Redact(rel),
				text:      red,
				firstLine: 1 + strings.Count(string(raw[:dec.offset]), "\n"),
				collapsed: true,
			})
		}
		return nil
	})
	return out
}

// decoded is one decoded blob plus the byte offset of the ENCODED run it came from, which is
// what lets a finding cite the line the blob actually sits on.
type decoded struct {
	text   string
	offset int
}

// extractDecodable finds encoded runs in s and returns the printable decodings. Order:
// base64 (std then url) then hex; a run that decodes to mostly-binary is dropped (it was a
// real opaque token/secret, not a hidden text payload).
func extractDecodable(s string) []decoded {
	var res []decoded
	full := func() bool { return len(res) >= maxDecodedPayloads } // internal cap (memory-DoS guard)
	tryAdd := func(b []byte, at int) {
		if printableText(b) {
			res = append(res, decoded{text: string(b), offset: at})
		}
	}
	match := func(re *regexp.Regexp, dec func(string) ([]byte, error)) {
		for _, loc := range re.FindAllStringIndex(s, -1) {
			if full() {
				return
			}
			if b, err := dec(s[loc[0]:loc[1]]); err == nil {
				tryAdd(b, loc[0])
			}
		}
	}
	match(base64BlobRE, func(m string) ([]byte, error) {
		if b, err := base64.StdEncoding.DecodeString(m); err == nil {
			return b, nil
		}
		return base64.RawStdEncoding.DecodeString(m)
	})
	match(base64URLRE, base64.RawURLEncoding.DecodeString)
	match(hexBlobRE, hex.DecodeString)
	return res
}

// printableText reports whether a decoded blob is mostly human-readable text (so it's a
// hidden payload worth explaining) rather than binary (a real secret/opaque token).
func printableText(b []byte) bool {
	if len(b) < 6 {
		return false
	}
	printable := 0
	for _, c := range string(b) {
		if c == unicode.ReplacementChar {
			return false // invalid UTF-8 → binary
		}
		if c == '\n' || c == '\t' || c == '\r' || (c >= 0x20 && c < 0x7f) || unicode.IsPrint(c) {
			printable++
		}
	}
	return float64(printable)/float64(len([]rune(string(b)))) >= 0.85
}
