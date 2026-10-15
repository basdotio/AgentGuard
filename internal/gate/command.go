// SPDX-License-Identifier: MIT
package gate

import "github.com/basdotio/AgentGuard/internal/cmdline"

// maxCommandArgLen bounds one quoted argument of a command printed for copying (cmdline.MaxArgLen).
const maxCommandArgLen = cmdline.MaxArgLen

// CommandArg renders path as one word of a command an operator is meant to copy and paste: the full
// path, quoted for a POSIX shell, or a placeholder past maxCommandArgLen (P-030). The implementation
// lives in internal/cmdline so the report's next steps quote a path the same way (P-058).
func CommandArg(path string) string { return cmdline.Arg(path) }

// shellQuote quotes s as exactly one word for a POSIX shell (cmdline.Quote).
func shellQuote(s string) string { return cmdline.Quote(s) }
