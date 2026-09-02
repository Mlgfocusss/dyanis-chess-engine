package board

import (
	"fmt"
	"strconv"
	"strings"
)

// StartFEN is the standard starting position in FEN notation.
const StartFEN = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"

var fenPieceLetters = map[rune]Piece{
	'P': WP, 'N': WN, 'B': WB, 'R': WR, 'Q': WQ, 'K': WK,
	'p': BP, 'n': BN, 'b': BB, 'r': BR, 'q': BQ, 'k': BK,
}

// FromFEN parses a FEN string into a Board. FEN is the standard
// notation for describing a chess position (used everywhere: UCI,
// opening books, test suites), so this doubles as the format used to
// set up arbitrary test positions such as Kiwipete for perft testing.
func FromFEN(fen string) (*Board, error) {
	fields := strings.Fields(fen)
	if len(fields) < 4 {
		return nil, fmt.Errorf("invalid FEN, expected at least 4 fields: %q", fen)
	}

	b := &Board{EnPassant: NoSquare}

	// 1. Piece placement, ranks 8 down to 1, separated by '/'.
	ranks := strings.Split(fields[0], "/")
	if len(ranks) != 8 {
		return nil, fmt.Errorf("invalid FEN piece placement: %q", fields[0])
	}
	for i, rankStr := range ranks {
		rank := 7 - i
		file := 0
		for _, ch := range rankStr {
			if ch >= '1' && ch <= '8' {
				file += int(ch - '0')
				continue
			}
			piece, ok := fenPieceLetters[ch]
			if !ok {
				return nil, fmt.Errorf("invalid FEN piece letter: %q", string(ch))
			}
			if file > 7 {
				return nil, fmt.Errorf("invalid FEN rank (too long): %q", rankStr)
			}
			b.Squares[MakeSquare(file, rank)] = piece
			file++
		}
	}

	// 2. Side to move.
	switch fields[1] {
	case "w":
		b.SideToMove = White
	case "b":
		b.SideToMove = Black
	default:
		return nil, fmt.Errorf("invalid FEN side to move: %q", fields[1])
	}

	// 3. Castling rights.
	if fields[2] != "-" {
		for _, ch := range fields[2] {
			switch ch {
			case 'K':
				b.Castling |= WhiteKingside
			case 'Q':
				b.Castling |= WhiteQueenside
			case 'k':
				b.Castling |= BlackKingside
			case 'q':
				b.Castling |= BlackQueenside
			default:
				return nil, fmt.Errorf("invalid FEN castling field: %q", fields[2])
			}
		}
	}

	// 4. En passant target square.
	if fields[3] != "-" {
		sq, err := ParseSquare(fields[3])
		if err != nil {
			return nil, fmt.Errorf("invalid FEN en passant field: %w", err)
		}
		b.EnPassant = sq
	}

	// 5 & 6. Halfmove clock and fullmove number are optional in some
	// abbreviated FENs; default sensibly if missing.
	b.HalfmoveClock = 0
	b.FullmoveNumber = 1
	if len(fields) >= 5 {
		if n, err := strconv.Atoi(fields[4]); err == nil {
			b.HalfmoveClock = n
		}
	}
	if len(fields) >= 6 {
		if n, err := strconv.Atoi(fields[5]); err == nil {
			b.FullmoveNumber = n
		}
	}

	return b, nil
}

// ToFEN serializes the board back into FEN notation.
func (b *Board) ToFEN() string {
	var sb strings.Builder

	for rank := 7; rank >= 0; rank-- {
		empty := 0
		for file := 0; file < 8; file++ {
			p := b.Squares[MakeSquare(file, rank)]
			if p.IsNone() {
				empty++
				continue
			}
			if empty > 0 {
				sb.WriteString(strconv.Itoa(empty))
				empty = 0
			}
			sb.WriteString(p.String())
		}
		if empty > 0 {
			sb.WriteString(strconv.Itoa(empty))
		}
		if rank > 0 {
			sb.WriteString("/")
		}
	}

	sb.WriteString(" ")
	if b.SideToMove == White {
		sb.WriteString("w")
	} else {
		sb.WriteString("b")
	}

	sb.WriteString(" ")
	if b.Castling == 0 {
		sb.WriteString("-")
	} else {
		if b.Castling&WhiteKingside != 0 {
			sb.WriteString("K")
		}
		if b.Castling&WhiteQueenside != 0 {
			sb.WriteString("Q")
		}
		if b.Castling&BlackKingside != 0 {
			sb.WriteString("k")
		}
		if b.Castling&BlackQueenside != 0 {
			sb.WriteString("q")
		}
	}

	sb.WriteString(" ")
	sb.WriteString(b.EnPassant.String())

	sb.WriteString(fmt.Sprintf(" %d %d", b.HalfmoveClock, b.FullmoveNumber))

	return sb.String()
}
