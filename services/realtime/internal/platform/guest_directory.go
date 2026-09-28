package platform

import "github.com/chess404/realtime/internal/contracts"

type GuestDirectory interface {
	Backend() string
	Close() error
	EnsureGuest(guestID, sessionSecret string) (GuestSession, error)
	IssueGuestSession(guestID string) (GuestSession, error)
	ResumeGuest(guestID, sessionSecret string) (GuestSession, error)
	ResumeGuestByToken(guestID, sessionToken string) (GuestSession, error)
	FinalizeMatch(matchID, whiteGuestID, blackGuestID, winner string, modeID contracts.MatchModeID) (GuestProfile, GuestProfile, bool, error)
	ListGuests(limit int) []GuestProfile
	GetGuest(guestID string) (GuestProfile, bool)
	RenameGuest(guestID, displayName string) (GuestProfile, error)
	ListRecentGuests(limit int) []GuestProfile
	Stats() GuestStoreStats
}
