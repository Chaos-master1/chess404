package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/chess404/realtime/internal/contracts"
	"github.com/chess404/realtime/internal/engine"
)

func main() {
	// Self-play training mode
	selfplayGames := flag.Int("selfplay", 0, "Run N self-play games and export training data")
	selfplayOutput := flag.String("output", "training_data.json", "Output file for training data")
	selfplayTempInitial := flag.Float64("temp-init", 0.5, "Initial exploration temperature")
	selfplayTempFinal := flag.Float64("temp-final", 0.05, "Final exploration temperature")
	selfplayTime := flag.Int("time-per-move", 100, "Time per move in milliseconds")
	selfplayDepth := flag.Int("depth", 32, "Search depth")
	useMCTS := flag.Bool("mcts", false, "Use MCTS instead of alpha-beta search")
	mctsSims := flag.Int("mcts-sims", 800, "MCTS simulations per move")
	genPositions := flag.Int("gen-positions", 0, "Generate N random positions with classical eval (for NNUE training)")
	dashboard := flag.Bool("dashboard", false, "Start with live thinking dashboard")
	dashPort := flag.Int("dashboard-port", 8765, "Dashboard WebSocket port")
	threads := flag.Int("threads", 1, "Number of search threads (Lazy SMP)")
	flag.Parse()

	if *dashboard {
		engine.StartDashboard(*dashPort)
		fmt.Println("Dashboard started on port", *dashPort)
	}

	if *genPositions > 0 {
		positions := engine.GenRandomPositions(*genPositions)
		ds := &engine.TrainingDataSet{
			Games: []engine.SelfPlayResult{{
				GameNum:   1,
				PlyCount:  *genPositions,
				Result:    "*",
				Positions: positions,
			}},
		}
		if err := ds.ExportJSON("training_data.json"); err != nil {
			fmt.Fprintf(os.Stderr, "Error exporting: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Generated %d positions with classical eval → training_data.json\n", *genPositions)
		return
	}

	if *selfplayGames > 0 {
		runSelfPlay(*selfplayGames, *selfplayOutput, *selfplayTempInitial, *selfplayTempFinal, *selfplayTime, *selfplayDepth, *threads)
		return
	}

	// UCI mode (default)
	fmt.Println("404chess-engine v1.0 by chess404")
	scanner := bufio.NewScanner(os.Stdin)

	var tt *engine.TranspositionTable
	var currentState *contracts.MatchState

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		cmd := parts[0]

		switch cmd {
		case "uci":
			fmt.Println("id name 404chess-engine")
			fmt.Println("id author chess404")
			fmt.Println("uciok")

		case "isready":
			if tt == nil {
				tt = engine.NewTranspositionTable(1 << 20)
			}
			fmt.Println("readyok")

		case "position":
			if len(parts) < 2 {
				continue
			}
			var fen string
			moveStart := len(parts)

			if parts[1] == "startpos" {
				fen = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"
				moveStart = 2
			} else if parts[1] == "fen" && len(parts) >= 7 {
				fen = strings.Join(parts[2:8], " ")
				moveStart = 8
			}

			currentState = engine.MatchStateFromFEN(fen)
			if currentState == nil {
				fmt.Printf("info string error: failed to parse FEN: %s\n", fen)
				continue
			}

			// Apply moves
			for i := moveStart; i < len(parts); i++ {
				if parts[i] == "moves" {
					continue
				}
				move := parseUCIMove(parts[i])
				if move == nil {
					break
				}
				currentState = engine.ApplyMoveCopy(currentState, move)
			}

		case "go":
			if currentState == nil {
				continue
			}
			if tt == nil {
				tt = engine.NewTranspositionTable(1 << 20)
			}

			isWhite := currentState.Turn == "white"
			moves := engine.GenerateAllMoves(currentState, isWhite)
			if len(moves) == 0 {
				if engine.IsKingInCheck(currentState) {
					fmt.Println("info string checkmate")
				} else {
					fmt.Println("info string stalemate")
				}
				continue
			}

			var result engine.SearchResult
			if *useMCTS {
				mctsEngine := engine.NewMCTSEngine()
				mctsEngine.Config.Simulations = *mctsSims
				result = mctsEngine.FindBestMove(currentState)
				fmt.Printf("info string mcts sims=%d score=%d\n", result.Nodes, result.Score)
			} else {
				depth := 4
				timePerMove := 5000
				for i := 1; i < len(parts); i++ {
					if parts[i] == "depth" && i+1 < len(parts) {
						d, err := strconv.Atoi(parts[i+1])
						if err == nil {
							depth = d
						}
					}
					if parts[i] == "movetime" && i+1 < len(parts) {
						t, err := strconv.Atoi(parts[i+1])
						if err == nil {
							timePerMove = t
						}
					}
				}
				if *threads > 1 {
					result = engine.ParallelSearch(currentState, depth, tt, time.Duration(timePerMove)*time.Millisecond, *threads)
				} else {
					result = engine.SearchWithTime(currentState, depth, tt, time.Duration(timePerMove)*time.Millisecond)
				}
				fmt.Printf("info depth %d score cp %d nodes %d\n", depth, result.Score, result.Nodes)
			}
			if result.BestMove.From.Row == 0 && result.BestMove.From.Col == 0 &&
				result.BestMove.To.Row == 0 && result.BestMove.To.Col == 0 {
				result.BestMove = moves[0]
			}
			fmt.Printf("bestmove %s\n", engine.MoveToUCI(&result.BestMove))

		case "perft":
			if currentState == nil || len(parts) < 2 {
				continue
			}
			depth, err := strconv.Atoi(parts[1])
			if err != nil {
				continue
			}
			divide := engine.PerftDivide(currentState, depth)
			total := 0
			for move, count := range divide {
				fmt.Printf("%s: %d\n", move, count)
				total += count
			}
			fmt.Printf("\nTotal: %d\n", total)

		case "print":
			if currentState != nil {
				fmt.Println(engine.BoardToSimpleFEN(currentState))
			}

		case "quit", "exit":
			return
		}
	}
}

func parseUCIMove(uci string) *engine.Move {
	if len(uci) < 4 {
		return nil
	}
	fromCol := int(uci[0] - 'a')
	fromRow := int(uci[1] - '1')
	toCol := int(uci[2] - 'a')
	toRow := int(uci[3] - '1')
	m := &engine.Move{
		From: contracts.Square{Row: fromRow, Col: fromCol},
		To:   contracts.Square{Row: toRow, Col: toCol},
	}
	if len(uci) == 5 {
		switch uci[4] {
		case 'q':
			m.Promotion = "queen"
		case 'r':
			m.Promotion = "rook"
		case 'b':
			m.Promotion = "bishop"
		case 'n':
			m.Promotion = "knight"
		}
	}
	return m
}

func runSelfPlay(games int, output string, tempInit, tempFinal float64, timePerMoveMs, depth int, threads int) {
	cfg := engine.DefaultSelfPlayConfig
	cfg.Games = games
	cfg.InitialTemperature = tempInit
	cfg.FinalTemperature = tempFinal
	cfg.TimePerMove = time.Duration(timePerMoveMs) * time.Millisecond
	cfg.SearchDepth = depth
	cfg.NumThreads = threads

	fmt.Printf("Starting self-play: %d games, temp %.2f→%.2f, %dms/move, depth %d\n",
		games, tempInit, tempFinal, timePerMoveMs, depth)

	results := engine.RunSelfPlay(cfg)

	totalPly := 0
	wins := 0
	losses := 0
	draws := 0
	totalPositions := 0
	for _, r := range results {
		totalPly += r.PlyCount
		totalPositions += len(r.Positions)
		switch r.Result {
		case "1-0":
			wins++
		case "0-1":
			losses++
		default:
			draws++
		}
	}

	fmt.Printf("Games: %d | White wins: %d | Black wins: %d | Draws: %d\n", games, wins, losses, draws)
	fmt.Printf("Avg ply: %.1f | Total positions: %d\n", float64(totalPly)/float64(games), totalPositions)

	ds := &engine.TrainingDataSet{
		Config: cfg,
		Games:  results,
	}
	if err := ds.ExportJSON(output); err != nil {
		fmt.Fprintf(os.Stderr, "Error exporting training data: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Training data exported to %s\n", output)
}
