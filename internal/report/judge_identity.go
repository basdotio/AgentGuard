// SPDX-License-Identifier: MIT
package report

import (
	"fmt"

	"github.com/basdotio/AgentGuard/internal/model"
)

// judgeIdentityLine names the judge a report's LLM findings came from (P-031): the model and
// samples the run used and the two versions of this build's judge, under the judge summary line.
// Derived only — every value is a field of the judge block, printed as is. "" when the judge did not
// run (it produced nothing to attribute) or the block predates the versions.
//
// The model comes from the operator's config, never from the scanned tree, so the renderers treat it
// like the endpoint reason beside it: Sanitize in the terminal and HTML (sanitizeResult), text() in
// markdown.
func judgeIdentityLine(j *model.JudgeSummary) string {
	if j == nil || !j.Ran || j.PromptVersion == "" {
		return ""
	}
	return fmt.Sprintf("LLM judge: model %s · samples %d · prompt_version %s · excerpt_version %d",
		j.Model, j.Samples, j.PromptVersion, j.ExcerptVersion)
}
