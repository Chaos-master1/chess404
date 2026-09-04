package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Direct challenge create/accept flows.

func createGatewayDirectChallenge(config GatewayConfig, client *http.Client, request GatewayDirectChallengeRequest, r *http.Request) (GatewayDirectChallengeLaunchResponse, int, error) {
	session, statusCode, err := ensureGatewayPrivateGuestSession(config, client, request.Guest, r)
	if err != nil {
		return GatewayDirectChallengeLaunchResponse{}, statusCode, err
	}
	accountSession, statusCode, err := ensureGatewayPrivateAccountSession(config, client, request.Account, session, r)
	if err != nil {
		return GatewayDirectChallengeLaunchResponse{}, statusCode, err
	}
	if accountSession == nil {
		return GatewayDirectChallengeLaunchResponse{}, http.StatusUnauthorized, errors.New("direct challenges require a signed-in account session")
	}
	targetAccountID := strings.TrimSpace(request.TargetAccountID)
	if targetAccountID == "" {
		return GatewayDirectChallengeLaunchResponse{}, http.StatusBadRequest, errors.New("target account is required")
	}

	eligibility := fetchGatewayJSONRequest(r, client, http.MethodPost, config.PlatformServiceURL+"/api/platform/challenges/eligibility", map[string]string{
		"accountId":       accountSession.Account.AccountID,
		"sessionToken":    accountSession.SessionToken,
		"targetAccountId": targetAccountID,
	})
	if !eligibility.Healthy {
		return GatewayDirectChallengeLaunchResponse{}, statusOrDefault(eligibility.StatusCode, http.StatusBadGateway), errors.New(gatewayErrorMessage(eligibility, "failed to validate direct challenge"))
	}

	matchResponse, statusCode, err := createGatewayPrivateMatch(config, client, GatewayPrivateMatchRequest{
		Guest:         request.Guest,
		Account:       request.Account,
		ModeID:        request.ModeID,
		ClockSeconds:  request.ClockSeconds,
		PreferredSeat: request.PreferredSeat,
	}, r)
	if err != nil {
		return GatewayDirectChallengeLaunchResponse{}, statusCode, err
	}

	createResult := fetchGatewayJSONRequest(r, client, http.MethodPost, config.PlatformServiceURL+"/api/platform/challenges", map[string]any{
		"accountId":       accountSession.Account.AccountID,
		"sessionToken":    accountSession.SessionToken,
		"targetAccountId": targetAccountID,
		"matchId":         matchResponse.MatchID,
		"modeId":          matchResponse.Snapshot.Match.ModeID,
		"clockSeconds":    request.ClockSeconds,
		"challengerSeat":  matchResponse.SeatColor,
	})
	if !createResult.Healthy {
		return GatewayDirectChallengeLaunchResponse{}, statusOrDefault(createResult.StatusCode, http.StatusBadGateway), errors.New(gatewayErrorMessage(createResult, "failed to persist direct challenge"))
	}
	challenge, err := decodeGatewayPayload[GatewayDirectChallengeView](createResult.Payload)
	if err != nil {
		return GatewayDirectChallengeLaunchResponse{}, http.StatusBadGateway, fmt.Errorf("failed to decode direct challenge: %v", err)
	}

	return GatewayDirectChallengeLaunchResponse{
		ChallengeID: challenge.ChallengeID,
		ModeID:      challenge.ModeID,
		Match:       matchResponse,
	}, http.StatusCreated, nil
}

func acceptGatewayDirectChallenge(config GatewayConfig, client *http.Client, challengeID string, request GatewayDirectChallengeAcceptRequest, r *http.Request) (GatewayDirectChallengeLaunchResponse, int, error) {
	session, statusCode, err := ensureGatewayPrivateGuestSession(config, client, request.Guest, r)
	if err != nil {
		return GatewayDirectChallengeLaunchResponse{}, statusCode, err
	}
	accountSession, statusCode, err := ensureGatewayPrivateAccountSession(config, client, request.Account, session, r)
	if err != nil {
		return GatewayDirectChallengeLaunchResponse{}, statusCode, err
	}
	if accountSession == nil {
		return GatewayDirectChallengeLaunchResponse{}, http.StatusUnauthorized, errors.New("direct challenges require a signed-in account session")
	}

	viewResult := fetchGatewayJSONRequest(r, client, http.MethodPost, config.PlatformServiceURL+"/api/platform/challenges/"+challengeID+"/view", map[string]string{
		"accountId":    accountSession.Account.AccountID,
		"sessionToken": accountSession.SessionToken,
	})
	if !viewResult.Healthy {
		return GatewayDirectChallengeLaunchResponse{}, statusOrDefault(viewResult.StatusCode, http.StatusBadGateway), errors.New(gatewayErrorMessage(viewResult, "failed to load direct challenge"))
	}
	challenge, err := decodeGatewayPayload[GatewayDirectChallengeView](viewResult.Payload)
	if err != nil {
		return GatewayDirectChallengeLaunchResponse{}, http.StatusBadGateway, fmt.Errorf("failed to decode direct challenge: %v", err)
	}
	if challenge.Status != "pending" {
		return GatewayDirectChallengeLaunchResponse{}, http.StatusConflict, errors.New("direct challenge is no longer pending")
	}

	matchResponse, statusCode, err := joinGatewayPrivateMatch(config, client, challenge.MatchID, GatewayPrivateMatchRequest{
		Guest:   request.Guest,
		Account: request.Account,
	}, r)
	if err != nil {
		return GatewayDirectChallengeLaunchResponse{}, statusCode, err
	}

	respondResult := fetchGatewayJSONRequest(r, client, http.MethodPost, config.PlatformServiceURL+"/api/platform/challenges/"+challengeID+"/respond", map[string]any{
		"accountId":    accountSession.Account.AccountID,
		"sessionToken": accountSession.SessionToken,
		"accept":       true,
	})
	if !respondResult.Healthy {
		return GatewayDirectChallengeLaunchResponse{}, statusOrDefault(respondResult.StatusCode, http.StatusBadGateway), errors.New(gatewayErrorMessage(respondResult, "failed to accept direct challenge"))
	}

	return GatewayDirectChallengeLaunchResponse{
		ChallengeID: challenge.ChallengeID,
		ModeID:      challenge.ModeID,
		Match:       matchResponse,
	}, http.StatusOK, nil
}
