package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/chess404/realtime/internal/contracts"
	"github.com/chess404/realtime/internal/platform"
)

// Private match create/join/rematch flows.

func createGatewayPrivateMatch(config GatewayConfig, client *http.Client, request GatewayPrivateMatchRequest, r *http.Request) (GatewayPrivateMatchResponse, int, error) {
	session, statusCode, err := ensureGatewayPrivateGuestSession(config, client, request.Guest, r)
	if err != nil {
		return GatewayPrivateMatchResponse{}, statusCode, err
	}
	accountSession, _, accountSessionErr := ensureGatewayPrivateAccountSession(config, client, request.Account, session, r)
	if accountSessionErr != nil {
		log.Printf("note: account session bootstrap skipped: %v", accountSessionErr)
	}
	return createGatewayPrivateMatchForSession(config, client, session, accountSession, request.Queue, request.ModeID, request.ClockSeconds, request.PreferredSeat, request.Difficulty, r)
}

func createGatewayPrivateMatchForSession(
	config GatewayConfig,
	client *http.Client,
	session *platform.GuestSession,
	accountSession *platform.AccountSession,
	queue string,
	modeID contracts.MatchModeID,
	clockSeconds int64,
	preferredSeat string,
	difficulty string,
	r *http.Request,
) (GatewayPrivateMatchResponse, int, error) {
	if session == nil {
		return GatewayPrivateMatchResponse{}, http.StatusBadRequest, errors.New("guest session is required")
	}
	preferredSeat = strings.ToLower(strings.TrimSpace(preferredSeat))
	if preferredSeat == "" {
		preferredSeat = "white"
	}

	matchQueue := strings.TrimSpace(queue)
	if matchQueue == "" {
		matchQueue = "direct"
	} else if matchQueue != "rated" && matchQueue != "casual" && matchQueue != "direct" {
		matchQueue = "direct"
	}

	createReq := contracts.CreateMatchRequest{
		ClockSeconds:    clockSeconds,
		Queue:           matchQueue,
		ModeID:          contracts.NormalizeMatchModeID(string(modeID)),
		Difficulty:      strings.TrimSpace(difficulty),
		StarterHandMode: "starter_three",
	}
	if createReq.ModeID == "" {
		createReq.ModeID = contracts.MatchModeOpenCards
	}
	if preferredSeat == "black" {
		createReq.BlackGuestID = session.Guest.GuestID
		createReq.BlackName = session.Guest.DisplayName
		createReq.BlackPlayerSecret = session.SessionSecret
		if accountSession != nil {
			createReq.BlackAccountID = accountSession.Account.AccountID
		}
	} else {
		createReq.WhiteGuestID = session.Guest.GuestID
		createReq.WhiteName = session.Guest.DisplayName
		createReq.WhitePlayerSecret = session.SessionSecret
		if accountSession != nil {
			createReq.WhiteAccountID = accountSession.Account.AccountID
		}
	}

	log.Printf("gw:create-private: starting modeID=%q difficulty=%q preferredSeat=%q matchServiceURL=%q",
		createReq.ModeID, createReq.Difficulty, preferredSeat, config.MatchServiceURL)
	result := fetchGatewayJSONRequest(r, client, http.MethodPost, config.MatchServiceURL+"/api/matches", createReq)
	if result.Error != "" && result.StatusCode == 0 {
		log.Printf("gw:create-private: connection error to match-service: %v", result.Error)
		return GatewayPrivateMatchResponse{}, http.StatusBadGateway, fmt.Errorf("match-service unreachable: %v", result.Error)
	}
	if !result.Healthy {
		log.Printf("gw:create-private: match-service returned status=%d", result.StatusCode)
		return GatewayPrivateMatchResponse{}, statusOrDefault(result.StatusCode, http.StatusBadGateway), errors.New(formatUpstreamError(result, "failed to create private match"))
	}
	snapshot, err := decodeGatewayPayload[contracts.MatchSnapshotResponse](result.Payload)
	if err != nil {
		log.Printf("gw:create-private: failed to decode match-service response: %v", err)
		return GatewayPrivateMatchResponse{}, http.StatusBadGateway, fmt.Errorf("failed to decode private match snapshot: %v", err)
	}
	log.Printf("gw:create-private: match created matchID=%s modeID=%s", snapshot.Match.MatchID, snapshot.Match.ModeID)

	syncMatchSnapshotToPlatformService(config, client, snapshot, r)

	seatColor := strings.ToLower(strings.TrimSpace(preferredSeat))
	if seatColor != "black" {
		seatColor = "white"
	}
	matchFields := &matchClaimBootstrapFields{
		SeatColor:    seatColor,
		PlayerSecret: session.SessionSecret,
		WhiteGuestID: strings.TrimSpace(snapshot.Match.WhiteGuestID),
		BlackGuestID: strings.TrimSpace(snapshot.Match.BlackGuestID),
		WhiteName:    strings.TrimSpace(snapshot.Match.WhiteName),
		BlackName:    strings.TrimSpace(snapshot.Match.BlackName),
		Queue:        strings.TrimSpace(snapshot.Match.Queue),
		ModeID:       string(snapshot.Match.ModeID),
		MatchStatus:  strings.TrimSpace(snapshot.Match.Status),
	}
	claim, claimErr := bootstrapMatchClaimForSide(config, client, snapshot.Match.MatchID, session, matchFields, r)
	if claimErr != "" {
		return GatewayPrivateMatchResponse{}, http.StatusBadGateway, fmt.Errorf("failed to bootstrap match claim: %s", claimErr)
	}
	if claim != nil {
		seatColor = claim.SeatColor
	}

	return GatewayPrivateMatchResponse{
		MatchID:            snapshot.Match.MatchID,
		SeatColor:          seatColor,
		WaitingForOpponent: snapshot.Match.Status == "waiting",
		Snapshot:           snapshot,
		Claim:              sanitizeSeatClaim(claim),
		GuestSession:       session,
	}, http.StatusCreated, nil
}

func joinGatewayPrivateMatch(config GatewayConfig, client *http.Client, matchID string, request GatewayPrivateMatchRequest, r *http.Request) (GatewayPrivateMatchResponse, int, error) {
	session, statusCode, err := ensureGatewayPrivateGuestSession(config, client, request.Guest, r)
	if err != nil {
		return GatewayPrivateMatchResponse{}, statusCode, err
	}
	accountSession, _, accountSessionErr := ensureGatewayPrivateAccountSession(config, client, request.Account, session, r)
	if accountSessionErr != nil {
		log.Printf("note: account session bootstrap skipped: %v", accountSessionErr)
	}

	joinReq := contracts.JoinMatchSeatRequest{
		GuestID:       session.Guest.GuestID,
		DisplayName:   session.Guest.DisplayName,
		PlayerSecret:  session.SessionSecret,
		PreferredSeat: strings.ToLower(strings.TrimSpace(request.PreferredSeat)),
	}
	if accountSession != nil {
		joinReq.AccountID = accountSession.Account.AccountID
	}

	result := fetchGatewayJSONRequest(r, client, http.MethodPost, config.MatchServiceURL+"/api/matches/"+matchID+"/join", joinReq)
	if result.Error != "" && result.StatusCode == 0 {
		return GatewayPrivateMatchResponse{}, http.StatusBadGateway, errors.New(result.Error)
	}
	if !result.Healthy {
		return GatewayPrivateMatchResponse{}, statusOrDefault(result.StatusCode, http.StatusBadGateway), errors.New(gatewayErrorMessage(result, "failed to join private match"))
	}
	joined, err := decodeGatewayPayload[contracts.JoinMatchSeatResponse](result.Payload)
	if err != nil {
		return GatewayPrivateMatchResponse{}, http.StatusBadGateway, fmt.Errorf("failed to decode private join response: %v", err)
	}

	syncMatchSnapshotToPlatformService(config, client, joined.Match, r)

	joinSeatColor := strings.ToLower(strings.TrimSpace(request.PreferredSeat))
	if joinSeatColor != "black" {
		joinSeatColor = "white"
	}
	joinMatchFields := &matchClaimBootstrapFields{
		SeatColor:    joinSeatColor,
		PlayerSecret: session.SessionSecret,
		WhiteGuestID: strings.TrimSpace(joined.Match.Match.WhiteGuestID),
		BlackGuestID: strings.TrimSpace(joined.Match.Match.BlackGuestID),
		WhiteName:    strings.TrimSpace(joined.Match.Match.WhiteName),
		BlackName:    strings.TrimSpace(joined.Match.Match.BlackName),
		Queue:        strings.TrimSpace(joined.Match.Match.Queue),
		ModeID:       string(joined.Match.Match.ModeID),
		MatchStatus:  strings.TrimSpace(joined.Match.Match.Status),
	}
	claim, claimErr := bootstrapMatchClaimForSide(config, client, matchID, session, joinMatchFields, r)
	if claimErr != "" {
		return GatewayPrivateMatchResponse{}, http.StatusBadGateway, fmt.Errorf("failed to bootstrap match claim: %s", claimErr)
	}

	if claim != nil {
		joinSeatColor = claim.SeatColor
	}
	return GatewayPrivateMatchResponse{
		MatchID:            joined.Match.Match.MatchID,
		SeatColor:          joinSeatColor,
		WaitingForOpponent: joined.WaitingForOpponent,
		Snapshot:           joined.Match,
		Claim:              sanitizeSeatClaim(claim),
		GuestSession:       session,
	}, http.StatusOK, nil
}

func rematchGatewayPrivateMatch(config GatewayConfig, client *http.Client, matchID string, request GatewayPrivateMatchRequest, r *http.Request) (GatewayPrivateMatchResponse, int, error) {
	session, statusCode, err := ensureGatewayPrivateGuestSession(config, client, request.Guest, r)
	if err != nil {
		return GatewayPrivateMatchResponse{}, statusCode, err
	}
	accountSession, _, accountSessionErr := ensureGatewayPrivateAccountSession(config, client, request.Account, session, r)
	if accountSessionErr != nil {
		log.Printf("note: account session bootstrap skipped: %v", accountSessionErr)
	}

	result := fetchGatewayJSONRequest(r, client, http.MethodGet, config.MatchServiceURL+"/api/matches/"+matchID, nil)
	if result.Error != "" && result.StatusCode == 0 {
		return GatewayPrivateMatchResponse{}, http.StatusBadGateway, errors.New(result.Error)
	}
	if !result.Healthy {
		return GatewayPrivateMatchResponse{}, statusOrDefault(result.StatusCode, http.StatusBadGateway), errors.New(gatewayErrorMessage(result, "failed to load private match for rematch"))
	}
	snapshot, err := decodeGatewayPayload[contracts.MatchSnapshotResponse](result.Payload)
	if err != nil {
		return GatewayPrivateMatchResponse{}, http.StatusBadGateway, fmt.Errorf("failed to decode private match snapshot: %v", err)
	}
	if snapshot.Match.Queue != "direct" {
		return GatewayPrivateMatchResponse{}, http.StatusConflict, errors.New("rematch rooms are only available for private direct matches")
	}
	if snapshot.Match.Status != "finished" {
		return GatewayPrivateMatchResponse{}, http.StatusConflict, errors.New("rematch is only available after the private match finishes")
	}

	requesterSeat := ""
	switch session.Guest.GuestID {
	case strings.TrimSpace(snapshot.Match.WhiteGuestID):
		requesterSeat = "white"
	case strings.TrimSpace(snapshot.Match.BlackGuestID):
		requesterSeat = "black"
	default:
		return GatewayPrivateMatchResponse{}, http.StatusForbidden, errors.New("only players from the original private match can create a rematch room")
	}

	clockSeconds := request.ClockSeconds
	if clockSeconds <= 0 {
		clockSeconds = 600
	}

	return createGatewayPrivateMatchForSession(
		config,
		client,
		session,
		accountSession,
		snapshot.Match.Queue,
		snapshot.Match.ModeID,
		clockSeconds,
		requesterSeat,
		"",
		r,
	)
}
