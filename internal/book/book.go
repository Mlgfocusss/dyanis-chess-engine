// Package book reads Polyglot-format (.bin) opening books: a lookup
// table from Zobrist position hash to a set of known good moves with
// weights. Search should consult Book.Lookup before running its own
// search; if there's a hit, play from the book (instant, "theoretical"
// looking moves); if not, fall through to normal search.
package book

import (
	"encoding/binary"
	"os"

	"github.com/yourname/dyanis-chess-engine/internal/board"
)

// Entry is one 16-byte record in a Polyglot .bin file.
type Entry struct {
	Key    uint64 // Zobrist hash of the position
	Move   uint16 // Polyglot-encoded move (from/to/promotion bit-packed)
	Weight uint16 // relative frequency/quality, used to pick among options
	Learn  uint32 // rarely used learning data, kept for completeness
}

const entrySize = 16

// Book is an in-memory Polyglot opening book.
type Book struct {
	entries []Entry
}

// Load reads a Polyglot .bin file from disk. See
// http://hgm.nubati.net/book_format.html for the exact entry layout.
// This is a thin os.ReadFile wrapper around Parse — kept separate
// because Parse is also the entry point for callers that already have
// the bytes in memory and no filesystem to read from, such as the
// wasm build running in a browser (see cmd/wasm/main.go).
func Load(path string) (*Book, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse decodes Polyglot .bin data already in memory. Same format
// Load reads from disk; this just skips the os.ReadFile step, for
// callers that obtained the bytes some other way (e.g. a browser
// fetch()'d the .bin and handed the bytes to Go via wasm — there's no
// filesystem to open a path against in that environment).
func Parse(data []byte) (*Book, error) {
	b := &Book{entries: make([]Entry, 0, len(data)/entrySize)}
	for i := 0; i+entrySize <= len(data); i += entrySize {
		b.entries = append(b.entries, Entry{
			Key:    binary.BigEndian.Uint64(data[i : i+8]),
			Move:   binary.BigEndian.Uint16(data[i+8 : i+10]),
			Weight: binary.BigEndian.Uint16(data[i+10 : i+12]),
			Learn:  binary.BigEndian.Uint32(data[i+12 : i+16]),
		})
	}
	return b, nil
}

// Len reports how many entries (positions × candidate moves — not
// distinct positions) are in the book. Mainly useful so a caller that
// just loaded a book (e.g. the wasm API's loadBook) can report back
// "loaded, N entries" rather than a bare boolean.
func (b *Book) Len() int {
	return len(b.entries)
}

// Lookup returns every book entry recorded for a given Zobrist hash.
// Most callers want LookupBoard instead; this lower-level form is kept
// for testing against known hashes without needing a *board.Board.
func (b *Book) Lookup(zobristKey uint64) []Entry {
	var found []Entry
	for _, e := range b.entries {
		if e.Key == zobristKey {
			found = append(found, e)
		}
	}
	return found
}

// LookupBoard returns every book entry recorded for a position.
func (b *Book) LookupBoard(pos *board.Board) []Entry {
	return b.Lookup(pos.Hash())
}

// polyglotPromotion maps Polyglot's 3-bit promotion code to a
// PieceType. 0 means "not a promotion" and has no entry here — callers
// check bits != 0 first (see decodeRaw).
var polyglotPromotion = map[uint16]board.PieceType{
	1: board.Knight,
	2: board.Bishop,
	3: board.Rook,
	4: board.Queen,
}

// decodeRaw unpacks a Polyglot move's from/to squares and promotion
// piece from its 16-bit encoding:
//
//	bits 0-2:   to file
//	bits 3-5:   to rank
//	bits 6-8:   from file
//	bits 9-11:  from rank
//	bits 12-14: promotion piece (0 = none, 1..4 = N/B/R/Q)
//
// Squares decode directly into this engine's Square numbering (file +
// 8*rank, 0 = a1) with no translation needed — Polyglot uses the same
// convention.
func decodeRaw(raw uint16) (from, to board.Square, promotion board.PieceType) {
	toFile := int(raw & 0x7)
	toRank := int((raw >> 3) & 0x7)
	fromFile := int((raw >> 6) & 0x7)
	fromRank := int((raw >> 9) & 0x7)
	promoBits := (raw >> 12) & 0x7

	from = board.MakeSquare(fromFile, fromRank)
	to = board.MakeSquare(toFile, toRank)
	promotion = polyglotPromotion[promoBits] // NoType (zero value) if promoBits == 0
	return
}

// normalizeCastling corrects Polyglot's "king takes own rook"
// encoding for castling moves. Polyglot represents O-O / O-O-O as the
// king's from-square and the *rook's* home square as the target
// (e.g. white kingside castling is encoded as e1h1, not e1g1) — a
// convention it shares with UCI_Chess960 engines. This engine's own
// Move/movegen use the normal king destination (g1/c1/g8/c8), so
// entries matching this exact pattern get their target square
// corrected before being matched against the legal move list.
func normalizeCastling(from, to board.Square) board.Square {
	switch [2]board.Square{from, to} {
	case [2]board.Square{board.MakeSquare(4, 0), board.MakeSquare(7, 0)}: // e1h1
		return board.MakeSquare(6, 0) // g1
	case [2]board.Square{board.MakeSquare(4, 0), board.MakeSquare(0, 0)}: // e1a1
		return board.MakeSquare(2, 0) // c1
	case [2]board.Square{board.MakeSquare(4, 7), board.MakeSquare(7, 7)}: // e8h8
		return board.MakeSquare(6, 7) // g8
	case [2]board.Square{board.MakeSquare(4, 7), board.MakeSquare(0, 7)}: // e8a8
		return board.MakeSquare(2, 7) // c8
	default:
		return to
	}
}

// DecodeMove translates a book Entry's packed move into this engine's
// board.Move. It does this by decoding the raw (from, to, promotion)
// triple and then matching it against legal, the position's legal
// move list — rather than re-deriving Move.Flag (capture / en
// passant / castling / promotion) by hand. Reusing movegen's already
// perft-tested output for that is both less code and less risk than
// a second, parallel implementation of the same flag logic here.
//
// Returns ok == false if the decoded squares don't correspond to any
// move in legal — which would mean either a corrupt/foreign book
// entry, or (for a well-formed book) simply the wrong position.
func DecodeMove(e Entry, legal []board.Move) (board.Move, bool) {
	from, to, promotion := decodeRaw(e.Move)
	to = normalizeCastling(from, to)

	for _, m := range legal {
		if m.From != from || m.To != to {
			continue
		}
		isPromo := m.Flag == board.Promotion || m.Flag == board.PromotionCapture
		if isPromo != (promotion != board.NoType) {
			continue
		}
		if isPromo && m.Promotion != promotion {
			continue
		}
		return m, true
	}
	return board.Move{}, false
}

// New builds a Book directly from a slice of entries, bypassing Load.
// Mainly useful for tests and for programmatically assembling a book
// (e.g. merging several loaded books) without going through a .bin
// file on disk.
func New(entries []Entry) *Book {
	return &Book{entries: entries}
}
