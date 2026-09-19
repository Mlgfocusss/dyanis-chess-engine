// Magic bitboards for sliding-piece (bishop/rook/queen) attacks — the
// bitboard-side replacement for movegen.slidingAttack's ray walk,
// which was the single biggest chunk of the profile that motivated
// this whole migration (see TECHNICAL.md / the original profiling
// numbers: IsSquareAttacked at 28.57% cumulative, slidingAttack at
// 10.39% flat, on BestMove(depth=6) from the starting position).
//
// The idea: for a sliding piece on square s, only the OTHER pieces
// standing on the small set of squares between s and the board edge
// along s's rays can possibly change what s attacks (the "relevant
// occupancy"). rookMask/bishopMask below compute exactly that set —
// deliberately excluding the outermost edge square of every ray,
// since a piece sitting there blocks the ray either way, occupied or
// not, so it can never change the answer and including it would only
// waste mask bits.
//
// A magic number is a constant that, multiplied into "which of those
// relevant squares are actually occupied right now" and taken from
// the top bits of the 64-bit product, produces a collision-free index
// into a small precomputed table of attack bitboards — one entry per
// distinct occupancy pattern the mask can produce. That's what makes
// this O(1): no ray walk at query time, just a multiply, a shift, and
// an array read.
//
// Finding a magic number that happens to avoid collisions isn't
// something to derive analytically — it's normally found by random
// search, which is what happened here (a standalone Python script,
// not part of this engine, tried random sparse 64-bit numbers per
// square until one produced zero collisions across every occupancy
// subset of that square's mask). rookMagics/bishopMagics below are
// that search's output. Their correctness doesn't rest on trusting
// the search, though, for two independent reasons: (1) the search's
// own result was re-verified afterward by a second, independent
// program that exhaustively enumerated every occupancy subset for
// every square and confirmed zero collisions against every one of
// these 128 numbers; (2) everything that actually MATTERS for
// correctness at runtime — the masks, the real (ray-walked) attack
// bitboards used to fill each table, the indexing arithmetic — is
// implemented here, in Go, from first principles, and would produce
// visibly wrong attacks (caught by TestMagicAttacksMatchRayWalk / the
// existing perft suite) if any of the 128 numbers were actually bad.
// The numbers are just data; nothing about how they're used here
// assumes they're trustworthy a priori.
package board

// rookMask returns the relevant-occupancy mask for a rook on s: every
// square along its rank and file, excluding s itself and excluding
// the outermost square of each ray (see the package comment for why).
func rookMask(s Square) Bitboard {
	f, r := s.File(), s.Rank()
	var m Bitboard
	for rr := r + 1; rr <= 6; rr++ {
		m.Set(MakeSquare(f, rr))
	}
	for rr := r - 1; rr >= 1; rr-- {
		m.Set(MakeSquare(f, rr))
	}
	for ff := f + 1; ff <= 6; ff++ {
		m.Set(MakeSquare(ff, r))
	}
	for ff := f - 1; ff >= 1; ff-- {
		m.Set(MakeSquare(ff, r))
	}
	return m
}

// bishopMask is rookMask's diagonal counterpart.
func bishopMask(s Square) Bitboard {
	f, r := s.File(), s.Rank()
	var m Bitboard
	for _, d := range [4][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}} {
		rr, ff := r+d[0], f+d[1]
		for rr >= 1 && rr <= 6 && ff >= 1 && ff <= 6 {
			m.Set(MakeSquare(ff, rr))
			rr += d[0]
			ff += d[1]
		}
	}
	return m
}

// rookAttacksFromScratch ray-walks from s in the four rook directions
// against a REAL (unmasked) occupancy bitboard, stopping at and
// including the first occupied square in each direction — exactly
// what movegen.slidingAttack used to do one direction-check at a
// time, just returning the full destination set as a Bitboard instead
// of testing a single target square. Only used here, at init, to fill
// in every entry of each square's attack table; never on the query
// path (that's the whole point of the magic lookup below).
func rookAttacksFromScratch(s Square, occ Bitboard) Bitboard {
	f, r := s.File(), s.Rank()
	var a Bitboard
	for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		rr, ff := r+d[0], f+d[1]
		for onBoardFR(ff, rr) {
			to := MakeSquare(ff, rr)
			a.Set(to)
			if occ.Test(to) {
				break
			}
			rr += d[0]
			ff += d[1]
		}
	}
	return a
}

// bishopAttacksFromScratch is rookAttacksFromScratch's diagonal
// counterpart.
func bishopAttacksFromScratch(s Square, occ Bitboard) Bitboard {
	f, r := s.File(), s.Rank()
	var a Bitboard
	for _, d := range [4][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}} {
		rr, ff := r+d[0], f+d[1]
		for onBoardFR(ff, rr) {
			to := MakeSquare(ff, rr)
			a.Set(to)
			if occ.Test(to) {
				break
			}
			rr += d[0]
			ff += d[1]
		}
	}
	return a
}

// rookMagics / bishopMagics: see the package comment for what these
// are and how their correctness was checked. Found by random search
// over sparse 64-bit candidates (three random 64-bit numbers ANDed
// together, the usual trick to bias toward the few-bits-set numbers
// that tend to work as magics), one per square, requiring zero
// collisions across every occupancy subset of that square's mask.
var rookMagics = [64]uint64{
	0x00800220801A4001, 0x0140001000C06000, 0x2100090010402002, 0x0480040800801002,
	0x0200020044086110, 0x0300040002150008, 0x8480010002000080, 0x830002008640A100,
	0x70A0800020804000, 0x01A280200080400C, 0x0001001020004102, 0x2002000820120040,
	0x0086002048041200, 0x2004800E002C0080, 0x0001000100020004, 0x2261802250800100,
	0x8040818000400060, 0x0430104000200040, 0x0A12020040208011, 0x0000808010000804,
	0x0004008004080080, 0x0001010002080400, 0x2000010100020004, 0x808002001900A044,
	0x0480400880098064, 0x0010500040082000, 0x2020080040401000, 0x0220090100100020,
	0x4400180280240080, 0x0065840080800200, 0x0A8022040001D008, 0x02190402000C8061,
	0x0480002000404000, 0x2058400089002102, 0x0200110041002004, 0x6006420A02001020,
	0xC200040080800800, 0x4048200408011040, 0x0000011084000208, 0x0042908062000104,
	0x0000802040128000, 0x0890004020004000, 0x80A0010040210010, 0x2180220008420010,
	0x8130280100250010, 0x0084000810020200, 0x0000011002040008, 0x01052C80440A0021,
	0x1802401880022080, 0x00412000401000C0, 0x0400200010048880, 0x101022011088C200,
	0x0200040008008080, 0x0222800200040080, 0x0000107118020400, 0x0000006114840200,
	0x0008104021008202, 0x2485400211008821, 0x0000801200400822, 0x0008850008900121,
	0x00C2002008345092, 0x0102004108841002, 0x0080411000920814, 0x0000108034004102,
}

var bishopMagics = [64]uint64{
	0x0540020A24490100, 0x4208101400465214, 0x0A0906021A008191, 0x4004040880000010,
	0x8002021009000020, 0x0202011008310040, 0x0002008404420802, 0x102040C414202A0A,
	0x2C00084204044400, 0x0804205101122480, 0x2019041812104048, 0x2000480E81050400,
	0x0180120210808040, 0x8020020824040281, 0x1048060082201124, 0x0014002C04040410,
	0x0511000920086080, 0x0220001001410104, 0x0801093021002100, 0x0002001040104000,
	0x0001004820080002, 0x41C1024280600200, 0x0081000048225080, 0x0000241104020220,
	0xF042404032101200, 0x006C821030020810, 0x0E00404184090202, 0x04C0044024010020,
	0x9401010000104000, 0x0008020140412088, 0x8182040000A40100, 0x0204004000960088,
	0x0002082004842000, 0x000201200104480A, 0x00440090400A0400, 0x0001208020080200,
	0xC010020201002008, 0x0210040821011000, 0x0802009C18010404, 0x2401042580110050,
	0x0C22221044084000, 0x0002010160B24800, 0x1041420050004100, 0x0604084010430200,
	0x1140200204100880, 0xC240190403000321, 0x4031241820802240, 0x00100C0080880224,
	0x00CB00D044200000, 0x0481008250220A01, 0x0160004204D00080, 0x2810861484240620,
	0x0001001332020002, 0x0080412801050200, 0x0841080200820580, 0x8184840800410630,
	0x2084410088014018, 0x4010820202410400, 0x0001000104011482, 0x0008089004841105,
	0x4400002010020884, 0x003002C044085082, 0x2000400464242448, 0x000808A100440100,
}

var (
	rookMaskTable     [64]Bitboard
	bishopMaskTable   [64]Bitboard
	rookShiftTable    [64]uint
	bishopShiftTable  [64]uint
	rookAttackTable   [64][]Bitboard
	bishopAttackTable [64][]Bitboard
)

// buildMagicTable fills in every entry of one square's attack table
// by enumerating all 2^popcount(mask) subsets of mask (the standard
// "carry-rippler" trick: subset = (subset - mask) & mask, wrapping
// back to 0 after visiting every subset exactly once) and, for each,
// computing the real ray-walked attack set and storing it at the
// magic-computed index. Runs once per square at package init — never
// on the query path.
func buildMagicTable(mask Bitboard, magic uint64, shift uint, attacksFromScratch func(occ Bitboard) Bitboard) []Bitboard {
	table := make([]Bitboard, 1<<(64-shift))
	subset := Bitboard(0)
	for {
		idx := (uint64(subset) * magic) >> shift
		table[idx] = attacksFromScratch(subset)
		subset = (subset - mask) & mask
		if subset == 0 {
			break
		}
	}
	return table
}

func init() {
	for i := 0; i < 64; i++ {
		s := Square(i)

		rm := rookMask(s)
		rookMaskTable[i] = rm
		rookShiftTable[i] = uint(64 - rm.Count())
		rookAttackTable[i] = buildMagicTable(rm, rookMagics[i], rookShiftTable[i],
			func(occ Bitboard) Bitboard { return rookAttacksFromScratch(s, occ) })

		bm := bishopMask(s)
		bishopMaskTable[i] = bm
		bishopShiftTable[i] = uint(64 - bm.Count())
		bishopAttackTable[i] = buildMagicTable(bm, bishopMagics[i], bishopShiftTable[i],
			func(occ Bitboard) Bitboard { return bishopAttacksFromScratch(s, occ) })
	}
}

// RookAttacks returns the rook attack bitboard from sq given the full
// board occupancy (every piece, either color) — an O(1) magic lookup,
// replacing what used to be a ray walk in movegen.slidingAttack.
func RookAttacks(sq Square, occupied Bitboard) Bitboard {
	m := rookMaskTable[sq]
	idx := (uint64(occupied&m) * rookMagics[sq]) >> rookShiftTable[sq]
	return rookAttackTable[sq][idx]
}

// BishopAttacks is RookAttacks' diagonal counterpart.
func BishopAttacks(sq Square, occupied Bitboard) Bitboard {
	m := bishopMaskTable[sq]
	idx := (uint64(occupied&m) * bishopMagics[sq]) >> bishopShiftTable[sq]
	return bishopAttackTable[sq][idx]
}

// QueenAttacks is simply the union of the two — a queen attacks
// exactly what a rook and a bishop on the same square would attack,
// combined.
func QueenAttacks(sq Square, occupied Bitboard) Bitboard {
	return RookAttacks(sq, occupied) | BishopAttacks(sq, occupied)
}
