// Package search implements move search: negamax with alpha-beta
// pruning (step 3), a transposition table keyed by board.Board.Hash()
// for both pruning and move ordering (TT move / MVV-LVA / killers /
// history — see the Move ordering section in search.go), quiescence
// search at leaf nodes to avoid the horizon effect, null-move pruning
// to skip subtrees that are fine even if the side to move does
// nothing, and iterative deepening with a time budget (step 6)
// layered on top of the same negamax. An opening book lookup runs in
// front of all of this — see opening_book.go.
//
// Step 10 adds four more tricks, three of them (LMR, PVS, aspiration
// windows) aimed at proving the SAME answer with a narrower alpha-beta
// window — never at changing which move is best — and the fourth (SEE)
// aimed at making capture ordering and quiescence pruning more accurate
// than MVV-LVA alone can be:
//   - Late Move Reductions (LMR): late, quiet, non-critical moves get
//     a cheap reduced-depth probe first, and only earn a full-depth
//     re-search if that probe suggests they might actually matter.
//   - Principal Variation Search (PVS/NegaScout): every move after
//     the first (expected, ordering-wise, to already be the best one)
//     gets a cheap null-window "is this even better than what we
//     have" probe before any move earns a full-window search.
//   - Aspiration windows: each iterative-deepening pass after the
//     first starts with a narrow window centered on the previous
//     pass's score instead of (-Infinity, Infinity), re-searching
//     with the full window only on the rare pass where the score
//     actually moved outside it.
//   - Static Exchange Evaluation (SEE, see.go): simulates a full
//     capture sequence on a square to estimate its true material
//     outcome, unlike MVV-LVA's "biggest victim, cheapest attacker"
//     heuristic which can't tell a winning capture from one that just
//     loses a piece to a defended square. Used by quiescenceSearch
//     below to skip clearly-losing captures and to order the rest by
//     their real expected value instead of the MVV-LVA proxy.
package search

import (
	"errors"
	"fmt"
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
// A raw checkmate leaf returns ply-MateScore, not a flat -MateScore
// (see negamax) — this is mate-distance adjustment: a mate found
// closer to the root (smaller ply) scores CLOSER to ±MateScore than
// one found deeper, so alpha-beta correctly prefers a faster mate
// over a slower one, and correctly prefers delaying an unavoidable
// mate as long as possible when on the losing side. Every non-leaf
// node just negates its child's score unchanged, so this ply-relative
// value propagates all the way to the root intact.
const MateScore = 900_000

// mateThreshold is the boundary used to recognize "this score
// represents some mate distance, not a normal material/positional
// eval" — anything at or beyond it is treated as a mate score by
// storeAdjustMateScore/readAdjustMateScore below. Comfortably below
// MateScore itself: even an absurdly deep search (thousands of plies)
// couldn't push a genuine mate-in-N score below this line, while no
// plausible eval.Evaluate output gets anywhere close to it.
const mateThreshold = MateScore - 1000

// storeAdjustMateScore converts a mate score from "relative to the
// search root" (what negamax computes and returns — see MateScore's
// comment) into "relative to this node" before it's cached in the
// transposition table. This is the fix for the documented TT bug:
// without it, a mate score cached while reaching a position at one
// ply from the root would be silently reused, unmodified, if that
// SAME position were later reached via a transposing line at a
// DIFFERENT ply — reporting the wrong mate distance (or in the worst
// case, treating a non-mate as one, or vice versa, since the absolute
// numbers no longer line up with the position's actual distance to
// mate from wherever it's currently being reached). Storing the
// node-relative value instead means readAdjustMateScore can correctly
// re-derive the right root-relative number no matter which ply the
// entry gets read back at.
func storeAdjustMateScore(score, ply int) int {
	switch {
	case score >= mateThreshold:
		return score + ply
	case score <= -mateThreshold:
		return score - ply
	default:
		return score
	}
}

// readAdjustMateScore is storeAdjustMateScore's exact inverse, applied
// when a cached score is read back out of the transposition table at
// the current node's ply (which may differ from the ply it was
// originally stored at — see storeAdjustMateScore's comment).
func readAdjustMateScore(score, ply int) int {
	switch {
	case score >= mateThreshold:
		return score - ply
	case score <= -mateThreshold:
		return score + ply
	default:
		return score
	}
}

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

	// nodes counts every negamax/quiescenceSearch call made using this
	// table, across however many iterative-deepening passes share it —
	// exposed to callers via SearchInfo.Nodes, for UCI's "info ...
	// nodes N" line. Deliberately NOT reset between BestMoveTimed's
	// depth-by-depth passes (only a brand new TranspositionTable, at
	// the start of a whole BestMove/BestMoveTimed call, starts this
	// back at 0) — a running total across the whole search is what UCI
	// GUIs and "nodes per second" displays actually expect.
	nodes int64
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

// SearchInfo summarizes one completed pass of search — either the
// single call BestMove/BestMoveInfo make, or one depth of
// BestMoveTimed/BestMoveTimedInfo's iterative deepening — for callers
// that want to report progress as it happens rather than only ever
// seeing the final move. Its one real consumer today is the UCI
// package's "info depth ... score ... nodes ... pv ..." line (see
// internal/uci/uci.go), but nothing here is UCI-specific; it's plain
// search-progress data.
type SearchInfo struct {
	Depth int
	Score int
	// PV is the principal variation found at this depth: the best
	// move at the root, then the best reply to that, and so on — see
	// principalVariation below for how it's reconstructed.
	PV []board.Move
	// Nodes is the running total of negamax/quiescence calls made so
	// far in this whole search (see TranspositionTable.nodes) — not
	// reset between depths within one BestMoveTimedInfo call.
	Nodes int64
}

// ScoreToUCI renders a raw negamax score (root-relative centipawns, or
// a mate score per MateScore's comment) the way UCI expects it: "cp N"
// for an ordinary evaluation, or "mate N" once the score represents a
// forced mate found somewhere in the tree — N moves until mate is
// delivered (not plies: UCI counts full moves, matching how a human
// would say "mate in 2"), positive if the side to move at the root
// delivers it, negative if the root's side is the one getting mated.
func ScoreToUCI(score int) string {
	if moves, isMate := mateDistanceInMoves(score); isMate {
		return fmt.Sprintf("mate %d", moves)
	}
	return fmt.Sprintf("cp %d", score)
}

// mateDistanceInMoves inverts the ply-MateScore construction described
// in MateScore's own comment: a score of magnitude >= mateThreshold
// encodes "mate found at this many plies from the root" as
// MateScore-ply, so recovering ply back out of the score and rounding
// half-moves up to full moves (ply 1 and 2 are both "mate in 1": White
// mates on ply 1, or is mated by Black's ply-2 reply to a move that
// already lost) gives the number UCI wants.
func mateDistanceInMoves(score int) (moves int, isMate bool) {
	abs := score
	if abs < 0 {
		abs = -abs
	}
	if abs < mateThreshold {
		return 0, false
	}
	plyAtMate := MateScore - abs
	moves = (plyAtMate + 1) / 2
	if score < 0 {
		moves = -moves
	}
	return moves, true
}

// principalVariation reconstructs the best line found from position b
// by repeatedly looking up the best move the transposition table
// recorded for the current position, applying it, and looking up the
// resulting position in turn — the same tt the search itself just
// populated, so this comes essentially for free instead of needing a
// dedicated triangular PV table threaded through negamax's recursion.
//
// Stops early, rather than risk showing a wrong or nonsensical line,
// if: there's no TT entry for the current position (most commonly
// because the line has run past negamax's own depth into territory
// only quiescence looked at, which doesn't write to the TT), the
// recorded move isn't actually legal there (a defensive check against
// the vanishingly rare 64-bit Zobrist hash collision — see Hash's
// comment in zobrist.go — silently walking a corrupted PV would be a
// worse outcome than just truncating it early), the same position
// would be visited a second time (a defensive guard against an actual
// cycle, which a correct search shouldn't produce but a bug or hash
// collision might), or maxLen moves have already been collected.
func principalVariation(b *board.Board, tt *TranspositionTable, maxLen int) []board.Move {
	if tt == nil {
		return nil
	}

	var pv []board.Move
	// b itself must not be mutated — it's the caller's actual search
	// root (or, one level up, the receiver negamax was handed) — so
	// this walk works on a disposable copy instead. Nothing here needs
	// to unmake anything: the copy is discarded once the loop ends.
	cur := b.Copy()
	seen := map[uint64]bool{}
	for len(pv) < maxLen {
		hash := cur.Hash()
		if seen[hash] {
			break
		}
		seen[hash] = true

		entry, ok := tt.entries[hash]
		if !ok || !isLegalMove(cur, entry.move) {
			break
		}
		pv = append(pv, entry.move)
		cur.MakeMove(entry.move)
	}
	return pv
}

func isLegalMove(b *board.Board, m board.Move) bool {
	for _, legal := range movegen.GenerateLegalMoves(b) {
		if legal == m {
			return true
		}
	}
	return false
}

// BestMove searches to a fixed depth (in plies) using negamax with
// alpha-beta pruning and a fresh transposition table, and returns the
// best move found for the side to move. depth 1 already means "look at
// every legal move and check whether it immediately delivers
// checkmate" (the terminal check happens before the depth==0 cutoff),
// so a mate-in-1 is found even at depth 1.
func BestMove(b *board.Board, depth int) (board.Move, error) {
	return BestMoveInfo(b, depth, nil)
}

// BestMoveInfo is BestMove with an added onInfo callback, invoked
// exactly once after the (single, fixed-depth) search completes —
// onInfo may be nil, in which case this behaves exactly like BestMove.
func BestMoveInfo(b *board.Board, depth int, onInfo func(SearchInfo)) (board.Move, error) {
	if len(movegen.GenerateLegalMoves(b)) == 0 {
		return board.Move{}, errors.New("search.BestMove: no legal moves in this position")
	}
	tt := NewTranspositionTable()
	score, bestMove := negamax(b, depth, 0, -Infinity, Infinity, true, tt)
	if onInfo != nil {
		onInfo(SearchInfo{Depth: depth, Score: score, PV: principalVariation(b, tt, depth), Nodes: tt.nodes})
	}
	return bestMove, nil
}

// aspirationDelta is the initial half-width of the window placed
// around the previous iteration's score, in centipawns. Narrow enough
// that most iterations actually benefit (fewer nodes explored before
// either bound proves out), wide enough that only a real swing in
// evaluation — a genuine tactic appearing at the new depth — triggers
// the full-window re-search below.
const aspirationDelta = 50

// aspirationMinDepth is the shallowest depth aspiration windows are
// attempted at. Depth 1 has no previous score to center a window on,
// and very shallow depths swing enough between iterations that a
// narrow window would just cause repeated fail-high/fail-low
// re-searches instead of saving any work.
const aspirationMinDepth = 4

// BestMoveTimed runs iterative deepening: it searches depth 1, then 2,
// then 3, and so on (sharing one transposition table across all of
// them, so each deeper pass benefits from move ordering learned by the
// previous one), stopping and returning the last FULLY completed
// depth's move once budget has elapsed or maxDepth is reached.
//
// From aspirationMinDepth onward, each pass first tries a narrow
// window centered on the PREVIOUS pass's score (see aspirationDelta)
// instead of the full (-Infinity, Infinity) — most of the time the
// score doesn't move much between one depth and the next, so the
// narrow window still contains the true score and proves it with
// fewer nodes. A score landing exactly on either edge of that window
// is only a bound, not the real value (alpha-beta never proves an
// exact score right at a window boundary), so that case re-searches
// the SAME depth with the full window before moving on — correctness
// never depends on the narrow window succeeding, only speed does.
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
	return BestMoveTimedInfo(b, maxDepth, budget, nil)
}

// BestMoveTimedInfo is BestMoveTimed with an added onInfo callback,
// invoked once after EVERY completed depth (not just the last one) —
// this is what lets a caller like the UCI loop print a progressively
// improving "info depth N score ... pv ..." line as the search goes,
// rather than only learning the outcome once iterative deepening stops
// altogether. onInfo may be nil, in which case this behaves exactly
// like BestMoveTimed.
func BestMoveTimedInfo(b *board.Board, maxDepth int, budget time.Duration, onInfo func(SearchInfo)) (board.Move, error) {
	if len(movegen.GenerateLegalMoves(b)) == 0 {
		return board.Move{}, errors.New("search.BestMoveTimed: no legal moves in this position")
	}

	tt := NewTranspositionTable()
	start := time.Now()

	var bestMove board.Move
	prevScore := 0
	for depth := 1; depth <= maxDepth; depth++ {
		if depth > 1 && time.Since(start) >= budget {
			break
		}

		alpha, beta := -Infinity, Infinity
		if depth >= aspirationMinDepth {
			alpha, beta = prevScore-aspirationDelta, prevScore+aspirationDelta
		}

		score, m := negamax(b, depth, 0, alpha, beta, true, tt)
		if score <= alpha || score >= beta {
			// Fail-low or fail-high: the narrow window didn't contain
			// the true score, so the move/score above can't be
			// trusted as-is. Re-search this same depth with the full
			// window rather than either widening incrementally or
			// silently accepting a bound as if it were exact.
			score, m = negamax(b, depth, 0, -Infinity, Infinity, true, tt)
		}

		bestMove = m
		prevScore = score

		if onInfo != nil {
			onInfo(SearchInfo{Depth: depth, Score: score, PV: principalVariation(b, tt, depth), Nodes: tt.nodes})
		}
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

// lmrMinDepth is the shallowest remaining depth Late Move Reductions
// bothers attempting at — below this there's barely anything left to
// shave off, so the extra branching (probe, maybe re-search) isn't
// worth it. Kept at the same value as nullMoveMinDepth for the same
// reason: with lmrReduction=1, depth-1-lmrReduction must stay >= 0,
// which any depth >= 2 already guarantees, but 3 leaves headroom to
// raise lmrReduction later without redoing this arithmetic.
const lmrMinDepth = 3

// lmrFullDepthMoves is how many moves at the front of the (already
// TT/MVV-LVA/killer/history-ordered) move list are always searched at
// full depth before LMR starts reducing — move ordering has already
// spent its best guesses on exactly these moves, so they're the ones
// least worth gambling a reduced search on.
const lmrFullDepthMoves = 4

// lmrReduction is how many extra plies, beyond the normal depth-1, a
// reduced move's initial probe skips. 1 is the conventional
// conservative starting value — aggressive enough to matter across a
// whole tree of late quiet moves, modest enough that a genuinely good
// move reliably still beats alpha at the reduced depth and triggers
// the full-depth re-search below rather than getting missed outright.
const lmrReduction = 1

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
// ply is how many plies deep from the search ROOT this call is (0 at
// the root, incremented by 1 on every recursive call — including the
// null-move probe). It exists purely for mate-distance adjustment —
// see MateScore's comment and storeAdjustMateScore/
// readAdjustMateScore above — and plays no other role in the search.
func negamax(b *board.Board, depth, ply, alpha, beta int, isRoot bool, tt *TranspositionTable) (score int, bestMove board.Move) {
	if tt != nil {
		tt.nodes++
	}
	origAlpha := alpha
	var hash uint64
	if tt != nil {
		hash = b.Hash()
		if entry, ok := tt.entries[hash]; ok && entry.depth >= depth {
			adjustedScore := readAdjustMateScore(entry.score, ply)
			switch entry.bound {
			case exactBound:
				return adjustedScore, entry.move
			case lowerBound:
				if adjustedScore > alpha {
					alpha = adjustedScore
				}
			case upperBound:
				if adjustedScore < beta {
					beta = adjustedScore
				}
			}
			if alpha >= beta {
				return adjustedScore, entry.move
			}
		}
	}

	// Generated once, right here, and reused for both the terminal-
	// status check immediately below and the move loop further down
	// (after null-move pruning and the depth==0 quiescence handoff,
	// neither of which need this list themselves). Generating a
	// position's legal moves is by far the most expensive part of a
	// search node — a full pseudo-legal pass plus a king-safety filter
	// per candidate — so computing it twice per node (once inside
	// GameStatus, once for the move loop, as this used to do) was a
	// real, measurable chunk of total search time, not a
	// micro-optimization.
	moves := movegen.GenerateLegalMoves(b)

	switch movegen.GameStatusFromMoves(b, moves) {
	case movegen.Checkmate:
		return ply - MateScore, board.Move{}
	case movegen.Stalemate, movegen.DrawFiftyMove, movegen.DrawInsufficientMaterial:
		return 0, board.Move{}
	}

	// Computed once and reused below by both null-move pruning's guard
	// and Late Move Reductions (see the move loop further down): being
	// in check disqualifies both heuristics for the same reason — every
	// reply is forced/critical here, not "probably safe to skip or
	// search shallowly".
	inCheck := movegen.InCheck(b)

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
	//   - !inCheck: "skip a turn while in check" isn't even a legal
	//     concept to reason about — you'd still be in check on the
	//     opponent's next move, which isn't a fair stand-in for what a
	//     real reply looks like.
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
		!inCheck && hasNonPawnMaterial(b, b.SideToMove) {
		nullUndo := b.MakeNullMove()
		nullScore, _ := negamax(b, depth-1-nullMoveReduction, ply+1, -beta, -beta+1, false, tt)
		nullScore = -nullScore
		b.UnmakeNullMove(nullUndo)
		if nullScore >= beta {
			return beta, board.Move{}
		}
	}

	if depth == 0 {
		return quiescence(b, ply, alpha, beta, tt), board.Move{}
	}

	// moves was already generated above, right after the TT lookup —
	// reused here rather than generated a second time (see the
	// comment there).

	var ttMove board.Move
	hasTTMove := false
	if tt != nil {
		if entry, ok := tt.entries[hash]; ok {
			ttMove, hasTTMove = entry.move, true
		}
	}
	orderMoves(b, moves, ttMove, hasTTMove, depth, tt)

	best := -Infinity
	bestHere := moves[0] // GameStatusFromMoves above already ruled out the empty-moves case

	for i, m := range moves {
		undo := b.MakeMove(m)

		// Late Move Reductions guard: move ordering has already put
		// its best guesses (TT move, captures by MVV-LVA, killers,
		// history) at the front of moves — a quiet move still this
		// far down the list, this deep in the tree, is unlikely to be
		// the best one here, so its FIRST probe (see below) is done a
		// few plies shallower than normal. Captures and promotions are
		// excluded (their tactical value isn't something a
		// reduced-depth glance reliably judges), and the whole thing
		// is skipped while in check, same reasoning as null-move
		// pruning's guard above.
		reduceEligible := !inCheck &&
			depth >= lmrMinDepth &&
			i >= lmrFullDepthMoves &&
			!m.IsCapture() &&
			m.Flag != board.Promotion && m.Flag != board.PromotionCapture

		var s int
		if i == 0 {
			// Principal Variation Search: the first move is exactly
			// the one move ordering has bet everything on being best
			// here. Rather than making it prove that with a cheap
			// probe first, it goes straight to the full window — the
			// probe/re-search dance below exists to cheaply REJECT
			// moves that don't beat alpha, and this move is expected
			// to set alpha, not merely clear it.
			childScore, _ := negamax(b, depth-1, ply+1, -beta, -alpha, false, tt)
			s = -childScore
		} else {
			// Every later move: a cheap null-window probe first — a
			// yes/no answer to "is this move even better than what we
			// already have (alpha)?" — before anything earns the
			// expensive full-window search. LMR folds in here as
			// simply a shallower depth for that first probe: a
			// reduced-depth null-window answer is cheaper still, and a
			// probe that fails to beat alpha at reduced depth almost
			// always also fails at full depth, so most late quiet
			// moves get rejected without ever running at full depth.
			searchDepth := depth - 1
			if reduceEligible {
				searchDepth = depth - 1 - lmrReduction
			}
			probeScore, _ := negamax(b, searchDepth, ply+1, -alpha-1, -alpha, false, tt)
			s = -probeScore

			if reduceEligible && s > alpha {
				// The reduced probe beat alpha — that alone might
				// just be an artifact of the shallower depth, not a
				// real signal. Re-confirm with the same cheap null
				// window at full depth before paying for a
				// full-window search.
				probeScore, _ = negamax(b, depth-1, ply+1, -alpha-1, -alpha, false, tt)
				s = -probeScore
			}

			if s > alpha && s < beta {
				// A null window only ever answers "beats alpha or
				// not" — it can't pin down the real score once that's
				// yes. Only a move that earned it gets the full-
				// window search that actually establishes its value.
				fullScore, _ := negamax(b, depth-1, ply+1, -beta, -alpha, false, tt)
				s = -fullScore
			}
		}

		b.UnmakeMove(m, undo)

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
		tt.entries[hash] = ttEntry{depth: depth, score: storeAdjustMateScore(best, ply), bound: resultBound, move: bestHere}
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
func quiescence(b *board.Board, ply, alpha, beta int, tt *TranspositionTable) int {
	return quiescenceSearch(b, ply, alpha, beta, maxQuiescenceCheckExtensions, tt)
}

func quiescenceSearch(b *board.Board, ply, alpha, beta, checkExtLeft int, tt *TranspositionTable) int {
	if tt != nil {
		tt.nodes++
	}
	inCheck := movegen.InCheck(b)
	legal := movegen.GenerateLegalMoves(b)

	if len(legal) == 0 {
		if inCheck {
			return ply - MateScore
		}
		return 0 // stalemate
	}
	if b.HalfmoveClock >= 100 || movegen.InsufficientMaterial(b) {
		return 0 // draw by 50-move rule or insufficient material — same
		// reasoning as negamax's GameStatus check: quiescence must not
		// walk past a boundary that's already a rules draw while
		// chasing a capture sequence, regardless of how "won" the
		// material looks.
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
			if m.IsCapture() && SEE(b, m) >= 0 {
				candidates = append(candidates, m)
			}
		}
		// Sort by SEE itself, not MVV-LVA: MVV-LVA is a cheap PROXY
		// for "which capture is probably good" (biggest victim, cheapest
		// attacker) that breaks down on defended pieces — pawn-takes-
		// defended-queen ranks above knight-takes-hanging-rook under
		// MVV-LVA even though the queen capture nets nothing once the
		// recapture is accounted for. SEE already computed the real
		// number for every candidate just above (to decide whether to
		// keep it at all), so sorting by that same number costs nothing
		// extra and orders captures by their actual expected outcome.
		sort.Slice(candidates, func(i, j int) bool {
			return SEE(b, candidates[i]) > SEE(b, candidates[j])
		})
	}

	nextCheckExtLeft := checkExtLeft
	if extendForCheck {
		nextCheckExtLeft--
	}

	for _, m := range candidates {
		undo := b.MakeMove(m)
		score := -quiescenceSearch(b, ply+1, -beta, -alpha, nextCheckExtLeft, tt)
		b.UnmakeMove(m, undo)
		if score >= beta {
			return beta
		}
		if score > alpha {
			alpha = score
		}
	}

	return alpha
}
