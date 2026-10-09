// SPDX-License-Identifier: MIT
package main

import (
	"fmt"

	"github.com/basdotio/AgentGuard/internal/config"
	"github.com/basdotio/AgentGuard/internal/judge"
	"github.com/basdotio/AgentGuard/internal/model"
)

// The judge block of a report is built here and only here, for the environment, a single target
// and the Downloads section alike, so all three name the judge the same way (P-031).
//
// Every block carries the two versions of this build's judge — prompt_version, computed over what
// the client sends, and excerpt_version, for how an artifact becomes the excerpt and how a quote is
// grounded — because the block exists exactly when --llm was requested, and a missing key should
// mean only "older report". A configured judge adds how it is run: the model every request names
// and the samples each question takes, both as the run will use them, and the endpoint, as before.
// Nothing else from the llm config goes in — no key, no headers, no authority.

// judgeConfigured starts the block for a judge configured to run over the given artifacts.
func judgeConfigured(llm config.LLMConfig, artifacts int) *model.JudgeSummary {
	return &model.JudgeSummary{
		Artifacts: artifacts, Endpoint: llm.BaseURL,
		PromptVersion: judge.PromptVersion(), ExcerptVersion: judge.ExcerptVersion,
		Model: judge.ModelFor(llm.Model), Samples: judge.SamplesFor(llm.Samples),
	}
}

// judgeNotConfigured is the block for a run where --llm was passed but the config does not enable
// the judge: it says why, and names no model or samples, since nothing was configured to run.
func judgeNotConfigured(artifacts int, reason string) *model.JudgeSummary {
	return &model.JudgeSummary{
		Artifacts: artifacts, Reason: reason,
		PromptVersion: judge.PromptVersion(), ExcerptVersion: judge.ExcerptVersion,
	}
}

// judgeVersionFields is what `aguard version` appends after rules=: the two versions of this build's
// judge. After every existing field, so $2 is still the version (release.yml) and the baselines
// adapter, which keeps the whole line as tool_version, records the judge build with no change.
func judgeVersionFields() string {
	return fmt.Sprintf(" · judge-prompt=%s · judge-excerpt=%d", judge.PromptVersion(), judge.ExcerptVersion)
}
