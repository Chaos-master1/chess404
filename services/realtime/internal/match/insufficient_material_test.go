package match

import (
	"testing"

	"github.com/chess404/realtime/internal/contracts"
)

func boardWithKingsAnd(t *testing.T, pieces ...struct {
	Sq    [2]int
	Color string
	Type  string
}) [][]*contracts.Piece {
	t.Helper()
	board := emptyBoard()
	board[0][4] = &contracts.Piece{Type: "king", Color: "white"}
	board[7][4] = &contracts.Piece{Type: "king", Color: "black"}
	for _, p := range pieces {
		board[p.Sq[0]][p.Sq[1]] = &contracts.Piece{Type: p.Type, Color: p.Color}
	}
	return board
}

// Regression: KNN vs K used to be declared a draw by insufficientMaterial
// even though the function's own comment said two knights are not a forced
// draw -- mate is possible against a cornered king. A winnable game must not
// be auto-drawn.
func TestInsufficientMaterialTwoKnightsNotDraw(t *testing.T) {
	board := boardWithKingsAnd(t, struct {
		Sq    [2]int
		Color string
		Type  string
	}{Sq: [2]int{4, 4}, Color: "white", Type: "knight"},
		struct {
			Sq    [2]int
			Color string
			Type  string
		}{Sq: [2]int{5, 5}, Color: "white", Type: "knight"})

	if insufficientMaterial(board) {
		t.Fatal("KNN vs K must not be declared insufficient material (mate is possible)")
	}
}

// KBB vs K with bishops on the same square color is a dead position.
func TestInsufficientMaterialSameColorBishopsDraw(t *testing.T) {
	// a1(0,0) and c1(0,2) are both dark squares.
	board := boardWithKingsAnd(t, struct {
		Sq    [2]int
		Color string
		Type  string
	}{Sq: [2]int{0, 0}, Color: "white", Type: "bishop"},
		struct {
			Sq    [2]int
			Color string
			Type  string
		}{Sq: [2]int{0, 2}, Color: "white", Type: "bishop"})

	if !insufficientMaterial(board) {
		t.Fatal("KBB vs K with same-colored bishops must be a draw")
	}
}

// Regression: KBB vs K with bishops on OPPOSITE square colors used to be
// auto-drawn because the old check ignored square colors entirely -- but
// opposite-colored bishops retain mating potential, so it is not dead.
func TestInsufficientMaterialOppositeColorBishopsNotDraw(t *testing.T) {
	// a1(0,0) is dark, b1(0,1) is light.
	board := boardWithKingsAnd(t, struct {
		Sq    [2]int
		Color string
		Type  string
	}{Sq: [2]int{0, 0}, Color: "white", Type: "bishop"},
		struct {
			Sq    [2]int
			Color string
			Type  string
		}{Sq: [2]int{0, 1}, Color: "white", Type: "bishop"})

	if insufficientMaterial(board) {
		t.Fatal("KBB vs K with opposite-colored bishops must not be declared a draw")
	}
}

// KB vs KB with both bishops on the same square color is dead regardless of
// which side owns which bishop.
func TestInsufficientMaterialOppositeSidesSameColorBishopsDraw(t *testing.T) {
	// a1(0,0) and b1(7,1)... row 7 col 1: (7+1)%2 == 0, both dark squares.
	board := boardWithKingsAnd(t, struct {
		Sq    [2]int
		Color string
		Type  string
	}{Sq: [2]int{0, 0}, Color: "white", Type: "bishop"},
		struct {
			Sq    [2]int
			Color string
			Type  string
		}{Sq: [2]int{7, 1}, Color: "black", Type: "bishop"})

	if !insufficientMaterial(board) {
		t.Fatal("KB vs KB with same-colored bishops must be a draw")
	}
}

// Bishop + knight combination is still winnable.
func TestInsufficientMaterialBishopKnightNotDraw(t *testing.T) {
	board := boardWithKingsAnd(t, struct {
		Sq    [2]int
		Color string
		Type  string
	}{Sq: [2]int{0, 0}, Color: "white", Type: "bishop"},
		struct {
			Sq    [2]int
			Color string
			Type  string
		}{Sq: [2]int{0, 1}, Color: "white", Type: "knight"})

	if insufficientMaterial(board) {
		t.Fatal("KBN vs K must not be declared insufficient material")
	}
}
