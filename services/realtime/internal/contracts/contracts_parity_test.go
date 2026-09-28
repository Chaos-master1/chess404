package contracts

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// WP6 gate: every JSON field the Go MatchState can ever serialize must have a
// counterpart in the TypeScript MatchState interface
// (packages/contracts/src/index.ts). Without this gate a new Go field sails
// through `go test` and only surfaces as silently-dropped client data -- or
// worse, a misspelled tag that never reaches the browser.
//
// The two PlayerSecret fields are the single deliberate exception: they are
// server-only state and must NEVER appear in the TS shape (they are redacted
// from every snapshot the server emits, but if someone accidentally
// serialized them, a TS field would make the leak look intentional and could
// even tempt client code to read it).
func tsMatchStateMembers(t *testing.T) map[string]bool {
	t.Helper()
	src, err := os.ReadFile("../../../../packages/contracts/src/index.ts")
	if err != nil {
		t.Fatalf("cannot read TS contracts source: %v", err)
	}
	start := strings.Index(string(src), "export interface MatchState")
	if start < 0 {
		t.Fatal("export interface MatchState not found in packages/contracts/src/index.ts")
	}
	// The interface block ends at the first closing brace at column 0.
	rest := string(src)[start:]
	end := strings.Index(rest, "\n}")
	if end < 0 {
		t.Fatal("MatchState interface block is unterminated")
	}
	body := rest[:end]
	memberRe := regexp.MustCompile(`(?m)^\s{2}([A-Za-z][A-Za-z0-9_]*)\??:`)
	members := map[string]bool{}
	for _, match := range memberRe.FindAllStringSubmatch(body, -1) {
		members[match[1]] = true
	}
	if len(members) == 0 {
		t.Fatal("parsed zero members from MatchState interface -- the extraction regex no longer matches the file layout")
	}
	return members
}

func jsonTagsOf(t *testing.T, v any) map[string]string {
	t.Helper()
	typ := reflect.TypeOf(v)
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	tags := map[string]string{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		raw := field.Tag.Get("json")
		if raw == "-" {
			continue
		}
		tag := strings.Split(raw, ",")[0]
		if tag == "" {
			// A field without a json tag serializes under the Go field name;
			// that is exactly the kind of accidental exposure this gate must
			// catch, so surface it under its Go name.
			tag = field.Name
		}
		tags[tag] = field.Name
	}
	return tags
}

func TestGoMatchStateFieldsHaveTypeScriptCounterparts(t *testing.T) {
	tsMembers := tsMatchStateMembers(t)
	goTags := jsonTagsOf(t, MatchState{})

	// Server-only state: must exist in Go and must NOT exist in TS.
	serverOnly := map[string]bool{
		"whitePlayerSecret": true,
		"blackPlayerSecret": true,
	}

	for tag, field := range goTags {
		if serverOnly[tag] {
			if tsMembers[tag] {
				t.Errorf("field %s (json %q) is server-only but is declared in the TypeScript MatchState -- remove it there", field, tag)
			}
			continue
		}
		if !tsMembers[tag] {
			t.Errorf("Go MatchState field %s has json tag %q but the TypeScript MatchState interface has no member %q -- add it to packages/contracts/src/index.ts", field, tag, tag)
		}
	}
	for member := range tsMembers {
		if serverOnly[member] {
			t.Errorf("TypeScript MatchState member %q is a server-only secret field and must never be declared client-side", member)
		}
	}
}

// Duplicate JSON tags inside one struct silently shadow each other's data.
func TestGoMatchStateHasNoDuplicateJSONTags(t *testing.T) {
	typ := reflect.TypeOf(MatchState{})
	seen := map[string]string{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := strings.Split(field.Tag.Get("json"), ",")[0]
		if tag == "-" {
			continue
		}
		if owner, dup := seen[tag]; dup {
			t.Errorf("fields %s and %s share json tag %q", owner, field.Name, tag)
		}
		seen[tag] = field.Name
	}
}
