package main

import (
	"strings"
	"time"

	"github.com/chess404/realtime/internal/contracts"
	"github.com/chess404/realtime/internal/matchmaking"
	"github.com/chess404/realtime/internal/platform"
)

// Gateway DTO types and constants.

// maxBodySize is the gateway-wide request body limit.
const maxBodySize = 1 << 20 // 1 MB limit for request bodies

func isValidPathParam(param string) bool {
	return !strings.Contains(param, "/") && !strings.Contains(param, "..") && strings.TrimSpace(param) == param && param != ""
}

type GatewayConfig struct {
	MatchServiceURL       string `json:"matchServiceUrl"`
	PlatformServiceURL    string `json:"platformServiceUrl"`
	MatchmakingServiceURL string `json:"matchmakingServiceUrl"`
}

type GatewayServiceHealth struct {
	URL        string `json:"url"`
	Healthy    bool   `json:"healthy"`
	StatusCode int    `json:"statusCode,omitempty"`
	Payload    any    `json:"payload,omitempty"`
	Error      string `json:"error,omitempty"`
}

type GatewaySystemStatus struct {
	Status    string                          `json:"status"`
	Service   string                          `json:"service"`
	CheckedAt time.Time                       `json:"checkedAt"`
	Services  map[string]GatewayServiceHealth `json:"services"`
}

type GatewayGuestIdentity struct {
	GuestID       string `json:"guestId,omitempty"`
	SessionSecret string `json:"sessionSecret,omitempty"`
	SessionToken  string `json:"sessionToken,omitempty"`
}

type GatewayAccountIdentity struct {
	AccountID    string `json:"accountId,omitempty"`
	SessionToken string `json:"sessionToken,omitempty"`
}

type GatewaySeatClaim struct {
	MatchID      string                `json:"matchId"`
	GuestID      string                `json:"guestId"`
	SeatColor    string                `json:"seatColor"`
	PlayerID     string                `json:"playerId"`
	PlayerSecret string                `json:"playerSecret"`
	ClaimToken   string                `json:"claimToken,omitempty"`
	ExpiresAt    time.Time             `json:"expiresAt,omitempty"`
	Queue        string                `json:"queue,omitempty"`
	ModeID       contracts.MatchModeID `json:"modeId,omitempty"`
	WhiteGuestID string                `json:"whiteGuestId,omitempty"`
	BlackGuestID string                `json:"blackGuestId,omitempty"`
	WhiteName    string                `json:"whiteName,omitempty"`
	BlackName    string                `json:"blackName,omitempty"`
	Status       string                `json:"status,omitempty"`
}

type GatewayBootstrapRequest struct {
	MatchID      string                  `json:"matchId,omitempty"`
	White        *GatewayGuestIdentity   `json:"white,omitempty"`
	Black        *GatewayGuestIdentity   `json:"black,omitempty"`
	WhiteAccount *GatewayAccountIdentity `json:"whiteAccount,omitempty"`
	BlackAccount *GatewayAccountIdentity `json:"blackAccount,omitempty"`
}

type GatewayBootstrapGuestSessions struct {
	White *platform.GuestSession `json:"white,omitempty"`
	Black *platform.GuestSession `json:"black,omitempty"`
}

type GatewayBootstrapMatchClaims struct {
	White *GatewaySeatClaim `json:"white,omitempty"`
	Black *GatewaySeatClaim `json:"black,omitempty"`
}

type GatewayBootstrapAccountSessions struct {
	White *platform.AccountSession `json:"white,omitempty"`
	Black *platform.AccountSession `json:"black,omitempty"`
}

type GatewayBootstrapQueueTickets struct {
	White *matchmaking.Ticket `json:"white,omitempty"`
	Black *matchmaking.Ticket `json:"black,omitempty"`
}

type GatewayBootstrapErrors struct {
	White string `json:"white,omitempty"`
	Black string `json:"black,omitempty"`
}

type GatewayBootstrapRecoveredMatch struct {
	MatchID      string                       `json:"matchId"`
	Queue        string                       `json:"queue,omitempty"`
	ModeID       contracts.MatchModeID        `json:"modeId,omitempty"`
	Status       string                       `json:"status,omitempty"`
	ViewerSeat   string                       `json:"viewerSeat,omitempty"`
	WhiteGuestID string                       `json:"whiteGuestId,omitempty"`
	BlackGuestID string                       `json:"blackGuestId,omitempty"`
	WhiteName    string                       `json:"whiteName,omitempty"`
	BlackName    string                       `json:"blackName,omitempty"`
	Claims       *GatewayBootstrapMatchClaims `json:"claims,omitempty"`
}

type GatewayBootstrapPayload struct {
	Status               string                           `json:"status"`
	RealtimeReady        bool                             `json:"realtimeReady"`
	PlatformReady        bool                             `json:"platformReady"`
	MatchmakingReady     bool                             `json:"matchmakingReady"`
	Authoritative        bool                             `json:"authoritative"`
	Services             map[string]GatewayServiceHealth  `json:"services"`
	ServiceEndpoints     GatewayConfig                    `json:"serviceEndpoints"`
	PlatformCaps         any                              `json:"platformCaps,omitempty"`
	DefaultQueue         any                              `json:"defaultQueue,omitempty"`
	GuestSessions        *GatewayBootstrapGuestSessions   `json:"guestSessions,omitempty"`
	MatchClaims          *GatewayBootstrapMatchClaims     `json:"matchClaims,omitempty"`
	AccountSessions      *GatewayBootstrapAccountSessions `json:"accountSessions,omitempty"`
	QueueTickets         *GatewayBootstrapQueueTickets    `json:"queueTickets,omitempty"`
	RecoveredMatch       *GatewayBootstrapRecoveredMatch  `json:"recoveredMatch,omitempty"`
	SessionErrors        *GatewayBootstrapErrors          `json:"sessionErrors,omitempty"`
	ClaimErrors          *GatewayBootstrapErrors          `json:"claimErrors,omitempty"`
	AccountErrors        *GatewayBootstrapErrors          `json:"accountErrors,omitempty"`
	QueueErrors          *GatewayBootstrapErrors          `json:"queueErrors,omitempty"`
	RecoveredMatchErrors *GatewayBootstrapErrors          `json:"recoveredMatchErrors,omitempty"`
	RequestedMatchID     string                           `json:"requestedMatchId,omitempty"`
	BootstrapCheckedAt   time.Time                        `json:"bootstrapCheckedAt"`
	Message              string                           `json:"message"`
}

type GatewayPrivateMatchRequest struct {
	Guest         GatewayGuestIdentity    `json:"guest"`
	Account       *GatewayAccountIdentity `json:"account,omitempty"`
	Queue         string                  `json:"queue,omitempty"`
	ModeID        contracts.MatchModeID   `json:"modeId,omitempty"`
	Difficulty    string                  `json:"difficulty,omitempty"`
	ClockSeconds  int64                   `json:"clockSeconds,omitempty"`
	PreferredSeat string                  `json:"preferredSeat,omitempty"`
}

type GatewayPrivateMatchResponse struct {
	MatchID            string                          `json:"matchId"`
	SeatColor          string                          `json:"seatColor"`
	WaitingForOpponent bool                            `json:"waitingForOpponent"`
	Snapshot           contracts.MatchSnapshotResponse `json:"snapshot"`
	Claim              *GatewaySeatClaim               `json:"claim,omitempty"`
}

type GatewayDirectChallengeRequest struct {
	Guest           GatewayGuestIdentity    `json:"guest"`
	Account         *GatewayAccountIdentity `json:"account,omitempty"`
	TargetAccountID string                  `json:"targetAccountId"`
	ModeID          contracts.MatchModeID   `json:"modeId,omitempty"`
	ClockSeconds    int64                   `json:"clockSeconds,omitempty"`
	PreferredSeat   string                  `json:"preferredSeat,omitempty"`
}

type GatewayDirectChallengeAcceptRequest struct {
	Guest   GatewayGuestIdentity    `json:"guest"`
	Account *GatewayAccountIdentity `json:"account,omitempty"`
}

type GatewayDirectChallengeView struct {
	ChallengeID    string                `json:"challengeId"`
	Status         string                `json:"status"`
	MatchID        string                `json:"matchId"`
	ModeID         contracts.MatchModeID `json:"modeId,omitempty"`
	ClockSeconds   int64                 `json:"clockSeconds,omitempty"`
	ChallengerSeat string                `json:"challengerSeat,omitempty"`
	ViewerSeat     string                `json:"viewerSeat,omitempty"`
}

type GatewayDirectChallengeLaunchResponse struct {
	ChallengeID string                      `json:"challengeId"`
	ModeID      contracts.MatchModeID       `json:"modeId,omitempty"`
	Match       GatewayPrivateMatchResponse `json:"match"`
}
