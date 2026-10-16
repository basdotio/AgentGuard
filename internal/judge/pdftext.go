// SPDX-License-Identifier: MIT
package judge

import (
	"bytes"
	"compress/zlib"
	"encoding/ascii85"
	"encoding/hex"
	"io"
	"strings"
	"unicode"
)

// pdfText returns the text layer of a PDF, or "" when it has none this reader can see. It is the
// smallest reader that answers the companion pass's question — what does the document the agent
// was told to read SAY — in the standard library and never outside the bytes it was given:
// each content stream is decoded through the filters its dictionary names (ASCII85, Flate, in
// order; nothing else), and the text-showing operators (Tj, TJ, ', ") yield their strings, one
// line per text-positioning operator. Fonts with a custom encoding (CID, subset Identity-H)
// come out as byte soup, and a result that is mostly unprintable is returned as nothing rather
// than as text, so a verdict cannot ground in noise. Image streams are skipped. Nothing is
// executed, no object graph is followed, and the output is bounded by maxFileRead like any file.
//
// Measured need: 38 of 240 SkillHarm trees put the attack in a ReportLab PDF the SKILL.md says
// to read in full; every one decodes here. A PDF this cannot read is disclosed, not ignored.
func pdfText(raw []byte) string {
	var out strings.Builder
	for pos := 0; pos < len(raw); {
		k := bytes.Index(raw[pos:], []byte("stream"))
		if k < 0 {
			break
		}
		at := pos + k
		pos = at + len("stream")
		if at >= 3 && string(raw[at-3:at]) == "end" { // endstream
			continue
		}
		start := pos
		if start < len(raw) && raw[start] == '\r' {
			start++
		}
		if start >= len(raw) || raw[start] != '\n' {
			continue
		}
		start++
		dict := dictBefore(raw, at)
		if dict == nil || bytes.Contains(dict, []byte("/Image")) {
			continue
		}
		end := bytes.Index(raw[start:], []byte("endstream"))
		if end < 0 {
			break
		}
		data := bytes.TrimRight(raw[start:start+end], "\r\n")
		pos = start + end
		content, ok := pdfDecode(dict, data)
		if !ok {
			continue
		}
		pdfShowText(content, &out)
		if out.Len() > maxFileRead {
			break
		}
	}
	text := strings.TrimSpace(out.String())
	if len(text) < minGroundedChars || !mostlyPrintable(text) {
		return ""
	}
	return text
}

// maxPDFDictBytes bounds how far back from a stream keyword the governing dictionary may begin, so
// a hostile file cannot make the search quadratic.
const maxPDFDictBytes = 4000

// dictBefore returns the dictionary that closes just before raw[at] (whitespace allowed between
// `>>` and `stream`), balanced over nested dictionaries, or nil when there is none in bounds.
func dictBefore(raw []byte, at int) []byte {
	i := at
	for i > 0 && (raw[i-1] == ' ' || raw[i-1] == '\n' || raw[i-1] == '\r' || raw[i-1] == '\t') {
		i--
	}
	if i < 2 || raw[i-1] != '>' || raw[i-2] != '>' {
		return nil
	}
	depth, lo := 0, max(0, i-maxPDFDictBytes)
	for j := i - 2; j >= lo; j-- {
		switch {
		case j+1 < len(raw) && raw[j] == '>' && raw[j+1] == '>':
			depth++
			j--
		case j+1 < len(raw) && raw[j] == '<' && raw[j+1] == '<':
			depth--
			if depth == 0 {
				return raw[j:i]
			}
		}
	}
	return nil
}

// pdfDecode applies the dictionary's filters, in the order written, and refuses any it does not
// know: a stream it cannot decode is not text, and guessing would ground a verdict in garbage.
func pdfDecode(dict, data []byte) ([]byte, bool) {
	type filter struct {
		pos  int
		name string
	}
	var filters []filter
	for _, name := range []string{"/ASCII85Decode", "/FlateDecode"} {
		for i := 0; ; {
			j := bytes.Index(dict[i:], []byte(name))
			if j < 0 {
				break
			}
			filters = append(filters, filter{i + j, name})
			i += j + len(name)
		}
	}
	for _, other := range []string{"/LZWDecode", "/DCTDecode", "/JPXDecode", "/CCITTFaxDecode", "/JBIG2Decode", "/RunLengthDecode", "/ASCIIHexDecode", "/Crypt"} {
		if bytes.Contains(dict, []byte(other)) {
			return nil, false
		}
	}
	for i := 1; i < len(filters); i++ { // by position: the dictionary's order is the decoding order
		for j := i; j > 0 && filters[j].pos < filters[j-1].pos; j-- {
			filters[j], filters[j-1] = filters[j-1], filters[j]
		}
	}
	for _, f := range filters {
		switch f.name {
		case "/ASCII85Decode":
			src := bytes.TrimSpace(data)
			src = bytes.TrimPrefix(src, []byte("<~"))
			if k := bytes.Index(src, []byte("~>")); k >= 0 {
				src = src[:k]
			}
			dst := make([]byte, len(src)*4/5+8)
			n, _, err := ascii85.Decode(dst, src, true)
			if err != nil {
				return nil, false
			}
			data = dst[:n]
		case "/FlateDecode":
			r, err := zlib.NewReader(bytes.NewReader(data))
			if err != nil {
				return nil, false
			}
			inflated, err := io.ReadAll(io.LimitReader(r, maxFileRead))
			if err != nil && len(inflated) == 0 {
				return nil, false
			}
			data = inflated
		}
	}
	return data, true
}

// pdfShowText walks a content stream and writes the strings its text operators show, a line per
// positioning operator. The tokenizer knows only what it needs: literal strings with their
// escapes and nesting, hex strings, arrays (TJ's kerned pieces), comments, and operator words.
func pdfShowText(content []byte, out *strings.Builder) {
	var pending []string
	var line strings.Builder
	flush := func() {
		if line.Len() > 0 {
			out.WriteString(strings.TrimSpace(line.String()))
			out.WriteByte('\n')
			line.Reset()
		}
	}
	show := func() {
		for _, s := range pending {
			line.WriteString(s)
		}
		pending = pending[:0]
	}
	for i := 0; i < len(content); {
		c := content[i]
		switch {
		case c == '(':
			s, next := pdfLiteral(content, i)
			pending = append(pending, s)
			i = next
		case c == '<' && i+1 < len(content) && content[i+1] != '<':
			end := bytes.IndexByte(content[i:], '>')
			if end < 0 {
				return
			}
			h := bytes.Map(func(r rune) rune {
				if unicode.IsSpace(r) {
					return -1
				}
				return r
			}, content[i+1:i+end])
			if len(h)%2 == 1 {
				h = append(h, '0')
			}
			if b, err := hex.DecodeString(string(h)); err == nil {
				pending = append(pending, latin1(b))
			}
			i += end + 1
		case c == '%':
			for i < len(content) && content[i] != '\n' && content[i] != '\r' {
				i++
			}
		case c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '\'' || c == '"' || c == '*':
			j := i
			for j < len(content) && (content[j] >= 'A' && content[j] <= 'Z' || content[j] >= 'a' && content[j] <= 'z' || content[j] == '*' || content[j] == '\'' || content[j] == '"') {
				j++
			}
			switch string(content[i:j]) {
			case "Tj", "TJ":
				show()
			case "'", "\"":
				flush()
				show()
			case "Td", "TD", "T*", "Tm", "ET":
				flush()
				pending = pending[:0]
			default:
				pending = pending[:0]
			}
			i = j
		default:
			i++
		}
	}
	flush()
}

// pdfLiteral reads a literal string starting at the '(' at content[i] and returns its text and
// the index after the closing ')'. Balanced parentheses nest; backslash escapes are the PDF set.
func pdfLiteral(content []byte, i int) (string, int) {
	var b []byte
	depth := 0
	for ; i < len(content); i++ {
		c := content[i]
		switch c {
		case '\\':
			if i+1 >= len(content) {
				return latin1(b), i + 1
			}
			i++
			switch e := content[i]; e {
			case 'n':
				b = append(b, '\n')
			case 'r':
				b = append(b, '\r')
			case 't':
				b = append(b, '\t')
			case 'b', 'f':
				b = append(b, ' ')
			case '\n', '\r':
				if e == '\r' && i+1 < len(content) && content[i+1] == '\n' {
					i++
				}
			default:
				if e >= '0' && e <= '7' {
					v, n := 0, 0
					for n < 3 && i < len(content) && content[i] >= '0' && content[i] <= '7' {
						v = v*8 + int(content[i]-'0')
						i++
						n++
					}
					i--
					b = append(b, byte(v))
				} else {
					b = append(b, e)
				}
			}
		case '(':
			if depth > 0 {
				b = append(b, c)
			}
			depth++
		case ')':
			depth--
			if depth == 0 {
				return latin1(b), i + 1
			}
			b = append(b, c)
		default:
			b = append(b, c)
		}
	}
	return latin1(b), i
}

// latin1 reads bytes as the single-byte encoding a standard-14 font uses (WinAnsi is Latin-1
// in the range that matters), so an accented letter becomes itself rather than an invalid byte.
func latin1(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		sb.WriteRune(rune(c))
	}
	return sb.String()
}

// mostlyPrintable is the noise gate: text from a font with a private encoding is bytes that look
// like nothing, and a reader must not be shown it as the document's words.
func mostlyPrintable(s string) bool {
	total, ok := 0, 0
	for _, r := range s {
		total++
		if unicode.IsPrint(r) || r == '\n' || r == '\t' {
			ok++
		}
	}
	return total > 0 && ok*10 >= total*9
}
