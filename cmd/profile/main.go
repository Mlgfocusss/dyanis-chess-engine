// Throwaway profiling harness — not part of the engine. Runs
// BestMove(depth=6) from the starting position under Go's CPU
// profiler, the same workload TECHNICAL.md's original `pprof -top`
// numbers were taken from (IsSquareAttacked 28.57% cumulative,
// slidingAttack 10.39% flat) — run again after the array-to-bitboard
// migration to see whether those hot spots actually moved.
//
// Usage, from the module root:
//
//	go run ./cmd/profile
//	go tool pprof -top cpu.prof
//
// Add -depth=N to profile a different depth, e.g.:
//
//	go run ./cmd/profile -depth=7
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime/pprof"
	"time"

	"github.com/yourname/dyanis-chess-engine/internal/board"
	"github.com/yourname/dyanis-chess-engine/internal/search"
)

func main() {
	depth := flag.Int("depth", 6, "search depth (plies)")
	out := flag.String("out", "cpu.prof", "CPU profile output path")
	flag.Parse()

	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "creating profile output:", err)
		os.Exit(1)
	}
	defer f.Close()

	if err := pprof.StartCPUProfile(f); err != nil {
		fmt.Fprintln(os.Stderr, "starting CPU profile:", err)
		os.Exit(1)
	}
	defer pprof.StopCPUProfile()

	b := board.NewInitialBoard()
	var lastInfo search.SearchInfo
	start := time.Now()
	move, err := search.BestMoveInfo(b, *depth, func(info search.SearchInfo) {
		lastInfo = info
	})
	elapsed := time.Since(start)
	if err != nil {
		fmt.Fprintln(os.Stderr, "search error:", err)
		os.Exit(1)
	}

	fmt.Printf("BestMove(depth=%d) from the starting position: %s (%s)\n", *depth, move.String(), elapsed)
	fmt.Printf("nodes: %d  score: %d\n", lastInfo.Nodes, lastInfo.Score)
	fmt.Printf("profile written to %s — now run: go tool pprof -top %s\n", *out, *out)
}
