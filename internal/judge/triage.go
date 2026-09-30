// SPDX-License-Identifier: MIT
package judge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/basdotio/agent-guard/internal/model"
)

const maxTriageItems = 40 // cap findings sent per artifact (keeps the prompt bounded)

const triageTask = "You are a security reviewer triaging STATIC findings for one artifact (each line in the data " +
	"block is one finding: [RULE-ID] evidence). For EACH distinct rule id, judge whether it looks like a real risk or " +
	"likely-benign noise (documentation examples, pinned official URLs, test fixtures). You are ADVISORY ONLY — you cannot " +
	"remove or downgrade any finding; they all still stand and still count. " +
	"Reply with ONLY a compact JSON object: " +
	`{"labels": [{"rule_id": "<id>", "label": "likely-real|likely-benign", "reason": "<short>"}]}.`

// triageResp is the model's structured triage reply.
type triageResp struct {
	Labels []struct {
		RuleID string `json:"rule_id"`
		Label  string `json:"label"`
		Reason string `json:"reason"`
	} `json:"labels"`
}

// Triage asks the model to label an artifact's static findings likely-real vs likely-benign.
// The returned labels are DISPLAY-ONLY advisory annotations (spec §5.2.1): they never remove,
// downgrade, or reorder a finding. Untrusted item text is fenced with the nonce barrier.
func (c *HTTPClient) Triage(ctx context.Context, artifact string, items []TriageItem) ([]model.AdvisoryLabel, error) {
	if len(items) == 0 {
		return nil, nil
	}
	if len(items) > maxTriageItems {
		items = items[:maxTriageItems]
	}
	nonce, err := newNonce()
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	for _, it := range items {
		fmt.Fprintf(&b, "[%s] %s\n", it.RuleID, it.Evidence)
	}
	fence := "===AGUARD:" + nonce + "==="
	user := fmt.Sprintf("%s\n%s\n%s", fence, b.String(), fence)

	// Temperature 0: triage produces display labels, never a finding, so there is nothing
	// to take a vote on and no reason to pay for variance.
	content, err := c.chat(ctx, triageTask+" "+barrierRule(nonce), user, 0)
	if err != nil {
		return nil, err
	}
	return parseTriage(content), nil
}

// parseTriage extracts the labels object from a reply, tolerating prose/fences around it.
// A label the model omits simply yields no annotation for that rule (finding still shows).
func parseTriage(content string) []model.AdvisoryLabel {
	start := strings.IndexByte(content, '{')
	end := strings.LastIndexByte(content, '}')
	if start < 0 || end <= start {
		return nil
	}
	var tr triageResp
	if json.Unmarshal([]byte(content[start:end+1]), &tr) != nil {
		return nil
	}
	out := make([]model.AdvisoryLabel, 0, len(tr.Labels))
	for _, l := range tr.Labels {
		if l.RuleID == "" {
			continue
		}
		out = append(out, model.AdvisoryLabel{
			RuleID: l.RuleID,
			Label:  clampLabel(l.Label),
			Reason: l.Reason,
		})
	}
	return out
}

// clampLabel normalizes to the two allowed values; unknown → likely-real (safe side, so an
// LLM slip can never silently mark a genuine finding benign).
func clampLabel(s string) string {
	if strings.Contains(strings.ToLower(s), "benign") {
		return model.LabelBenign
	}
	return model.LabelReal
}
