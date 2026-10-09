// SPDX-License-Identifier: MIT
package judge

// What a report records about how the judge was run, next to the two versions that name its code
// (P-031). Both are the EFFECTIVE values — what the requests carried and how many times each
// question was asked — not the configuration as written, because a report describes the run.

// ModelFor is the model every request names for a configured model name: the name itself, or the
// client's default when it is empty. It asks NewHTTP rather than restating the default, so a report
// cannot name a model the requests did not.
func ModelFor(configured string) string { return NewHTTP("", "", configured, nil).model }

// SamplesFor is how many times a run asks each question for a configured samples value: the value
// Options.defaults settles on, so `samples: 0` reads as the 1 the run actually took.
func SamplesFor(configured int) int { return Options{Samples: configured}.defaults().Samples }
