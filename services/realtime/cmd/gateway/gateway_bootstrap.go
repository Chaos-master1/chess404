package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/chess404/realtime/internal/contracts"
	"github.com/chess404/realtime/internal/matchmaking"
	"github.com/chess404/realtime/internal/platform"
)

// Bootstrap payload assembly: guest sessions, match claims, account sessions, queue tickets, recovered matches.

func buildGatewayBootstrapPayload(config GatewayConfig, client *http.Client, request GatewayBootstrapRequest, r *http.Request) GatewayBootstrapPayload {
	systemStatus := collectGatewayStatus(config, client, r)
	capabilities := fetchGatewayJSON(r, client, config.PlatformServiceURL+"/api/platform/capabilities")
	defaultQueue := fetchGatewayJSON(r, client, config.MatchmakingServiceURL+"/api/queues/default")
	guestSessions, sessionErrors := bootstrapGuestSessions(config, client, request, r)
	matchSnapshot := syncMatchSnapshotToPlatformServiceIfActive(config, client, request.MatchID, r)
	matchClaims, claimErrors := bootstrapMatchClaims(config, client, request.MatchID, guestSessions, r)
	if (matchClaims == nil || (matchClaims.White == nil && matchClaims.Black == nil)) && matchSnapshot != nil {
		fallback := buildFallbackMatchClaims(matchSnapshot, guestSessions)
		if fallback != nil {
			log.Printf("gw:bootstrap: using fallback match claims from snapshot for matchID=%s", request.MatchID)
			matchClaims = fallback
			claimErrors = nil
		}
	}
	accountSessions, accountErrors := bootstrapAccountSessions(config, client, request, guestSessions, r)
	queueTickets, queueErrors := bootstrapQueueTickets(config, client, guestSessions, accountSessions, r)
	recoveredMatch, recoveredMatchErrors := bootstrapRecoveredMatch(config, client, guestSessions, queueTickets, r)

	return GatewayBootstrapPayload{
		Status:               systemStatus.Status,
		RealtimeReady:        systemStatus.Services["match"].Healthy,
		PlatformReady:        systemStatus.Services["platform"].Healthy,
		MatchmakingReady:     systemStatus.Services["matchmaking"].Healthy,
		Authoritative:        systemStatus.Services["match"].Healthy,
		Services:             systemStatus.Services,
		ServiceEndpoints:     GatewayConfig{},
		PlatformCaps:         capabilities.Payload,
		DefaultQueue:         defaultQueue.Payload,
		GuestSessions:        guestSessions,
		MatchClaims:          matchClaims,
		AccountSessions:      accountSessions,
		QueueTickets:         queueTickets,
		RecoveredMatch:       recoveredMatch,
		SessionErrors:        sessionErrors,
		ClaimErrors:          claimErrors,
		AccountErrors:        accountErrors,
		QueueErrors:          queueErrors,
		RecoveredMatchErrors: recoveredMatchErrors,
		RequestedMatchID:     request.MatchID,
		BootstrapCheckedAt:   time.Now().UTC(),
		Message:              bootstrapMessage(systemStatus),
	}
}

// bootstrapResumedSuppliedGuest reports whether this seat's session is the very
// one the caller already holds credentials for. Only then can the response omit
// the secret. A caller whose identity failed to resume gets a brand-new guest
// back, and must be handed that new guest's secret or it can never authenticate.
func bootstrapResumedSuppliedGuest(request GatewayBootstrapRequest, side, resolvedGuestID string) bool {
	identity := request.White
	if side == "black" {
		identity = request.Black
	}
	if identity == nil {
		return false
	}
	supplied := strings.TrimSpace(identity.GuestID)
	if supplied == "" {
		return false
	}
	return supplied == strings.TrimSpace(resolvedGuestID)
}

func bootstrapGuestSessions(config GatewayConfig, client *http.Client, request GatewayBootstrapRequest, r *http.Request) (*GatewayBootstrapGuestSessions, *GatewayBootstrapErrors) {
	sessions := &GatewayBootstrapGuestSessions{}
	errors := &GatewayBootstrapErrors{}

	if session, errMessage := bootstrapGuestSessionForSide(config, client, request.White, r); session != nil {
		sessions.White = session
	} else if errMessage != "" {
		errors.White = errMessage
	}

	if session, errMessage := bootstrapGuestSessionForSide(config, client, request.Black, r); session != nil {
		sessions.Black = session
	} else if errMessage != "" {
		errors.Black = errMessage
	}

	if sessions.White == nil && sessions.Black == nil {
		sessions = nil
	}
	if errors.White == "" && errors.Black == "" {
		errors = nil
	}

	return sessions, errors
}

func bootstrapGuestSessionForSide(config GatewayConfig, client *http.Client, identity *GatewayGuestIdentity, r *http.Request) (*platform.GuestSession, string) {
	result := fetchGatewayJSONRequest(r, client, http.MethodPost, config.PlatformServiceURL+"/api/platform/guest-sessions", identity)
	if !result.Healthy && result.StatusCode == http.StatusUnauthorized && identity != nil && (identity.GuestID != "" || identity.SessionSecret != "") {
		result = fetchGatewayJSONRequest(r, client, http.MethodPost, config.PlatformServiceURL+"/api/platform/guest-sessions", GatewayGuestIdentity{})
	}
	if !result.Healthy {
		return nil, gatewayErrorMessage(result, "failed to bootstrap guest session")
	}

	session, err := decodeGatewayPayload[platform.GuestSession](result.Payload)
	if err != nil {
		return nil, fmt.Sprintf("failed to decode guest session: %v", err)
	}
	return &session, ""
}

func bootstrapMatchClaims(config GatewayConfig, client *http.Client, matchID string, sessions *GatewayBootstrapGuestSessions, r *http.Request) (*GatewayBootstrapMatchClaims, *GatewayBootstrapErrors) {
	if matchID == "" || sessions == nil {
		return nil, nil
	}

	claims := &GatewayBootstrapMatchClaims{}
	errors := &GatewayBootstrapErrors{}

	if claim, errMessage := bootstrapMatchClaimForSide(config, client, matchID, sessions.White, nil, r); claim != nil {
		claims.White = sanitizeSeatClaim(claim)
	} else if errMessage != "" {
		errors.White = errMessage
	}

	if claim, errMessage := bootstrapMatchClaimForSide(config, client, matchID, sessions.Black, nil, r); claim != nil {
		claims.Black = sanitizeSeatClaim(claim)
	} else if errMessage != "" {
		errors.Black = errMessage
	}

	if claims.White == nil && claims.Black == nil {
		claims = nil
	}
	if errors.White == "" && errors.Black == "" {
		errors = nil
	}

	return claims, errors
}

type matchClaimBootstrapFields struct {
	SeatColor    string
	PlayerSecret string
	WhiteGuestID string
	BlackGuestID string
	WhiteName    string
	BlackName    string
	Queue        string
	ModeID       string
	MatchStatus  string
}

func bootstrapMatchClaimForSide(config GatewayConfig, client *http.Client, matchID string, session *platform.GuestSession, matchFields *matchClaimBootstrapFields, r *http.Request) (*GatewaySeatClaim, string) {
	if session == nil || session.Guest.GuestID == "" || session.SessionSecret == "" {
		return nil, ""
	}

	body := map[string]string{
		"matchId":       matchID,
		"guestId":       session.Guest.GuestID,
		"sessionSecret": session.SessionSecret,
	}
	if matchFields != nil {
		body["seatColor"] = matchFields.SeatColor
		body["playerSecret"] = matchFields.PlayerSecret
		body["whiteGuestId"] = matchFields.WhiteGuestID
		body["blackGuestId"] = matchFields.BlackGuestID
		body["whiteName"] = matchFields.WhiteName
		body["blackName"] = matchFields.BlackName
		body["queue"] = matchFields.Queue
		body["modeId"] = matchFields.ModeID
		body["matchStatus"] = matchFields.MatchStatus
	}
	result := fetchGatewayJSONRequest(r, client, http.MethodPost, config.PlatformServiceURL+"/api/platform/match-claims", body)
	if !result.Healthy {
		return nil, gatewayErrorMessage(result, "failed to recover match claim")
	}

	claim, err := decodeGatewayPayload[GatewaySeatClaim](result.Payload)
	if err != nil {
		return nil, fmt.Sprintf("failed to decode match claim: %v", err)
	}
	return &claim, ""
}

func syncMatchSnapshotToPlatformService(config GatewayConfig, client *http.Client, snapshot contracts.MatchSnapshotResponse, r *http.Request) {
	if strings.TrimSpace(config.PlatformServiceURL) == "" {
		log.Printf("gw:sync-match: skipped, no platform-service URL configured")
		return
	}
	result := fetchGatewayJSONRequest(r, client, http.MethodPost, config.PlatformServiceURL+"/api/platform/matches", snapshot)
	if result.Error != "" && result.StatusCode == 0 {
		log.Printf("gw:sync-match: connection error to platform-service: %v", result.Error)
		return
	}
	if !result.Healthy {
		log.Printf("gw:sync-match: platform-service returned status=%d", result.StatusCode)
		return
	}
	log.Printf("gw:sync-match: ok matchID=%s", snapshot.Match.MatchID)
}

func syncMatchSnapshotToPlatformServiceIfActive(config GatewayConfig, client *http.Client, matchID string, r *http.Request) *contracts.MatchSnapshotResponse {
	if strings.TrimSpace(matchID) == "" || strings.TrimSpace(config.MatchServiceURL) == "" {
		return nil
	}
	result := fetchGatewayJSON(r, client, config.MatchServiceURL+"/api/matches/"+matchID)
	if !result.Healthy {
		return nil
	}
	snapshot, err := decodeGatewayPayload[contracts.MatchSnapshotResponse](result.Payload)
	if err != nil {
		log.Printf("gw:sync-match: failed to decode match-service snapshot for matchID=%s: %v", matchID, err)
		return nil
	}
	syncMatchSnapshotToPlatformService(config, client, snapshot, r)
	return &snapshot
}

func buildFallbackMatchClaims(snapshot *contracts.MatchSnapshotResponse, sessions *GatewayBootstrapGuestSessions) *GatewayBootstrapMatchClaims {
	if snapshot == nil || sessions == nil {
		return nil
	}
	claims := &GatewayBootstrapMatchClaims{}
	m := snapshot.Match

	buildClaim := func(session *platform.GuestSession) *GatewaySeatClaim {
		if session == nil || session.Guest.GuestID == "" {
			return nil
		}
		gid := session.Guest.GuestID
		switch gid {
		case m.WhiteGuestID:
			return &GatewaySeatClaim{
				MatchID:      m.MatchID,
				GuestID:      gid,
				SeatColor:    "white",
				PlayerID:     gid,
				PlayerSecret: m.WhitePlayerSecret,
				Queue:        m.Queue,
				ModeID:       m.ModeID,
				WhiteGuestID: m.WhiteGuestID,
				BlackGuestID: m.BlackGuestID,
				WhiteName:    m.WhiteName,
				BlackName:    m.BlackName,
				Status:       m.Status,
			}
		case m.BlackGuestID:
			return &GatewaySeatClaim{
				MatchID:      m.MatchID,
				GuestID:      gid,
				SeatColor:    "black",
				PlayerID:     gid,
				PlayerSecret: m.BlackPlayerSecret,
				Queue:        m.Queue,
				ModeID:       m.ModeID,
				WhiteGuestID: m.WhiteGuestID,
				BlackGuestID: m.BlackGuestID,
				WhiteName:    m.WhiteName,
				BlackName:    m.BlackName,
				Status:       m.Status,
			}
		}
		return nil
	}

	claims.White = buildClaim(sessions.White)
	claims.Black = buildClaim(sessions.Black)

	if claims.White == nil && claims.Black == nil {
		return nil
	}
	return claims
}

func bootstrapAccountSessions(config GatewayConfig, client *http.Client, request GatewayBootstrapRequest, guestSessions *GatewayBootstrapGuestSessions, r *http.Request) (*GatewayBootstrapAccountSessions, *GatewayBootstrapErrors) {
	sessions := &GatewayBootstrapAccountSessions{}
	errors := &GatewayBootstrapErrors{}

	if session, errMessage := bootstrapAccountSessionForSide(config, client, request.WhiteAccount, guestSessionsSide(guestSessions, "white"), r); session != nil {
		sessions.White = session
	} else if errMessage != "" {
		errors.White = errMessage
	}

	if session, errMessage := bootstrapAccountSessionForSide(config, client, request.BlackAccount, guestSessionsSide(guestSessions, "black"), r); session != nil {
		sessions.Black = session
	} else if errMessage != "" {
		errors.Black = errMessage
	}

	if sessions.White == nil && sessions.Black == nil {
		sessions = nil
	}
	if errors.White == "" && errors.Black == "" {
		errors = nil
	}

	return sessions, errors
}

func bootstrapQueueTickets(
	config GatewayConfig,
	client *http.Client,
	guestSessions *GatewayBootstrapGuestSessions,
	accountSessions *GatewayBootstrapAccountSessions,
	r *http.Request,
) (*GatewayBootstrapQueueTickets, *GatewayBootstrapErrors) {
	tickets := &GatewayBootstrapQueueTickets{}
	errors := &GatewayBootstrapErrors{}

	if ticket, errMessage := bootstrapQueueTicketForSide(config, client, guestSessionsSide(guestSessions, "white"), accountSessionsSide(accountSessions, "white"), r); ticket != nil {
		tickets.White = ticket
	} else if errMessage != "" {
		errors.White = errMessage
	}

	if ticket, errMessage := bootstrapQueueTicketForSide(config, client, guestSessionsSide(guestSessions, "black"), accountSessionsSide(accountSessions, "black"), r); ticket != nil {
		tickets.Black = ticket
	} else if errMessage != "" {
		errors.Black = errMessage
	}

	if tickets.White == nil && tickets.Black == nil {
		tickets = nil
	}
	if errors.White == "" && errors.Black == "" {
		errors = nil
	}

	return tickets, errors
}

func bootstrapQueueTicketForSide(
	config GatewayConfig,
	client *http.Client,
	guestSession *platform.GuestSession,
	accountSession *platform.AccountSession,
	r *http.Request,
) (*matchmaking.Ticket, string) {
	guestID := ""
	if guestSession != nil {
		guestID = strings.TrimSpace(guestSession.Guest.GuestID)
	}
	accountID := ""
	if accountSession != nil {
		accountID = strings.TrimSpace(accountSession.Account.AccountID)
	}
	if guestID == "" && accountID == "" {
		return nil, ""
	}

	params := url.Values{}
	if guestID != "" {
		params.Set("guestId", guestID)
	}
	if accountID != "" {
		params.Set("accountId", accountID)
	}

	result := fetchGatewayJSON(r, client, config.MatchmakingServiceURL+"/api/queues/tickets?"+params.Encode())
	if result.StatusCode == http.StatusNotFound {
		return nil, ""
	}
	if !result.Healthy {
		return nil, gatewayErrorMessage(result, "failed to recover queue ticket")
	}

	payload, err := decodeGatewayPayload[struct {
		Ticket matchmaking.Ticket `json:"ticket"`
	}](result.Payload)
	if err != nil {
		return nil, fmt.Sprintf("failed to decode queue ticket: %v", err)
	}
	return &payload.Ticket, ""
}

func bootstrapRecoveredMatch(
	config GatewayConfig,
	client *http.Client,
	guestSessions *GatewayBootstrapGuestSessions,
	queueTickets *GatewayBootstrapQueueTickets,
	r *http.Request,
) (*GatewayBootstrapRecoveredMatch, *GatewayBootstrapErrors) {
	claims, errors := bootstrapActiveMatchClaims(config, client, guestSessions, r)
	if activeMatch := recoveredMatchFromClaims(claims); activeMatch != nil {
		return activeMatch, errors
	}

	if activeMatch := recoveredMatchFromQueueTickets(queueTickets, guestSessions); activeMatch != nil {
		return activeMatch, errors
	}

	if errors != nil && errors.White == "" && errors.Black == "" {
		errors = nil
	}
	return nil, errors
}

func bootstrapActiveMatchClaims(
	config GatewayConfig,
	client *http.Client,
	guestSessions *GatewayBootstrapGuestSessions,
	r *http.Request,
) (*GatewayBootstrapMatchClaims, *GatewayBootstrapErrors) {
	claims := &GatewayBootstrapMatchClaims{}
	errors := &GatewayBootstrapErrors{}

	if claim, errMessage := bootstrapActiveMatchClaimForSide(config, client, guestSessionsSide(guestSessions, "white"), r); claim != nil {
		claims.White = claim
	} else if errMessage != "" {
		errors.White = errMessage
	}

	if claim, errMessage := bootstrapActiveMatchClaimForSide(config, client, guestSessionsSide(guestSessions, "black"), r); claim != nil {
		claims.Black = claim
	} else if errMessage != "" {
		errors.Black = errMessage
	}

	if claims.White == nil && claims.Black == nil {
		claims = nil
	}
	if errors.White == "" && errors.Black == "" {
		errors = nil
	}

	return claims, errors
}

func bootstrapActiveMatchClaimForSide(
	config GatewayConfig,
	client *http.Client,
	session *platform.GuestSession,
	r *http.Request,
) (*GatewaySeatClaim, string) {
	if session == nil || strings.TrimSpace(session.Guest.GuestID) == "" {
		return nil, ""
	}

	payload := map[string]string{
		"guestId": session.Guest.GuestID,
	}
	if strings.TrimSpace(session.SessionToken) != "" {
		payload["sessionToken"] = strings.TrimSpace(session.SessionToken)
	} else if strings.TrimSpace(session.SessionSecret) != "" {
		payload["sessionSecret"] = strings.TrimSpace(session.SessionSecret)
	}

	result := fetchGatewayJSONRequest(r, client, http.MethodPost, config.PlatformServiceURL+"/api/platform/match-claims/active", payload)
	if result.StatusCode == http.StatusNotFound {
		return nil, ""
	}
	if !result.Healthy {
		return nil, gatewayErrorMessage(result, "failed to recover active match")
	}

	claim, err := decodeGatewayPayload[GatewaySeatClaim](result.Payload)
	if err != nil {
		return nil, fmt.Sprintf("failed to decode active match claim: %v", err)
	}
	return &claim, ""
}

func recoveredMatchFromClaims(claims *GatewayBootstrapMatchClaims) *GatewayBootstrapRecoveredMatch {
	if claims == nil {
		return nil
	}

	primary := claims.White
	if primary == nil || !isGatewayRecoverableClaimStatus(primary.Status) {
		primary = claims.Black
	}
	if primary == nil || !isGatewayRecoverableClaimStatus(primary.Status) || strings.TrimSpace(primary.MatchID) == "" {
		return nil
	}

	return &GatewayBootstrapRecoveredMatch{
		MatchID:      primary.MatchID,
		Queue:        primary.Queue,
		ModeID:       primary.ModeID,
		Status:       primary.Status,
		ViewerSeat:   primary.SeatColor,
		WhiteGuestID: primary.WhiteGuestID,
		BlackGuestID: primary.BlackGuestID,
		WhiteName:    primary.WhiteName,
		BlackName:    primary.BlackName,
		Claims:       claims,
	}
}

func recoveredMatchFromQueueTickets(
	tickets *GatewayBootstrapQueueTickets,
	guestSessions *GatewayBootstrapGuestSessions,
) *GatewayBootstrapRecoveredMatch {
	if tickets == nil {
		return nil
	}

	type ticketCandidate struct {
		side   string
		ticket *matchmaking.Ticket
	}
	for _, candidateEntry := range []ticketCandidate{
		{side: "white", ticket: tickets.White},
		{side: "black", ticket: tickets.Black},
	} {
		candidate := candidateEntry.ticket
		if candidate == nil || candidate.Status != matchmaking.StatusMatched || strings.TrimSpace(candidate.AssignedRoom) == "" {
			continue
		}

		viewerSeat := strings.TrimSpace(candidate.SeatColor)
		whiteGuestID := strings.TrimSpace(candidate.MatchedWith)
		blackGuestID := strings.TrimSpace(candidate.MatchedWith)
		whiteName := strings.TrimSpace(candidate.OpponentName)
		blackName := strings.TrimSpace(candidate.OpponentName)

		if viewerSeat == "white" {
			if guest := guestSessionsSide(guestSessions, candidateEntry.side); guest != nil {
				whiteGuestID = guest.Guest.GuestID
				whiteName = guest.Guest.DisplayName
			}
		} else if viewerSeat == "black" {
			if guest := guestSessionsSide(guestSessions, candidateEntry.side); guest != nil {
				blackGuestID = guest.Guest.GuestID
				blackName = guest.Guest.DisplayName
			}
		}

		return &GatewayBootstrapRecoveredMatch{
			MatchID:      candidate.AssignedRoom,
			Queue:        string(candidate.Queue),
			ModeID:       candidate.ModeID,
			Status:       string(candidate.Status),
			ViewerSeat:   viewerSeat,
			WhiteGuestID: whiteGuestID,
			BlackGuestID: blackGuestID,
			WhiteName:    whiteName,
			BlackName:    blackName,
		}
	}

	return nil
}

func isGatewayRecoverableClaimStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "waiting", "active":
		return true
	default:
		return false
	}
}

func guestSessionsSide(sessions *GatewayBootstrapGuestSessions, side string) *platform.GuestSession {
	if sessions == nil {
		return nil
	}
	if side == "white" {
		return sessions.White
	}
	return sessions.Black
}

func accountSessionsSide(sessions *GatewayBootstrapAccountSessions, side string) *platform.AccountSession {
	if sessions == nil {
		return nil
	}
	if side == "white" {
		return sessions.White
	}
	return sessions.Black
}
