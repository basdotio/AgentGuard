// SPDX-License-Identifier: MIT

package judgefold

import "fmt"

// Select picks one sample's answer from its attempts, given in argument order — the directories
// of a run, its shards, its resumed parts and its whole-sample retries, merged by this one rule:
// the first complete answer wins. An attempt is taken or left whole, so votes from two draws of
// the model never meet in one row. With no complete attempt the first is kept and its row says so,
// with how many attempts there were; with no attempt at all there is nothing to select.
func Select(attempts []Answer) (Answer, bool) {
	if len(attempts) == 0 {
		return Answer{}, false
	}
	for _, a := range attempts {
		if a.Row.Incomplete == "" {
			return a, true
		}
	}
	first := attempts[0]
	if len(attempts) > 1 {
		row := first.Row
		row.Incomplete = fmt.Sprintf("%s (%d attempts, none complete)", row.Incomplete, len(attempts))
		first.Row = row
	}
	return first, true
}
