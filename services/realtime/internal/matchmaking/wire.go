package matchmaking

import "encoding/json"

// Client responses must never carry the internal two-phase pairing
// reservation ("pairing" + half-created room) or the one-time cancel secret.
// Historical bug (2026-10-03): handlers embedded raw Ticket values with only
// a cancel-secret strip, so a poll landing inside the reserve -> cross-service
// CreateMatch -> promote window serialized status "pairing" to the browser;
// the client stopped polling (non-queued) and never navigated (not matched),
// parking players on "Matched - opening game..." forever.
//
// WireTicket is the structural fix: it embeds the internal Ticket with
// MarshalJSON overridden to project through PublicView. Handler code can no
// longer serialize a raw Ticket by mistake -- handlers must wrap, and the
// projection is enforced at marshal time even if a future call site forgets.

// WireTicket is a ticket shaped for a client-facing JSON response. Marshal it
// (directly, or inside a map/slice of response fields) and the projection is
// applied automatically; there is deliberately no way to marshal the embedded
// raw state.
type WireTicket struct {
	Ticket
}

func (w WireTicket) MarshalJSON() ([]byte, error) {
	return json.Marshal(w.Ticket.PublicView())
}

func (w *WireTicket) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &w.Ticket)
}
