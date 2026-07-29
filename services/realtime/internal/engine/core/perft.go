package core

// Perft ("performance test", despite the name it's a correctness tool, not a
// benchmark) counts the number of legal move sequences of exactly the given
// depth from p. It is THE standard chess movegen correctness check: the
// correct node count for standard starting positions at each depth has been
// independently computed and published (chessprogramming.org/Perft_Results),
// so matching them is strong, well-established evidence that castling, en
// passant, promotion, check evasion, and pinned-piece handling are all
// simultaneously correct -- a single missed or extra edge case in any of
// them throws the count off, usually by a lot, well before depth 4-5.
func Perft(p *Position, depth int) uint64 {
	if depth == 0 {
		return 1
	}
	moves := GenerateLegalMoves(p)
	if depth == 1 {
		return uint64(len(moves))
	}
	var nodes uint64
	for _, m := range moves {
		u := p.MakeMove(m)
		nodes += Perft(p, depth-1)
		p.UnmakeMove(u)
	}
	return nodes
}

// PerftDivide returns the perft count for each root move separately --
// invaluable for isolating a movegen bug: run it on both the known-correct
// engine and the suspect one at the same depth, and the first root move
// whose count differs identifies exactly which piece/rule to look at, rather
// than debugging a single wrong aggregate number blind.
func PerftDivide(p *Position, depth int) map[string]uint64 {
	result := make(map[string]uint64)
	if depth < 1 {
		return result
	}
	for _, m := range GenerateLegalMoves(p) {
		u := p.MakeMove(m)
		result[moveToUCI(m)] = Perft(p, depth-1)
		p.UnmakeMove(u)
	}
	return result
}

// moveToUCI renders a move in UCI's plain "e2e4"/"e7e8q" form.
func moveToUCI(m Move) string {
	s := m.From.String() + m.To.String()
	if m.IsPromotion() {
		switch m.Promotion {
		case Knight:
			s += "n"
		case Bishop:
			s += "b"
		case Rook:
			s += "r"
		case Queen:
			s += "q"
		}
	}
	return s
}
