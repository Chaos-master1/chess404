package match

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// WP6 gate: the shared card catalog is generated once by
// packages/game-core/scripts/sync-cards-json.mjs (src/cards.json -> this
// package's cards.json, which is //go:embed'ed). These tests pin the three
// invariants that keep client and server card definitions from drifting:
//
//  1. every template carries complete metadata (ids unique, mechanics unique),
//  2. the mechanic set exactly matches the CardMechanic union in
//     packages/contracts/src/index.ts (the type the whole web client renders
//     against), and
//  3. every mechanic in the catalog is dispatched somewhere in the server's
//     play/target code (applyPlayCard + applySelectTarget), so a card can
//     never exist in the deck without executable rules behind it.
//
// A mechanic present in Go but missing from the TS union makes the client
// render garbage; one present in the catalog but missing from Go dispatch
// plays as "card vanishes, nothing happens". Both drifted silently before
// this gate existed.

// expectedMechanics mirrors the CardMechanic union in
// packages/contracts/src/index.ts -- and is verified against that file by
// TestCardCatalogMatchesTypeScriptUnion, so updating the union REQUIRES
// updating this slice (and vice versa) in the same commit.
var expectedMechanics = []string{
	"halffuse", "fullfusion", "swapme", "swapus", "swaphim",
	"sniper", "badsniper", "promote", "demote", "shield",
	"doublemove_diff", "doublemove_same", "fortress", "fog_village",
	"freeze", "jump", "teleport", "clone", "demotehim", "promotehim",
	"borrow", "mindcontrol", "parasite", "lavaground", "blackhole",
	"fakepiece", "gambler", "bigsacrifice", "smallsacrifice",
	"mirror", "radar", "undo", "joker", "reverse", "cheater", "unabomber", "invisible",
}

func TestCardCatalogMetadataComplete(t *testing.T) {
	cards := getStarterCards()
	if len(cards) == 0 {
		t.Fatal("card catalog is empty")
	}
	ids := map[string]int{}
	mechanics := map[string]int{}
	for _, card := range cards {
		if card.ID == "" {
			t.Errorf("card %q (%s) has empty ID", card.Name, card.Mechanic)
		}
		ids[card.ID]++
		mechanics[card.Mechanic]++
		if card.Name == "" || card.Desc == "" || card.Color == "" || card.Accent == "" || card.Icon == "" {
			t.Errorf("card %q (id %q) is missing rendering metadata (name/desc/color/accent/icon)", card.Name, card.ID)
		}
		switch card.Type {
		case "spell", "trap":
		default:
			t.Errorf("card %q has unknown type %q", card.ID, card.Type)
		}
		switch card.Rarity {
		case "trash", "common", "rare", "epic", "legendary":
		default:
			t.Errorf("card %q has unknown rarity %q", card.ID, card.Rarity)
		}
	}
	for id, count := range ids {
		if count > 1 {
			t.Errorf("card id %q appears %d times in the catalog", id, count)
		}
	}
	for mechanic, count := range mechanics {
		if count > 1 {
			t.Errorf("mechanic %q appears on %d cards -- templates must keep one mechanic per card", mechanic, count)
		}
	}
}

func tsCardMechanicUnion(t *testing.T) map[string]bool {
	t.Helper()
	src, err := os.ReadFile("../../../../packages/contracts/src/index.ts")
	if err != nil {
		t.Fatalf("cannot read TS contracts source: %v", err)
	}
	blockRe := regexp.MustCompile(`export type CardMechanic =[\s\S]*?;`)
	block := blockRe.FindString(string(src))
	if block == "" {
		t.Fatal("export type CardMechanic not found in packages/contracts/src/index.ts")
	}
	idRe := regexp.MustCompile(`'([a-z_]+)'`)
	members := map[string]bool{}
	for _, match := range idRe.FindAllStringSubmatch(block, -1) {
		members[match[1]] = true
	}
	if len(members) == 0 {
		t.Fatal("parsed zero members from the CardMechanic union -- the extraction regex no longer matches the file layout")
	}
	return members
}

func TestCardCatalogMatchesTypeScriptUnion(t *testing.T) {
	tsUnion := tsCardMechanicUnion(t)

	expected := map[string]bool{}
	for _, mechanic := range expectedMechanics {
		expected[mechanic] = true
		if !tsUnion[mechanic] {
			t.Errorf("expected mechanic %q is missing from the CardMechanic union in packages/contracts/src/index.ts", mechanic)
		}
	}
	for member := range tsUnion {
		if !expected[member] {
			t.Errorf("CardMechanic union member %q is not in expectedMechanics (cards_parity_test.go) -- add it there and wire it server-side", member)
		}
	}
	for _, card := range getStarterCards() {
		if !tsUnion[card.Mechanic] {
			t.Errorf("catalog card %q uses mechanic %q which the TypeScript CardMechanic union does not declare", card.ID, card.Mechanic)
		}
	}
}

func TestCardCatalogIsFullyWiredServerSide(t *testing.T) {
	dispatch := readMatchGoSources(t)
	for _, card := range getStarterCards() {
		if !strings.Contains(dispatch, `"`+card.Mechanic+`"`) {
			t.Errorf("mechanic %q (card %q) is never referenced by name in the match package's dispatch code (applyPlayCard / applySelectTarget / cards_mechanics) -- the card exists in the deck but has no rules", card.Mechanic, card.ID)
		}
		if got := cardTemplateByMechanic(card.Mechanic); got.Mechanic == "" {
			t.Errorf("cardTemplateByMechanic(%q) returns the zero card -- hand/deal code would ship a blank template", card.Mechanic)
		}
	}
}

func readMatchGoSources(t *testing.T) string {
	t.Helper()
	files := []string{"cards_play.go", "cards_target_select.go", "cards_mechanics.go"}
	var combined strings.Builder
	for _, name := range files {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("cannot read dispatch source %s: %v", name, err)
		}
		combined.Write(data)
		combined.WriteString("\n")
	}
	return combined.String()
}

// The Go catalog is //go:embed'ed from the same cards.json the client's
// card-pool imports. Belt and braces: verify the two catalog files on disk
// are actually identical so a stale copy cannot hide behind a passing
// metadata test.
func TestEmbeddedCardCatalogMatchesGeneratedSource(t *testing.T) {
	src, err := os.ReadFile("../../../../packages/game-core/src/cards.json")
	if err != nil {
		t.Fatalf("cannot read generated card catalog: %v", err)
	}
	dst, err := os.ReadFile("cards.json")
	if err != nil {
		t.Fatalf("cannot read embedded card catalog: %v", err)
	}
	if string(src) != string(dst) {
		t.Fatal("services/realtime/internal/match/cards.json differs from packages/game-core/src/cards.json -- run packages/game-core/scripts/sync-cards-json.mjs")
	}
	var catalog []map[string]any
	if err := json.Unmarshal(dst, &catalog); err != nil {
		t.Fatalf("embedded catalog is not valid JSON: %v", err)
	}
	if len(catalog) != len(getStarterCards()) {
		t.Fatalf("embedded catalog has %d cards but getStarterCards returns %d", len(catalog), len(getStarterCards()))
	}
}
