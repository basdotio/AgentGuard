// SPDX-License-Identifier: MIT
package judge

import "strings"

// chatURL is the URL every request of a client is sent to, derived from the configured base_url.
func chatURL(baseURL string) string { return strings.TrimRight(baseURL, "/") + "/chat/completions" }
