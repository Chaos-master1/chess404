package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/chess404/realtime/internal/platform"
)

// Guest/account session bootstrapping and reclamation per side.

func bootstrapAccountSessionForSide(config GatewayConfig, client *http.Client, identity *GatewayAccountIdentity, guestSession *platform.GuestSession, r *http.Request) (*platform.AccountSession, string) {
	if identity == nil || strings.TrimSpace(identity.AccountID) == "" {
		return nil, ""
	}

	result := fetchGatewayJSONRequest(r, client, http.MethodPost, config.PlatformServiceURL+"/api/platform/account-sessions", map[string]string{
		"accountId":    strings.TrimSpace(identity.AccountID),
		"sessionToken": strings.TrimSpace(identity.SessionToken),
	})
	if result.Healthy {
		session, err := decodeGatewayPayload[platform.AccountSession](result.Payload)
		if err != nil {
			return nil, fmt.Sprintf("failed to decode account session: %v", err)
		}
		return &session, ""
	}

	if result.StatusCode == http.StatusUnauthorized && guestSession != nil && strings.TrimSpace(guestSession.Guest.GuestID) != "" {
		reclaimed, errMessage := reclaimGatewayAccountSession(config, client, strings.TrimSpace(identity.AccountID), guestSession, r)
		if reclaimed != nil || errMessage != "" {
			return reclaimed, errMessage
		}
	}

	return nil, gatewayErrorMessage(result, "failed to bootstrap account session")
}

func reclaimGatewayAccountSession(config GatewayConfig, client *http.Client, accountID string, guestSession *platform.GuestSession, r *http.Request) (*platform.AccountSession, string) {
	accountResult := fetchGatewayJSON(r, client, config.PlatformServiceURL+"/api/platform/accounts/"+accountID)
	if !accountResult.Healthy {
		return nil, gatewayErrorMessage(accountResult, "failed to fetch account profile")
	}
	accountEnvelope, err := decodeGatewayPayload[struct {
		Account platform.AccountProfile `json:"account"`
	}](accountResult.Payload)
	if err != nil {
		return nil, fmt.Sprintf("failed to decode account profile: %v", err)
	}
	if strings.TrimSpace(accountEnvelope.Account.Handle) == "" {
		return nil, "account profile is missing handle"
	}

	claimPayload := map[string]string{
		"guestId": guestSession.Guest.GuestID,
		"handle":  accountEnvelope.Account.Handle,
	}
	if strings.TrimSpace(guestSession.SessionToken) != "" {
		claimPayload["sessionToken"] = strings.TrimSpace(guestSession.SessionToken)
	} else {
		claimPayload["sessionSecret"] = strings.TrimSpace(guestSession.SessionSecret)
	}

	claimResult := fetchGatewayJSONRequest(r, client, http.MethodPost, config.PlatformServiceURL+"/api/platform/accounts/claim", claimPayload)
	if !claimResult.Healthy {
		return nil, gatewayErrorMessage(claimResult, "failed to reclaim account session")
	}
	session, err := decodeGatewayPayload[platform.AccountSession](claimResult.Payload)
	if err != nil {
		return nil, fmt.Sprintf("failed to decode reclaimed account session: %v", err)
	}
	return &session, ""
}

func resolveGatewayClaimByToken(config GatewayConfig, client *http.Client, matchID, claimToken string, r *http.Request) (*GatewaySeatClaim, string) {
	result := fetchGatewayJSONRequest(r, client, http.MethodPost, config.PlatformServiceURL+"/api/platform/match-claims/resolve", map[string]string{
		"matchId":    matchID,
		"claimToken": claimToken,
	})
	if !result.Healthy {
		return nil, gatewayErrorMessage(result, "failed to resolve room claim")
	}
	claim, err := decodeGatewayPayload[GatewaySeatClaim](result.Payload)
	if err != nil {
		return nil, fmt.Sprintf("failed to decode room claim: %v", err)
	}
	return &claim, ""
}
