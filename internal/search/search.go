// Package search implements move search: negamax with alpha-beta
// pruning (step 3), a transposition table keyed by board.Board.Hash()
// for both pruning and move ordering (TT move / MVV-LVA / killers /
// history — see the Move ordering section in search.go), quiescence
// search at leaf nodes to avoid the horizon effect, null-move pruning
// to skip subtrees that are fine even if the side to move does
// nothing, and iterative deepening with a time budget (step 6)
// layered on top of the same negamax. An opening book lookup runs in
// front of all of this — see opening_book.go.
package search

import (
	"errors"
	"sort"
	"time"

	"github.com/yourname/dyanis-chess-engine/internal/board"
	"github.com/yourname/dyanis-chess-engine/internal/eval"
	"github.com/yourname/dyanis-chess-engine/internal/movegen"
)

// Infinity is a value larger than any realistic evaluation score,
// used as the initial alpha-beta window bound.
const Infinity = 1_000_000

// MateScore is returned for a checkmated position — deliberately far
// larger than any material score so mate is always preferred over
// material gain, but still comfortably below Infinity so alpha-beta
// bounds don't collide with it.
//
// Caveat: this version doesn't do mate-distance adjustment (preferring
// a mate in 1 over a mate in 3), so at deeper search the engine may be
// slow to actually deliver a mate it already "sees" — a reasonable
// follow-up once this basic version is verified working.
const MateScore = 900_000

// --- Transposition table -------------------------------------------

// bound records which side of the true score a stored value
// represents, since alpha-beta search on its own only ever proves a
// position is "at least this good" (a cutoff on beta) or "at most
// this good" (nothing beat alpha) unless the full window was
// searched — only an exact-bound entry can be trusted as the position's
// actual score outright.
type bound uint8

const (
	exactBound bound = iota
	lowerBound       // stored score is a lower bound (search failed high, score >= beta)
	upperBound       // stored score is an upper bound (search failed low, score <= alpha)
)

type ttEntry struct {
	depth int
	score int
	bound bound
	move  board.Move
}

// killerSlots is how many killer moves are remembered per depth
// level. Two is the conventional choice: enough that a second good
// quiet move at a given depth still gets tried early even after the
// first killer stops applying (e.g. it's no longer legal in a
// sibling position), without the bookkeeping of a longer list.
const killerSlots = 2

// historyKey identifies a quiet move for the history heuristic by
// from/to squares alone (not piece type or flags) — same convention
// TT's own move-equality checks already rely on implicitly via
// board.Move's == comparisons.
type historyKey struct {
	From, To board.Square
}

// TranspositionTable caches negamax results by position hash, so a
// position reached by a different move order (a "transposition")
// doesn't get re-searched from scratch, and so the best move found for
// a position at a shallow iterative-deepening depth can be tried FIRST
// at the next, deeper iteration — a good first move is what lets
// alpha-beta actually prune effectively.
//
// Implementation: a plain Go map keyed by the full 64-bit Zobrist
// hash. That's simpler than the fixed-size array + replacement-scheme
// approach faster engines use, at the cost of unbounded memory growth
// over a long search — acceptable for this project's current depths,
// worth revisiting (fixed-size table, always-replace or depth-
// preferred replacement) if that ever becomes a real problem.
//
// killers and history also live here, even though neither is a
// position cache: a TranspositionTable is already the one piece of
// state threaded through an entire top-level search call (BestMove/
// BestMoveTimed each construct a fresh one), which is exactly the
// scope both move-ordering aids need — reset between unrelated
// searches, shared across every node and every iterative-deepening
// pass within one search. killers is indexed by *remaining* depth
// rather than absolute ply from the root, which conflates killers
// from different lines that happen to reach the same remaining depth
// — a known simplification (see negamax's move-ordering comment)
// rather than the more precise but more bookkeeping-heavy per-ply
// version.
type TranspositionTable struct {
	entries map[uint64]ttEntry
	killers [][killerSlots]board.Move
	history map[historyKey]int
}

// NewTranspositionTable returns an empty table.
func NewTranspositionTable() *TranspositionTable {
	return &TranspositionTable{
		entries: make(map[uint64]ttEntry),
		history: make(map[historyKey]int),
	}
}

// killersAt returns the killer-move slots for a given remaining
// depth, growing the backing slice on demand — depths are small,
// bounded ints (rarely more than a couple dozen even with iterative
// deepening), so a plain growable slice is simpler than a map here.
func (tt *TranspositionTable) killersAt(depth int) *[killerSlots]board.Move {
	for len(tt.killers) <= depth {
		tt.killers = append(tt.killers, [killerSlots]board.Move{})
	}
	return &tt.killers[depth]
}

// recordKiller notes that m (a quiet move) caused a beta cutoff at
// this depth, pushing it into slot 0 and bumping the previous slot-0
// killer down to slot 1 — unless m is already slot 0, in which case
// there's nothing to do (repeatedly re-recording the same killer
// shouldn't demote it).
func (tt *TranspositionTable) recordKiller(depth int, m board.Move) {
	slots := tt.killersAt(depth)
	if slots[0] == m {
		return
	}
	slots[1] = slots[0]
	slots[0] = m
}

// recordHistory rewards a quiet move that caused a cutoff, weighted
// by depth*depth so cutoffs found deeper in the tree (typically
// harder-won, and more broadly applicable) count for more than
// shallow ones. This accumulates across the WHOLE search (unlike
// killers, which are depth-scoped), on the theory that a quiet move
// which has been strong in many different positions during this
// search is a reasonable one to try early in a new position too.
func (tt *TranspositionTable) recordHistory(m board.Move, depth int) {
	tt.history[historyKey{m.From, m.To}] += depth * depth
}

func (tt *TranspositionTable) historyScore(m board.Move) int {
	return tt.history[historyKey{m.From, m.To}]
}

// --- Search -----------------------------------------------------------

// BestMove searches to a fixed depth (in plies) using negamax with
// alpha-beta pruning and a fresh transposition table, and returns the
// best move found for the side to move. depth 1 already means "look at
// every legal move and check whether it immediately delivers
// checkmate" (the terminal check happens before the depth==0 cutoff),
// so a mate-in-1 is found even at depth 1.
func BestMove(b *board.Board, depth int) (board.Move, error) {
	if len(movegen.GenerateLegalMoves(b)) == 0 {
		return board.Move{}, errors.New("search.BestMove: no legal moves in this position")
	}
	tt := NewTranspositionTable()
	_, bestMove := negamax(b, depth, -Infinity, Infinity, true, tt)
	return bestMove, nil
}

// BestMoveTimed runs iterative deepening: it searches depth 1, then 2,
// then 3, and so on (sharing one transposition table across all of
// them, so each deeper pass benefits from move ordering learned by the
// previous one), stopping and returning the last FULLY completed
// depth's move once budget has elapsed or maxDepth is reached.
//
// The time check only happens BETWEEN depths, not during one — there's
// no mid-search cancellation plumbed through negamax's recursion, so a
// single iteration that turns out to be slow (e.g. a sudden tactical
// position at the max depth) can overshoot budget by however long that
// iteration takes. Depth 1 always runs regardless of budget, so this
// never returns a zero-value move even if budget is very small (or
// zero). Proper node-count-based mid-search cutoffs are a reasonable
// future improvement if overshoot ever becomes a real problem.
func BestMoveTimed(b *board.Board, maxDepth int, budget time.Duration) (board.Move, error) {
	if len(movegen.GenerateLegalMoves(b)) == 0 {
		return board.Move{}, errors.New("search.BestMoveTimed: no legal moves in this position")
	}

	tt := NewTranspositionTable()
	start := time.Now()

	var bestMove board.Move
	for depth := 1; depth <= maxDepth; depth++ {
		if depth > 1 && time.Since(start) >= budget {
			break
		}
		_, m := negamax(b, depth, -Infinity, Infinity, true, tt)
		bestMove = m
	}
	return bestMove, nil
}

// nullMoveReduction (R) is how many EXTRA plies, beyond the mandatory
// one, the null-move probe skips compared to a real move — i.e. it
// searches to depth-1-nullMoveReduction instead of depth-1. 2 is the
// conventional starting value: aggressive enough to matter, modest
// enough to stay safe without a verification search on top.
const nullMoveReduction = 2

// nullMoveMinDepth is the shallowest depth NMP bothers attempting at.
// With nullMoveReduction=2, this must be at least 3 for the reduced
// search's depth (depth-1-nullMoveReduction) to never go negative.
const nullMoveMinDepth = 3

// hasNonPawnMaterial reports whether color c has any piece besides
// pawns and the king — the standard null-move pruning guard against
// zugzwang. In king-and-pawn endgames "doing nothing" is often
// exactly what the side to move would want but structurally can't (a
// real move must be played, and every one may worsen the position),
// so a null-move probe's "even doing nothing is fine" conclusion
// isn't trustworthy there.
func hasNonPawnMaterial(b *board.Board, c board.Color) bool {
	for sq := board.Square(0); sq < 64; sq++ {
		p := b.PieceAt(sq)
		if p.IsNone() || p.Color() != c {
			continue
		}
		if t := p.Type(); t != board.Pawn && t != board.King {
			return true
		}
	}
	return false
}

// negamax evaluates a position from the perspective of the side to
// move, exploring `depth` more plies, and returns both the score and
// the best move found at this node (callers above the leaves use the
// move; leaves return the zero value, which is never read since a
// leaf's caller only wants its score).
//
// Alpha-beta pruning: once we find a move so good the opponent would
// never let us reach this position (score >= beta), we stop looking at
// this node's remaining moves — the opponent has a better alternative
// earlier in the tree that avoids this position entirely, so its exact
// value doesn't matter.
//
// tt may be nil, in which case this behaves as plain negamax with no
// caching or move-ordering help — kept possible mainly so the table
// isn't a hidden requirement threaded through every call site, though
// in practice every current caller passes one.
//
// isRoot must be true only for the top-level call (from BestMove/
// BestMoveTimed) and false for every recursive call. It exists solely
// to disable null-move pruning at the root — see the NMP comment
// below for why: a null-move cutoff skips computing bestHere, which
// is fine deep in the tree (parents only ever read a child's score,
// never its move — see the move loop below), but BestMove reads the
// ROOT call's returned move directly, so the root can never take that
// shortcut.
func negamax(b *board.Board, depth, alpha, beta int, isRoot bool, tt *TranspositionTable) (score int, bestMove board.Move) {
	origAlpha := alpha
	var hash uint64
	if tt != nil {
		hash = b.Hash()
		if entry, ok := tt.entries[hash]; ok && entry.depth >= depth {
			switch entry.bound {
			case exactBound:
				return entry.score, entry.move
			case lowerBound:
				if entry.score > alpha {
					alpha = entry.score
				}
			case upperBound:
				if entry.score < beta {
					beta = entry.score
				}
			}
			if alpha >= beta {
				return entry.score, entry.move
			}
		}
	}

	switch movegen.GameStatus(b) {
	case movegen.Checkmate:
		return -MateScore, board.Move{}
	case movegen.Stalemate:
		return 0, board.Move{}
	}

	// Null-move pruning: "if I skipped my turn entirely and the
	// opponent STILL couldn't punish me enough to matter, then any
	// real move I make — which is only ever at least as good as doing
	// nothing — also doesn't need full-width searching here." A
	// reduced-depth search from the null-move position that still
	// fails high (>= beta) is treated as proof of that, and this
	// subtree gets pruned without ever generating or trying its real
	// moves.
	//
	// Guards, each one a known way this heuristic gives wrong answers
	// if left unchecked:
	//   - !isRoot: see negamax's doc comment — the root must return a
	//     real move, which a null-move cutoff never computes.
	//   - !movegen.InCheck(b): "skip a turn while in check" isn't
	//     even a legal concept to reason about — you'd still be in
	//     check on the opponent's next move, which isn't a fair
	//     stand-in for what a real reply looks like.
	//   - hasNonPawnMaterial: in king-and-pawn-only endgames, "doing
	//     nothing" is frequently illegal in spirit (zugzwang) — the
	//     side to move often WANTS to pass but can't, and every legal
	//     move actively worsens their position. Null-move pruning
	//     would badly misjudge exactly these positions, so it's
	//     disabled whenever the side to move has no pieces besides
	//     pawns and king.
	//   - beta < MateScore: keeps this heuristic away from positions
	//     where a forced mate is already being proven/refuted, where
	//     a reduced-depth null-move probe's score isn't a reliable
	//     enough signal to short-circuit on.
	//   - depth >= nullMoveMinDepth: below this there's barely
	//     anything left to reduce into, so the overhead of the probe
	//     isn't worth attempting.
	if !isRoot && depth >= nullMoveMinDepth && beta < MateScore &&
		!movegen.InCheck(b) && hasNonPawnMaterial(b, b.SideToMove) {
		nullChild := b.MakeNullMove()
		nullScore, _ := negamax(nullChild, depth-1-nullMoveReduction, -beta, -beta+1, false, tt)
		nullScore = -nullScore
		if nullScore >= beta {
			return beta, board.Move{}
		}
	}

	if depth == 0 {
		return quiescence(b, alpha, beta), board.Move{}
	}

	moves := movegen.GenerateLegalMoves(b)

	var ttMove board.Move
	hasTTMove := false
	if tt != nil {
		if entry, ok := tt.entries[hash]; ok {
			ttMove, hasTTMove = entry.move, true
		}
	}
	orderMoves(b, moves, ttMove, hasTTMove, depth, tt)

	best := -Infinity
	bestHere := moves[0] // GameStatus above already ruled out the empty-moves case

	for _, m := range moves {
		child := b.MakeMove(m)
		childScore, _ := negamax(child, depth-1, -beta, -alpha, false, tt)
		s := -childScore

		if s > best {
			best = s
			bestHere = m
		}
		if best > alpha {
			alpha = best
		}
		if alpha >= beta {
			// Move-ordering aids only care about QUIET cutoff moves —
			// captures already get their own MVV-LVA ordering, and a
			// killer/history slot "this capture is great" would just
			// be redundant with that.
			if tt != nil && !m.IsCapture() {
				tt.recordKiller(depth, m)
				tt.recordHistory(m, depth)
			}
			break // alpha-beta cutoff
		}
	}

	if tt != nil {
		resultBound := exactBound
		if best <= origAlpha {
			resultBound = upperBound
		} else if best >= beta {
			resultBound = lowerBound
		}
		tt.entries[hash] = ttEntry{depth: depth, score: best, bound: resultBound, move: bestHere}
	}

	return best, bestHere
}

// --- Move ordering -------------------------------------------------
//
// Alpha-beta pruning's effectiveness depends entirely on searching
// the BEST move at each node first — a cutoff can only happen after
// something has already raised alpha, so a good first guess is what
// lets later siblings get skipped instead of fully searched. This
// section is the "good first guess" logic negamax's move loop above
// relies on, layered in priority order:
//
//  1. the transposition table's remembered best move for this exact
//     position (from a shallower iterative-deepening pass, or an
//     earlier visit via a different move order)
//  2. captures, ordered by MVV-LVA (Most Valuable Victim, Least
//     Valuable Attacker) — try pxq before qxp
//  3. killer moves: quiet moves that caused a beta cutoff in a
//     sibling position at this same remaining depth
//  4. everything else, by history heuristic score (accumulated across
//     the whole search, not just this depth)

// pieceOrderingValue gives a cheap, ordering-only weight per piece
// type — NOT the same table eval.Evaluate uses for scoring positions,
// just enough to rank captures by "which piece is taken / which piece
// is doing the taking".
var pieceOrderingValue = map[board.PieceType]int{
	board.Pawn:   100,
	board.Knight: 320,
	board.Bishop: 330,
	board.Rook:   500,
	board.Queen:  900,
	board.King:   20000,
}

// mvvLva scores a capture so that sorting descending tries the most
// valuable victim / least valuable attacker combinations first (e.g.
// pawn takes queen before queen takes pawn). Multiplying the victim's
// value by 10 spreads it out enough that even the cheapest attacker
// capturing the most valuable victim always outranks the most
// expensive attacker capturing the least valuable victim.
func mvvLva(b *board.Board, m board.Move) int {
	victimType := board.Pawn // en passant always takes a pawn, and that
	// pawn isn't on m.To (see move.go), so PieceAt(m.To) would see
	// nothing there — special-cased rather than misreading it as None.
	if m.Flag != board.EnPassantCapture {
		victimType = b.PieceAt(m.To).Type()
	}
	attackerType := b.PieceAt(m.From).Type()
	return pieceOrderingValue[victimType]*10 - pieceOrderingValue[attackerType]
}

// Score bands keep the four ordering tiers from ever overlapping:
// even the worst-scored capture (mvvLva can go slightly negative,
// e.g. pawn takes nothing valuable while a queen is the attacker)
// still outranks every killer/history-only quiet move, and the worst
// killer still outranks a history score of 0.
const (
	ttMoveScore = 1_000_000
	captureBand = 100_000
	killerBand  = 10_000
)

func moveOrderingScore(b *board.Board, m, ttMove board.Move, hasTTMove bool, killers [killerSlots]board.Move, tt *TranspositionTable) int {
	if hasTTMove && m == ttMove {
		return ttMoveScore
	}
	if m.IsCapture() {
		return captureBand + mvvLva(b, m)
	}
	for i, k := range killers {
		if m == k {
			return killerBand - i // slot 0 ranks just above slot 1
		}
	}
	if tt != nil {
		return tt.historyScore(m)
	}
	return 0
}

// orderMoves sorts moves in place, best-guess-first, per the priority
// list above. tt may be nil (see negamax's own nil-tt comment) — in
// that case there's no TT move, no killers, and no history, so this
// degrades to leaving moves in generation order except for putting
// captures (still MVV-LVA sorted) ahead of quiet moves, which costs
// nothing and is never wrong to do.
func orderMoves(b *board.Board, moves []board.Move, ttMove board.Move, hasTTMove bool, depth int, tt *TranspositionTable) {
	var killers [killerSlots]board.Move
	if tt != nil {
		killers = *tt.killersAt(depth)
	}

	scores := make([]int, len(moves))
	for i, m := range moves {
		scores[i] = moveOrderingScore(b, m, ttMove, hasTTMove, killers, tt)
	}
	sortByScore(moves, scores)
}

// sortByScore sorts moves descending by their parallel scores slice.
// Sorting a slice of indices and then materializing the result is
// simpler than trying to make sort.Slice permute two slices in
// lockstep (its swap only knows about the one slice it's given).
func sortByScore(moves []board.Move, scores []int) {
	idx := make([]int, len(moves))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(i, j int) bool {
		return scores[idx[i]] > scores[idx[j]]
	})

	ordered := make([]board.Move, len(moves))
	for i, j := range idx {
		ordered[i] = moves[j]
	}
	copy(moves, ordered)
}

// --- Quiescence search -------------------------------------------------

// maxQuiescenceCheckExtensions bounds how many plies of "must respond
// to check" quiescence will follow down a single line before it's
// willing to fall back to evaluating the position anyway. Without
// this cap, a long forced-check sequence (each side giving check,
// neither side capturing anything) could make quiescence recurse far
// deeper than intended, since "in check" moves aren't narrowed down
// to captures the way normal quiescence nodes are — see
// quiescenceSearch. Six plies is a fairly generous allowance for a
// real check sequence while still being a hard ceiling on the
// pathological case.
const maxQuiescenceCheckExtensions = 6

// quiescence is negamax's leaf-node evaluation, but instead of
// statically scoring whatever position depth 0 happens to land on, it
// keeps searching through "noisy" moves (captures, and check
// evasions) until the position settles down or the check-extension
// budget runs out. This is what fixes the horizon effect: plain
// negamax stopping exactly at depth 0 mid-capture-exchange would
// score "I just won a pawn" as good even when the very next move (one
// ply past the search horizon) recaptures a whole piece back.
func quiescence(b *board.Board, alpha, beta int) int {
	return quiescenceSearch(b, alpha, beta, maxQuiescenceCheckExtensions)
}

func quiescenceSearch(b *board.Board, alpha, beta, checkExtLeft int) int {
	inCheck := movegen.InCheck(b)
	legal := movegen.GenerateLegalMoves(b)

	if len(legal) == 0 {
		if inCheck {
			return -MateScore
		}
		return 0 // stalemate
	}

	// extendForCheck: while in check, there's no "standing pat" —
	// every legal reply (block, capture, king move) has to be
	// considered, not just captures, since being in check is itself
	// the noise quiescence exists to search through. Once the
	// check-extension budget is spent, this degrades to the normal
	// captures-only handling below even if still in check — a
	// pragmatic cap, not a fully sound one (see the const's comment).
	extendForCheck := inCheck && checkExtLeft > 0

	var candidates []board.Move
	if extendForCheck {
		candidates = legal
	} else {
		// standPat: the static evaluation of just... not capturing
		// anything here. A player is never forced to capture, so this
		// is a valid lower bound on the position's value — if simply
		// stopping here is already good enough to cause a beta
		// cutoff, there's no need to look at any capture at all.
		standPat := eval.Evaluate(b)
		if standPat >= beta {
			return beta
		}
		if standPat > alpha {
			alpha = standPat
		}
		for _, m := range legal {
			if m.IsCapture() {
				candidates = append(candidates, m)
			}
		}
		// Same MVV-LVA reasoning as negamax's move ordering: trying
		// the most-promising captures first lets the beta cutoff
		// below trigger sooner, pruning the rest of candidates.
		sort.Slice(candidates, func(i, j int) bool {
			return mvvLva(b, candidates[i]) > mvvLva(b, candidates[j])
		})
	}

	nextCheckExtLeft := checkExtLeft
	if extendForCheck {
		nextCheckExtLeft--
	}

	for _, m := range candidates {
		child := b.MakeMove(m)
		score := -quiescenceSearch(child, -beta, -alpha, nextCheckExtLeft)
		if score >= beta {
			return beta
		}
		if score > alpha {
			alpha = score
		}
	}

	return alpha
}
