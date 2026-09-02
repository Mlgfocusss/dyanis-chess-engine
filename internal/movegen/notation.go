// Standard Algebraic Notation (SAN): the "Nf3", "exd5", "O-O", "e8=Q#"
// format players actually read and write, as opposed to board.Move's
// own String() ("g1f3", "e7d5", ...), which is coordinate notation —
// simpler to generate and to match against user input character-for-
// character, which is why it's what Move.String(), the book decoder,
// and the rest of the engine use internally. SAN is a presentation/
// input concern layered on top, not a replacement for it.
package movegen

import (
	"fmt"
	"strings"

	"github.com/yourname/dyanis-chess-engine/internal/board"
)

var sanPieceLetter = map[board.PieceType]byte{
	board.Knight: 'N', board.Bishop: 'B', board.Rook: 'R', board.Queen: 'Q', board.King: 'K',
}

var sanPieceFromLetter = map[byte]board.PieceType{
	'N': board.Knight, 'B': board.Bishop, 'R': board.Rook, 'Q': board.Queen, 'K': board.King,
}

// SAN renders m, played from position before, in Standard Algebraic
// Notation, including the trailing "+" or "#" for check/checkmate.
// before must be the position the move is played FROM (not after) —
// SAN needs it both to know which piece is moving (Move itself only
// stores squares) and to compute disambiguation against every other
// legal move in the same position.
func SAN(before *board.Board, m board.Move) string {
	var body string
	switch m.Flag {
	case board.CastleKingside:
		body = "O-O"
	case board.CastleQueenside:
		body = "O-O-O"
	default:
		body = sanBody(before, m)
	}
	return body + checkSuffix(before, m)
}

func sanBody(before *board.Board, m board.Move) string {
	pieceType := before.PieceAt(m.From).Type()
	isCapture := m.IsCapture()

	var sb strings.Builder

	if pieceType == board.Pawn {
		// Pawns never get a letter prefix; captures are marked by the
		// file they moved FROM (e.g. "exd5"), not a piece letter.
		if isCapture {
			sb.WriteByte("abcdefgh"[m.From.File()])
			sb.WriteByte('x')
		}
		sb.WriteString(m.To.String())
		if m.Flag == board.Promotion || m.Flag == board.PromotionCapture {
			sb.WriteByte('=')
			sb.WriteByte(sanPieceLetter[m.Promotion])
		}
		return sb.String()
	}

	sb.WriteByte(sanPieceLetter[pieceType])
	sb.WriteString(disambiguate(before, m, pieceType))
	if isCapture {
		sb.WriteByte('x')
	}
	sb.WriteString(m.To.String())
	return sb.String()
}

// disambiguate returns the minimal prefix needed to distinguish m from
// every other legal move of the same piece type landing on the same
// square: nothing if m is the only one, the origin file if that alone
// is unique among the candidates, the origin rank if the file isn't
// unique but the rank is, or the full origin square if neither is
// (e.g. two knights sharing both a file and a rank with a third).
func disambiguate(before *board.Board, m board.Move, pieceType board.PieceType) string {
	legal := GenerateLegalMoves(before)

	var others []board.Square
	for _, lm := range legal {
		if lm.To != m.To || lm.From == m.From {
			continue
		}
		if before.PieceAt(lm.From).Type() != pieceType {
			continue
		}
		others = append(others, lm.From)
	}
	if len(others) == 0 {
		return ""
	}

	fileUnique := true
	for _, o := range others {
		if o.File() == m.From.File() {
			fileUnique = false
			break
		}
	}
	if fileUnique {
		return string("abcdefgh"[m.From.File()])
	}

	rankUnique := true
	for _, o := range others {
		if o.Rank() == m.From.Rank() {
			rankUnique = false
			break
		}
	}
	if rankUnique {
		return string("12345678"[m.From.Rank()])
	}

	return m.From.String()
}

// checkSuffix plays m on a copy of before and reports "#" if that
// checkmates the opponent, "+" if it merely checks them, or "" if
// neither — reusing GameStatus/InCheck rather than re-deriving check
// detection, since those are already the perft-verified source of
// truth for "is this king attacked / is this checkmate".
func checkSuffix(before *board.Board, m board.Move) string {
	after := before.MakeMove(m)
	switch GameStatus(after) {
	case Checkmate:
		return "#"
	case Ongoing:
		if InCheck(after) {
			return "+"
		}
	}
	return ""
}

// GameLog formats a sequence of already-played SAN moves (in order,
// starting with White's first move) into standard movetext, e.g.
// "1. e4 c5 2. Nf3 Nc6 3. Bb5". Works with an odd-length slice too
// (a game currently paused right after White's move).
func GameLog(sanMoves []string) string {
	var sb strings.Builder
	for i, m := range sanMoves {
		if i%2 == 0 {
			if i > 0 {
				sb.WriteByte(' ')
			}
			fmt.Fprintf(&sb, "%d. ", i/2+1)
		} else {
			sb.WriteByte(' ')
		}
		sb.WriteString(m)
	}
	return sb.String()
}

// ParseSAN parses algebraic notation ("Nf3", "exd5", "O-O", "e8=Q",
// with or without a trailing "+"/"#"/"!"/"?") against the legal moves
// available in b, returning the matching board.Move. It works by
// narrowing b's legal move list down using whatever the notation
// specifies — piece type, destination square, disambiguating file/
// rank, promotion piece — rather than fully re-deriving a move from
// scratch, so it can only ever return moves movegen itself already
// considers legal.
func ParseSAN(b *board.Board, input string) (board.Move, error) {
	s := strings.TrimSpace(input)
	for len(s) > 0 {
		last := s[len(s)-1]
		if last == '+' || last == '#' || last == '!' || last == '?' {
			s = s[:len(s)-1]
			continue
		}
		break
	}
	if s == "" {
		return board.Move{}, fmt.Errorf("empty move")
	}

	legal := GenerateLegalMoves(b)

	// Castling: accept both "O" (letter) and "0" (digit), both seen
	// in the wild.
	switch strings.ReplaceAll(strings.ToUpper(s), "0", "O") {
	case "O-O-O":
		return findByFlag(legal, board.CastleQueenside)
	case "O-O":
		return findByFlag(legal, board.CastleKingside)
	}

	pieceType := board.Pawn
	rest := s
	if pt, ok := sanPieceFromLetter[s[0]]; ok {
		pieceType = pt
		rest = s[1:]
	}

	promotion := board.NoType
	if idx := strings.IndexByte(rest, '='); idx != -1 {
		if idx+1 >= len(rest) {
			return board.Move{}, fmt.Errorf("invalid promotion in move %q", input)
		}
		pt, ok := sanPieceFromLetter[rest[idx+1]]
		if !ok {
			return board.Move{}, fmt.Errorf("invalid promotion piece in move %q", input)
		}
		promotion = pt
		rest = rest[:idx]
	}

	rest = strings.ReplaceAll(rest, "x", "")
	if len(rest) < 2 {
		return board.Move{}, fmt.Errorf("can't parse move %q", input)
	}

	to, err := board.ParseSquare(rest[len(rest)-2:])
	if err != nil {
		return board.Move{}, fmt.Errorf("can't parse destination square in move %q: %w", input, err)
	}

	fileConstraint, rankConstraint := -1, -1
	for _, ch := range rest[:len(rest)-2] {
		switch {
		case ch >= 'a' && ch <= 'h':
			fileConstraint = int(ch - 'a')
		case ch >= '1' && ch <= '8':
			rankConstraint = int(ch - '1')
		default:
			return board.Move{}, fmt.Errorf("can't parse move %q", input)
		}
	}

	var candidates []board.Move
	for _, m := range legal {
		if m.To != to {
			continue
		}
		if b.PieceAt(m.From).Type() != pieceType {
			continue
		}
		if fileConstraint != -1 && m.From.File() != fileConstraint {
			continue
		}
		if rankConstraint != -1 && m.From.Rank() != rankConstraint {
			continue
		}
		candidates = append(candidates, m)
	}

	if promotion != board.NoType {
		var filtered []board.Move
		for _, m := range candidates {
			if m.Promotion == promotion {
				filtered = append(filtered, m)
			}
		}
		candidates = filtered
	}

	switch len(candidates) {
	case 0:
		return board.Move{}, fmt.Errorf("no legal move matches %q", input)
	case 1:
		return candidates[0], nil
	default:
		// The only case multiple candidates should reach here with an
		// otherwise fully-specified move is an omitted promotion
		// suffix ("b8" instead of "b8=Q"): default to queen, the
		// overwhelmingly common choice, rather than force "=Q" every
		// time.
		if promotion == board.NoType {
			for _, m := range candidates {
				if m.Promotion == board.Queen {
					return m, nil
				}
			}
		}
		return board.Move{}, fmt.Errorf("move %q is ambiguous", input)
	}
}

func findByFlag(legal []board.Move, flag board.MoveFlag) (board.Move, error) {
	for _, m := range legal {
		if m.Flag == flag {
			return m, nil
		}
	}
	return board.Move{}, fmt.Errorf("castling isn't currently legal")
}
