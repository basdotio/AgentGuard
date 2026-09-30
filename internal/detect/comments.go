// SPDX-License-Identifier: MIT
package detect

import (
	"path/filepath"
	"strings"
)

// A1: comment-awareness. A rule that fires on a line whose entire code content is a
// COMMENT is a mention, not a real construct — dropped for droppable dimensions. This
// is intentionally conservative: only whole-line comments are dropped (a trailing
// `rm -rf / # note` keeps its finding), the scan is STRING-AWARE so a `/*` inside a
// string never opens a block comment, and any uncertainty resolves to "code" (kept).
// Result: pure precision, ZERO false-negative risk. Obfuscation (dim 6) is never
// dropped — an encoded payload can legitimately live in comment-adjacent text.

type commentLang int

const (
	langNone   commentLang = iota // no comment stripping (unknown / no line-comment syntax)
	langHash                      // # line comments (sh, py, yaml, toml, rb, pl)
	langCStyle                    // // line + /* */ block (js, ts, go, c-like)
)

// commentDroppableDim reports whether a comment-only match for this dimension may be
// dropped. Everything except OBF (6): an injection/exec/backdoor/etc. phrase sitting in
// a comment is never executed, but obfuscation stays (user-requested guard).
func commentDroppableDim(dim int) bool { return dim != 6 && dim != 0 }

func langForFile(path string) commentLang {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".sh", ".bash", ".zsh", ".py", ".rb", ".pl", ".yaml", ".yml", ".toml", ".env":
		return langHash
	case ".js", ".mjs", ".cjs", ".ts", ".go", ".c", ".cc", ".cpp", ".h", ".java", ".rs":
		return langCStyle
	}
	return langNone
}

// commentOnlyLines returns the set of 1-based line numbers whose non-whitespace content
// is entirely comment. Lines with any real code (including a bare string literal) are
// NOT included. langNone → empty (nothing dropped).
func commentOnlyLines(lang commentLang, content string) map[int]bool {
	if lang == langNone {
		return nil
	}
	out := map[int]bool{}
	inBlock := false // C-style /* */ spanning lines
	for i, raw := range strings.Split(content, "\n") {
		line := strings.TrimRight(raw, "\r")
		codeSeen, blk := scanLineForCode(lang, line, inBlock)
		inBlock = blk
		if !codeSeen && strings.TrimSpace(line) != "" {
			out[i+1] = true
		}
	}
	return out
}

// scanLineForCode reports whether the line contains any CODE character (outside comments)
// and the block-comment state carried to the next line. String contents count as code.
func scanLineForCode(lang commentLang, line string, inBlock bool) (codeSeen, blockOut bool) {
	r := []rune(line)
	var quote rune // 0 = not in string; else the open quote char
	for i := 0; i < len(r); i++ {
		c := r[i]
		switch {
		case inBlock:
			if lang == langCStyle && c == '*' && i+1 < len(r) && r[i+1] == '/' {
				inBlock = false
				i++
			}
			continue
		case quote != 0:
			codeSeen = true // string body is code
			if c == '\\' {
				i++ // skip escaped char
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		// normal state
		switch {
		case c == ' ' || c == '\t':
			continue
		case lang == langHash && c == '#':
			return codeSeen, false // rest of line is comment
		case lang == langCStyle && c == '/' && i+1 < len(r) && r[i+1] == '/':
			return codeSeen, false // // to EOL
		case lang == langCStyle && c == '/' && i+1 < len(r) && r[i+1] == '*':
			inBlock = true
			i++
		case c == '"' || c == '\'' || (lang == langCStyle && c == '`'):
			quote = c
			codeSeen = true
		default:
			codeSeen = true
		}
	}
	return codeSeen, inBlock
}

// CommentOnlyLines is the exported form of commentOnlyLines keyed by file name: the 1-based
// lines of content that are entirely comment for that file's language, or empty when the
// language is unknown. The LLM judge uses it to drop comments from excerpts (judge/excerpt.go):
// a justification paragraph written FOR the model must not spend the model's budget, and this
// is the same classifier the A1 comment filter uses, so the two cannot disagree about what a
// comment is.
func CommentOnlyLines(path, content string) map[int]bool {
	return commentOnlyLines(langForFile(path), content)
}
