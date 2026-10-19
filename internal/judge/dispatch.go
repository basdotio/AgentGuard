// SPDX-License-Identifier: MIT
package judge

// One nonce per question, and the order a free worker takes calls in (P-080).
//
// At samples k a question is the same request sent k times. Under a nonce per call the k requests
// shared no prefix longer than the pass's first sentence, so an endpoint's prompt cache (OpenAI's
// minimum is a 1,024-token identical prefix) could never serve a repeat. With one nonce per
// question the k requests are byte-identical; a repeat that is sent after its first was answered
// can be served from the prefix the first just paid for.
//
// Sharing the nonce keeps the barrier (spec §5.2): the scanned content is fixed before the nonce
// is drawn and never sees it, and no reply is fed into another sample's request, so a nonce shared
// by the k requests of one question is as unguessable to that content as k different ones. A
// re-ask (P-069) still draws its own (requestNonce); triage is one call and draws per call.

// withQuestionNonces returns a copy of tasks where each judge question's samples carry one nonce
// of their own. A draw that fails leaves the field empty: the client then draws per call and fails
// the call if it cannot, so there is never a guessable barrier (newNonce). tasks is not modified.
func withQuestionNonces(tasks []task) []task {
	out := append([]task(nil), tasks...)
	nonces := map[int]string{}
	for i := range out {
		if out[i].kind != taskJudge {
			continue
		}
		n, ok := nonces[out[i].group]
		if !ok {
			n, _ = newNonce() // "" on failure: the client's own draw reports it
			nonces[out[i].group] = n
		}
		out[i].req.Nonce = n
	}
	return out
}

// dispatchQueue decides which task a free worker takes next. A question's first sample is ready
// at once; its other samples wait until the first finished, so they reach the endpoint after the
// prefix they share is cached. They wait only while there is other work: a worker that would
// otherwise sit idle takes the earliest question's waiting samples instead. A run with more
// workers than calls (a check of one skill) therefore sends every call at once, as it did before
// the nonce was shared, and a busy run never leaves a worker idle that the old order kept busy —
// the queue changes which ready call a worker takes, never how many are in flight.
//
// With one worker the order is the table order, as before: a first finishes before anything
// else is taken, and its samples are next. Results are written by table index whatever the order
// (invariant #6).
type dispatchQueue struct {
	released []int         // samples whose first finished (or that were released early), in that order
	ready    []int         // firsts, triage and single questions, in table order
	waiting  map[int][]int // first sample's index → its question's other samples, until released
	order    []int         // firsts whose samples may still be waiting, in table order
}

func newDispatchQueue(tasks []task) *dispatchQueue {
	q := &dispatchQueue{waiting: map[int][]int{}}
	first := map[int]int{} // judge group → index of its first sample
	for i, t := range tasks {
		if t.kind != taskJudge {
			q.ready = append(q.ready, i)
			continue
		}
		f, ok := first[t.group]
		if !ok {
			first[t.group] = i
			q.ready = append(q.ready, i)
			continue
		}
		if len(q.waiting[f]) == 0 {
			q.order = append(q.order, f)
		}
		q.waiting[f] = append(q.waiting[f], i)
	}
	return q
}

// next returns the task a free worker should take, and false when every task has been taken.
func (q *dispatchQueue) next() (int, bool) {
	if len(q.released) == 0 && len(q.ready) == 0 {
		q.releaseEarliest()
	}
	switch {
	case len(q.released) > 0:
		i := q.released[0]
		q.released = q.released[1:]
		return i, true
	case len(q.ready) > 0:
		i := q.ready[0]
		q.ready = q.ready[1:]
		return i, true
	}
	return 0, false
}

// finished records that task i completed, answered or not: if it was a first whose samples still
// wait, they become the next to be taken. A failed first releases them the same way — there is
// nothing to gain by holding them, and they are the question's remaining votes.
func (q *dispatchQueue) finished(i int) {
	q.release(i)
}

// releaseEarliest releases the samples of the earliest question that still has some waiting.
func (q *dispatchQueue) releaseEarliest() {
	for len(q.order) > 0 {
		f := q.order[0]
		if _, ok := q.waiting[f]; ok {
			q.release(f)
			return
		}
		q.order = q.order[1:]
	}
}

func (q *dispatchQueue) release(first int) {
	samples, ok := q.waiting[first]
	if !ok {
		return
	}
	delete(q.waiting, first)
	q.released = append(q.released, samples...)
}
