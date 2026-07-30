package search

// Transposition table with correct exact/lower/upper bound classification
// -- the plan explicitly flags the old engine's TT as never storing an
// ExactScore because alpha/beta were mutated in place before the bound
// flag was derived from them, so cutoffs almost never fired
// (search.go:440-445 in the old engine). The fix is structural, not a
// patch: capture the ORIGINAL alpha (before the search loop tightens it)
// and classify strictly against that original window when storing, every
// single time -- see negamax's alphaOrig.

type ttBound int8

const (
	boundExact ttBound = iota
	boundLower
	boundUpper
)

type ttEntry struct {
	key   uint64
	depth int
	score int
	bound ttBound
	valid bool
}

// TranspositionTable is a single fixed-size array of slots, one entry per
// index (key % len) -- always-replace, no bucket/aging scheme. Simple and
// correct; a real search hot path would want a proper replacement policy
// eventually (the plan notes the old engine's own TT "discards the whole
// map on overflow, and one thread can wipe another's table mid-search" --
// this one doesn't have that specific bug, since it's a fixed array with
// no map/no growth, but it also isn't concurrency-safe -- one Searcher,
// one goroutine, matching Phase 2's scope).
type TranspositionTable struct {
	entries []ttEntry
}

func NewTranspositionTable(size int) *TranspositionTable {
	if size <= 0 {
		size = 1
	}
	return &TranspositionTable{entries: make([]ttEntry, size)}
}

func (tt *TranspositionTable) index(key uint64) int {
	return int(key % uint64(len(tt.entries)))
}

// Probe returns a usable score for (key, depth, alpha, beta), if the
// stored entry is deep enough and its bound actually resolves the current
// window (an upper bound only helps if it already falls at/below alpha; a
// lower bound only helps if it already reaches/exceeds beta; an exact
// score always helps).
func (tt *TranspositionTable) Probe(key uint64, depth, alpha, beta int) (int, bool) {
	e := tt.entries[tt.index(key)]
	if !e.valid || e.key != key || e.depth < depth {
		return 0, false
	}
	switch e.bound {
	case boundExact:
		return e.score, true
	case boundLower:
		if e.score >= beta {
			return e.score, true
		}
	case boundUpper:
		if e.score <= alpha {
			return e.score, true
		}
	}
	return 0, false
}

// Store classifies and records score for key, using alphaOrig (the window
// this node was CALLED with, before its own search loop tightened alpha)
// and beta (which negamax never mutates locally) -- not the possibly-
// tightened alpha at the end of the loop, which is exactly the old
// engine's bug.
func (tt *TranspositionTable) Store(key uint64, depth, score, alphaOrig, beta int) {
	bound := boundExact
	switch {
	case score <= alphaOrig:
		bound = boundUpper
	case score >= beta:
		bound = boundLower
	}
	tt.entries[tt.index(key)] = ttEntry{key: key, depth: depth, score: score, bound: bound, valid: true}
}
