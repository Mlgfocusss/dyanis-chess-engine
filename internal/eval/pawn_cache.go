package eval

import "github.com/yourname/dyanis-chess-engine/internal/board"

// pawnCacheSize is the pawn-structure cache's slot count — a fixed
// power of two, the same masking trick search.TranspositionTable's
// own position table uses (index = key & pawnCacheMask instead of a
// modulo). Much smaller than the main TT: pawn structure changes far
// less often than the position as a whole — any move that isn't a
// pawn move or a capture of a pawn leaves it completely untouched —
// so far fewer distinct structures ever need a slot.
const pawnCacheSize = 1 << 14
const pawnCacheMask = pawnCacheSize - 1

type pawnCacheEntry struct {
	key   uint64
	valid bool
	score int
}

// PawnCache caches pawnStructureScore's result, keyed by the two pawn
// bitboards alone — doubled/isolated/passed-pawn scoring depends on
// nothing else about the position. That gives it a real hit rate in
// practice: the overwhelming majority of moves in a search tree
// (every non-pawn, non-pawn-capturing move) leave the pawn structure
// completely untouched, so many different nodes share the exact same
// two bitboards and can reuse one cached score instead of
// recomputing doubled/isolated/passed-pawn status from scratch every
// time.
//
// Deliberately NOT a package-level global cache: scoped the same way
// TranspositionTable's own history/killers already are — one
// PawnCache per top-level search call (BestMove/BestMoveTimed each
// construct a fresh TranspositionTable, which owns one of these — see
// search.go), not shared across searches. A global would need a
// mutex the moment this engine's search becomes multi-threaded (or
// worse, silently return one search's cached score to a completely
// different, concurrent search); a per-search cache needs neither,
// since nothing outside that one search call ever touches it.
type PawnCache struct {
	entries []pawnCacheEntry
}

// NewPawnCache returns an empty cache.
func NewPawnCache() *PawnCache {
	return &PawnCache{entries: make([]pawnCacheEntry, pawnCacheSize)}
}

// pawnCacheKey combines both colors' pawn bitboards into one lookup
// key — a small splitmix64-style mix. Not cryptographic, just enough
// to spread real pawn-bitboard values reasonably evenly across the
// table; collisions are handled the same way search.TranspositionTable
// handles them (see its probe's comment) — the full key is stored
// alongside the cached score and checked on lookup, so a collision
// costs a cache miss, never a wrong answer.
func pawnCacheKey(white, black board.Bitboard) uint64 {
	h := uint64(white)*0x9E3779B97F4A7C15 ^ uint64(black)*0xC2B2AE3D27D4EB4F
	h ^= h >> 33
	h *= 0xFF51AFD7ED558CCD
	h ^= h >> 33
	return h
}
