package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/chess404/realtime/internal/contracts"
	"github.com/chess404/realtime/internal/platform"
)

// Response view types, error writers, and overview responders.

type friendOverviewResponse struct {
	Viewer   platform.PublicAccountProfile `json:"viewer"`
	Friends  []friendshipView              `json:"friends"`
	Incoming []friendRequestView           `json:"incoming"`
	Outgoing []friendRequestView           `json:"outgoing"`
}

type friendshipView struct {
	FriendshipID string                        `json:"friendshipId"`
	Account      platform.PublicAccountProfile `json:"account"`
	CreatedAt    time.Time                     `json:"createdAt"`
}

type friendRequestView struct {
	RequestID string                        `json:"requestId"`
	Status    string                        `json:"status"`
	Account   platform.PublicAccountProfile `json:"account"`
	CreatedAt time.Time                     `json:"createdAt"`
	UpdatedAt time.Time                     `json:"updatedAt"`
}

type challengeOverviewResponse struct {
	Viewer   platform.PublicAccountProfile `json:"viewer"`
	Incoming []directChallengeView         `json:"incoming"`
	Outgoing []directChallengeView         `json:"outgoing"`
}

type directChallengeView struct {
	ChallengeID    string                        `json:"challengeId"`
	Status         string                        `json:"status"`
	Account        platform.PublicAccountProfile `json:"account"`
	MatchID        string                        `json:"matchId"`
	ModeID         contracts.MatchModeID         `json:"modeId,omitempty"`
	ClockSeconds   int64                         `json:"clockSeconds,omitempty"`
	ChallengerSeat string                        `json:"challengerSeat,omitempty"`
	ViewerSeat     string                        `json:"viewerSeat,omitempty"`
	CreatedAt      time.Time                     `json:"createdAt"`
	UpdatedAt      time.Time                     `json:"updatedAt"`
}

type notificationOverviewResponse struct {
	Viewer        platform.PublicAccountProfile `json:"viewer"`
	Notifications []accountNotificationView     `json:"notifications"`
	UnreadCount   int                           `json:"unreadCount"`
}

type accountNotificationView struct {
	NotificationID  string                        `json:"notificationId"`
	Kind            string                        `json:"kind"`
	Actor           platform.PublicAccountProfile `json:"actor"`
	FriendRequestID string                        `json:"friendRequestId,omitempty"`
	ChallengeID     string                        `json:"challengeId,omitempty"`
	MatchID         string                        `json:"matchId,omitempty"`
	ModeID          contracts.MatchModeID         `json:"modeId,omitempty"`
	ChallengerSeat  string                        `json:"challengerSeat,omitempty"`
	CreatedAt       time.Time                     `json:"createdAt"`
	UpdatedAt       time.Time                     `json:"updatedAt"`
	ReadAt          *time.Time                    `json:"readAt,omitempty"`
}

type moderationOverviewResponse struct {
	Viewer           platform.PublicAccountProfile `json:"viewer"`
	OutgoingBlocks   []accountBlockView            `json:"outgoingBlocks"`
	IncomingBlocks   []accountBlockView            `json:"incomingBlocks"`
	SubmittedReports []playerReportView            `json:"submittedReports"`
}

type moderationAdminOverviewResponse struct {
	Viewer             platform.PublicAccountProfile `json:"viewer"`
	SelectedStatus     string                        `json:"selectedStatus,omitempty"`
	Reports            []moderationAdminReportView   `json:"reports"`
	RecentActions      []moderationActionAuditView   `json:"recentActions"`
	ActiveRestrictions []accountRestrictionView      `json:"activeRestrictions"`
}

type accountBlockView struct {
	BlockID   string                        `json:"blockId"`
	Direction string                        `json:"direction"`
	Reason    string                        `json:"reason,omitempty"`
	Account   platform.PublicAccountProfile `json:"account"`
	CreatedAt time.Time                     `json:"createdAt"`
	UpdatedAt time.Time                     `json:"updatedAt"`
}

type playerReportView struct {
	ReportID       string                         `json:"reportId"`
	Category       string                         `json:"category"`
	Details        string                         `json:"details,omitempty"`
	Status         string                         `json:"status"`
	Target         platform.PublicAccountProfile  `json:"target"`
	ReviewedBy     *platform.PublicAccountProfile `json:"reviewedBy,omitempty"`
	ReviewedAt     *time.Time                     `json:"reviewedAt,omitempty"`
	ResolutionNote string                         `json:"resolutionNote,omitempty"`
	CreatedAt      time.Time                      `json:"createdAt"`
	UpdatedAt      time.Time                      `json:"updatedAt"`
}

type moderationAdminReportView struct {
	ReportID          string                         `json:"reportId"`
	Category          string                         `json:"category"`
	Details           string                         `json:"details,omitempty"`
	Status            string                         `json:"status"`
	Reporter          platform.PublicAccountProfile  `json:"reporter"`
	Target            platform.PublicAccountProfile  `json:"target"`
	TargetRestriction *accountRestrictionView        `json:"targetRestriction,omitempty"`
	ReviewedBy        *platform.PublicAccountProfile `json:"reviewedBy,omitempty"`
	ReviewedAt        *time.Time                     `json:"reviewedAt,omitempty"`
	ResolutionNote    string                         `json:"resolutionNote,omitempty"`
	CreatedAt         time.Time                      `json:"createdAt"`
	UpdatedAt         time.Time                      `json:"updatedAt"`
}

type moderationActionAuditView struct {
	ActionID       string                        `json:"actionId"`
	ReportID       string                        `json:"reportId"`
	PreviousStatus string                        `json:"previousStatus"`
	NextStatus     string                        `json:"nextStatus"`
	Action         string                        `json:"action"`
	Note           string                        `json:"note,omitempty"`
	Moderator      platform.PublicAccountProfile `json:"moderator"`
	Reporter       platform.PublicAccountProfile `json:"reporter"`
	Target         platform.PublicAccountProfile `json:"target"`
	CreatedAt      time.Time                     `json:"createdAt"`
}

type accountRestrictionView struct {
	RestrictionID string                         `json:"restrictionId"`
	Account       platform.PublicAccountProfile  `json:"account"`
	Kind          string                         `json:"kind"`
	Reason        string                         `json:"reason,omitempty"`
	ReportID      string                         `json:"reportId,omitempty"`
	AppliedBy     *platform.PublicAccountProfile `json:"appliedBy,omitempty"`
	CreatedAt     time.Time                      `json:"createdAt"`
	UpdatedAt     time.Time                      `json:"updatedAt"`
}

func writeAccountSessionError(w http.ResponseWriter, err error) {
	switch err {
	case platform.ErrUnauthorizedAccountSession:
		http.Error(w, `{"error":"unauthorized account session"}`, http.StatusUnauthorized)
	case platform.ErrAccountRestricted:
		http.Error(w, `{"error":"account access restricted"}`, http.StatusForbidden)
	case os.ErrNotExist:
		http.Error(w, `{"error":"unknown account"}`, http.StatusNotFound)
	case os.ErrInvalid:
		http.Error(w, `{"error":"accountId is required"}`, http.StatusBadRequest)
	default:
		http.Error(w, `{"error":"failed to resume account session"}`, http.StatusBadRequest)
	}
}

func writeAccountAuthError(w http.ResponseWriter, err error) {
	switch err {
	case platform.ErrUnauthorizedAccountSession:
		http.Error(w, `{"error":"unauthorized account session"}`, http.StatusUnauthorized)
	case platform.ErrAccountRestricted:
		http.Error(w, `{"error":"account access restricted"}`, http.StatusForbidden)
	case platform.ErrInvalidAccountEmail:
		http.Error(w, `{"error":"invalid account email"}`, http.StatusBadRequest)
	case platform.ErrAccountEmailTaken:
		http.Error(w, `{"error":"account email already taken"}`, http.StatusConflict)
	case platform.ErrInvalidAccountPassword:
		http.Error(w, `{"error":"invalid account password"}`, http.StatusBadRequest)
	case platform.ErrAccountLoginUnavailable:
		http.Error(w, `{"error":"account login is not enabled"}`, http.StatusBadRequest)
	case platform.ErrAccountEmailAlreadyVerified:
		http.Error(w, `{"error":"account email already verified"}`, http.StatusConflict)
	case platform.ErrUnauthorizedAccountEmailVerification:
		http.Error(w, `{"error":"unauthorized account email verification"}`, http.StatusUnauthorized)
	case platform.ErrAccountEmailNotVerified:
		http.Error(w, `{"error":"account email is not verified"}`, http.StatusForbidden)
	case platform.ErrUnauthorizedAccountPasswordReset:
		http.Error(w, `{"error":"unauthorized account password reset"}`, http.StatusUnauthorized)
	case platform.ErrUnauthorizedAccountCredentials:
		http.Error(w, `{"error":"unauthorized account credentials"}`, http.StatusUnauthorized)
	case os.ErrNotExist:
		http.Error(w, `{"error":"unknown account"}`, http.StatusNotFound)
	case os.ErrInvalid:
		http.Error(w, `{"error":"accountId is required"}`, http.StatusBadRequest)
	default:
		http.Error(w, `{"error":"failed to update account authentication"}`, http.StatusInternalServerError)
	}
}

func writeModerationError(w http.ResponseWriter, err error) {
	switch err {
	case platform.ErrAccountInteractionBlocked:
		http.Error(w, `{"error":"account interaction blocked"}`, http.StatusForbidden)
	case platform.ErrAccountRestricted:
		http.Error(w, `{"error":"account access restricted"}`, http.StatusForbidden)
	case platform.ErrAccountBlockNotFound:
		http.Error(w, `{"error":"account block not found"}`, http.StatusNotFound)
	case platform.ErrInvalidAccountBlock:
		http.Error(w, `{"error":"invalid account block"}`, http.StatusBadRequest)
	case platform.ErrInvalidAccountRestriction:
		http.Error(w, `{"error":"invalid account restriction"}`, http.StatusBadRequest)
	case platform.ErrAccountRestrictionNotFound:
		http.Error(w, `{"error":"account restriction not found"}`, http.StatusNotFound)
	case platform.ErrInvalidPlayerReport:
		http.Error(w, `{"error":"invalid player report"}`, http.StatusBadRequest)
	case platform.ErrPlayerReportNotFound:
		http.Error(w, `{"error":"player report not found"}`, http.StatusNotFound)
	case platform.ErrInvalidModerationReview:
		http.Error(w, `{"error":"invalid moderation review"}`, http.StatusBadRequest)
	default:
		http.Error(w, `{"error":"failed to update moderation state"}`, http.StatusInternalServerError)
	}
}

func writeAccountRestrictionError(w http.ResponseWriter, restriction platform.AccountRestriction) {
	message := "account access restricted"
	switch restriction.Kind {
	case platform.AccountRestrictionKindSuspended:
		message = "account suspended"
	case platform.AccountRestrictionKindBanned:
		message = "account banned"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":             message,
		"restrictionKind":   restriction.Kind,
		"restrictionReason": strings.TrimSpace(restriction.Reason),
	})
}

func ensureAllowedAccountSession(accounts platform.AccountDirectory, moderation platform.ModerationDirectory, accountID, sessionToken string) (platform.AccountSession, *platform.AccountRestriction, error) {
	session, err := accounts.ResumeAccount(accountID, sessionToken)
	if err != nil {
		return platform.AccountSession{}, nil, err
	}
	if restriction, ok := moderation.GetAccountRestriction(session.Account.AccountID); ok {
		_ = accounts.LogoutAccount(session.Account.AccountID, session.SessionToken)
		return platform.AccountSession{}, &restriction, platform.ErrAccountRestricted
	}
	return session, nil, nil
}

func resumeAllowedAccountSessionOrWrite(w http.ResponseWriter, accounts platform.AccountDirectory, moderation platform.ModerationDirectory, accountID, sessionToken string) (platform.AccountSession, bool) {
	session, restriction, err := ensureAllowedAccountSession(accounts, moderation, accountID, sessionToken)
	if err != nil {
		if err == platform.ErrAccountRestricted && restriction != nil {
			writeAccountRestrictionError(w, *restriction)
		} else {
			writeAccountSessionError(w, err)
		}
		return platform.AccountSession{}, false
	}
	return session, true
}

func writeModerationAdminAuthError(w http.ResponseWriter) {
	http.Error(w, `{"error":"moderation admin access required"}`, http.StatusForbidden)
}

func writeNotificationError(w http.ResponseWriter, err error) {
	switch err {
	case platform.ErrAccountNotificationNotFound:
		http.Error(w, `{"error":"account notification not found"}`, http.StatusNotFound)
	case platform.ErrUnauthorizedAccountNotification:
		http.Error(w, `{"error":"unauthorized account notification"}`, http.StatusForbidden)
	case platform.ErrInvalidAccountNotification:
		http.Error(w, `{"error":"invalid account notification"}`, http.StatusBadRequest)
	default:
		http.Error(w, `{"error":"failed to update inbox"}`, http.StatusInternalServerError)
	}
}

func requireAccountInteractionAllowed(moderation platform.ModerationDirectory, accountID, otherAccountID string) error {
	if moderation.IsBlockedEitherDirection(accountID, otherAccountID) {
		return platform.ErrAccountInteractionBlocked
	}
	return nil
}

func isModerationAdminAccount(account platform.AccountProfile) bool {
	if account.AccountID != "" {
		if _, ok := configuredModerationAdminAccountIDs()[account.AccountID]; ok {
			return true
		}
	}
	// PLATFORM_ADMIN_HANDLES is an equally valid way to configure admin access
	// -- moderationAdminConfigured() (which gates whether the client renders
	// the admin panel at all) accepts it. Without this branch, a handles-only
	// deployment showed the panel and then 403'd every action inside it.
	if account.Handle != "" {
		if _, ok := configuredModerationAdminHandles()[strings.ToLower(account.Handle)]; ok {
			return true
		}
	}
	return false
}

func findFriendRequestCounterpartyAccountID(overview platform.FriendshipOverview, requestID, viewerAccountID string) string {
	resolvedRequestID := strings.TrimSpace(requestID)
	if resolvedRequestID == "" {
		return ""
	}
	for _, request := range overview.Incoming {
		if request.RequestID == resolvedRequestID {
			return request.RequesterAccountID
		}
	}
	for _, request := range overview.Outgoing {
		if request.RequestID == resolvedRequestID {
			return request.TargetAccountID
		}
	}
	return ""
}

func respondFriendOverview(w http.ResponseWriter, guests platform.GuestDirectory, accounts platform.AccountDirectory, friends platform.FriendshipDirectory, accountID string) {
	viewerAccount, ok := accounts.GetAccount(accountID)
	if !ok {
		http.Error(w, `{"error":"unknown account"}`, http.StatusNotFound)
		return
	}

	overview := friends.ListOverview(accountID)
	response := friendOverviewResponse{
		Viewer:   platform.BuildPublicAccountProfile(viewerAccount, guests),
		Friends:  make([]friendshipView, 0, len(overview.Friends)),
		Incoming: make([]friendRequestView, 0, len(overview.Incoming)),
		Outgoing: make([]friendRequestView, 0, len(overview.Outgoing)),
	}

	for _, friendship := range overview.Friends {
		friendAccountID := platform.FriendAccountForViewer(friendship, accountID)
		friendAccount, ok := accounts.GetAccount(friendAccountID)
		if !ok {
			continue
		}
		response.Friends = append(response.Friends, friendshipView{
			FriendshipID: friendship.FriendshipID,
			Account:      platform.BuildPublicAccountProfile(friendAccount, guests),
			CreatedAt:    friendship.CreatedAt,
		})
	}
	for _, request := range overview.Incoming {
		requester, ok := accounts.GetAccount(request.RequesterAccountID)
		if !ok {
			continue
		}
		response.Incoming = append(response.Incoming, friendRequestView{
			RequestID: request.RequestID,
			Status:    request.Status,
			Account:   platform.BuildPublicAccountProfile(requester, guests),
			CreatedAt: request.CreatedAt,
			UpdatedAt: request.UpdatedAt,
		})
	}
	for _, request := range overview.Outgoing {
		target, ok := accounts.GetAccount(request.TargetAccountID)
		if !ok {
			continue
		}
		response.Outgoing = append(response.Outgoing, friendRequestView{
			RequestID: request.RequestID,
			Status:    request.Status,
			Account:   platform.BuildPublicAccountProfile(target, guests),
			CreatedAt: request.CreatedAt,
			UpdatedAt: request.UpdatedAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func respondNotificationOverview(w http.ResponseWriter, guests platform.GuestDirectory, accounts platform.AccountDirectory, notifications platform.AccountNotificationDirectory, accountID string, limit int) {
	viewerAccount, ok := accounts.GetAccount(accountID)
	if !ok {
		http.Error(w, `{"error":"unknown account"}`, http.StatusNotFound)
		return
	}

	overview := notifications.ListOverview(accountID, limit)
	response := notificationOverviewResponse{
		Viewer:        platform.BuildPublicAccountProfile(viewerAccount, guests),
		Notifications: make([]accountNotificationView, 0, len(overview.Notifications)),
		UnreadCount:   overview.UnreadCount,
	}
	for _, notification := range overview.Notifications {
		view, ok := buildAccountNotificationView(guests, accounts, notification)
		if !ok {
			continue
		}
		response.Notifications = append(response.Notifications, view)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func writeAccountNotificationStreamEvent(w http.ResponseWriter, flusher http.Flusher, event platform.AccountNotificationEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: notification\ndata: %s\n\n", payload); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func buildAccountNotificationView(guests platform.GuestDirectory, accounts platform.AccountDirectory, notification platform.AccountNotification) (accountNotificationView, bool) {
	actor, ok := accounts.GetAccount(notification.ActorAccountID)
	if !ok {
		return accountNotificationView{}, false
	}
	return accountNotificationView{
		NotificationID:  notification.NotificationID,
		Kind:            notification.Kind,
		Actor:           platform.BuildPublicAccountProfile(actor, guests),
		FriendRequestID: notification.FriendRequestID,
		ChallengeID:     notification.ChallengeID,
		MatchID:         notification.MatchID,
		ModeID:          notification.ModeID,
		ChallengerSeat:  notification.ChallengerSeat,
		CreatedAt:       notification.CreatedAt,
		UpdatedAt:       notification.UpdatedAt,
		ReadAt:          notification.ReadAt,
	}, true
}

func respondModerationOverview(w http.ResponseWriter, guests platform.GuestDirectory, accounts platform.AccountDirectory, moderation platform.ModerationDirectory, accountID string) {
	viewerAccount, ok := accounts.GetAccount(accountID)
	if !ok {
		http.Error(w, `{"error":"unknown account"}`, http.StatusNotFound)
		return
	}

	overview := moderation.ListOverview(accountID)
	response := moderationOverviewResponse{
		Viewer:           platform.BuildPublicAccountProfile(viewerAccount, guests),
		OutgoingBlocks:   make([]accountBlockView, 0, len(overview.OutgoingBlocks)),
		IncomingBlocks:   make([]accountBlockView, 0, len(overview.IncomingBlocks)),
		SubmittedReports: make([]playerReportView, 0, len(overview.SubmittedReports)),
	}
	for _, block := range overview.OutgoingBlocks {
		target, ok := accounts.GetAccount(block.TargetAccountID)
		if !ok {
			continue
		}
		response.OutgoingBlocks = append(response.OutgoingBlocks, accountBlockView{
			BlockID:   block.BlockID,
			Direction: "outgoing",
			Reason:    block.Reason,
			Account:   platform.BuildPublicAccountProfile(target, guests),
			CreatedAt: block.CreatedAt,
			UpdatedAt: block.UpdatedAt,
		})
	}
	for _, block := range overview.IncomingBlocks {
		blocker, ok := accounts.GetAccount(block.BlockerAccountID)
		if !ok {
			continue
		}
		response.IncomingBlocks = append(response.IncomingBlocks, accountBlockView{
			BlockID:   block.BlockID,
			Direction: "incoming",
			Reason:    block.Reason,
			Account:   platform.BuildPublicAccountProfile(blocker, guests),
			CreatedAt: block.CreatedAt,
			UpdatedAt: block.UpdatedAt,
		})
	}
	for _, report := range overview.SubmittedReports {
		view, ok := buildPlayerReportView(guests, accounts, report)
		if ok {
			response.SubmittedReports = append(response.SubmittedReports, view)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func respondModerationAdminOverview(w http.ResponseWriter, guests platform.GuestDirectory, accounts platform.AccountDirectory, moderation platform.ModerationDirectory, accountID string, limit int, status string) {
	viewerAccount, ok := accounts.GetAccount(accountID)
	if !ok {
		http.Error(w, `{"error":"unknown account"}`, http.StatusNotFound)
		return
	}

	overview := moderation.ListAdminOverview(limit, status)
	response := moderationAdminOverviewResponse{
		Viewer:             platform.BuildPublicAccountProfile(viewerAccount, guests),
		SelectedStatus:     normalizeModerationStatusFilter(status),
		Reports:            make([]moderationAdminReportView, 0, len(overview.Reports)),
		RecentActions:      make([]moderationActionAuditView, 0, len(overview.RecentActions)),
		ActiveRestrictions: make([]accountRestrictionView, 0, len(overview.ActiveRestrictions)),
	}
	restrictionViews := make(map[string]accountRestrictionView, len(overview.ActiveRestrictions))
	for _, restriction := range overview.ActiveRestrictions {
		view, ok := buildAccountRestrictionView(guests, accounts, restriction)
		if !ok {
			continue
		}
		restrictionViews[restriction.AccountID] = view
		response.ActiveRestrictions = append(response.ActiveRestrictions, view)
	}
	for _, report := range overview.Reports {
		view, ok := buildModerationAdminReportView(guests, accounts, report)
		if ok {
			if restriction, restricted := restrictionViews[report.TargetAccountID]; restricted {
				restrictionCopy := restriction
				view.TargetRestriction = &restrictionCopy
			}
			response.Reports = append(response.Reports, view)
		}
	}
	for _, action := range overview.RecentActions {
		view, ok := buildModerationActionAuditView(guests, accounts, action)
		if ok {
			response.RecentActions = append(response.RecentActions, view)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func buildPlayerReportView(guests platform.GuestDirectory, accounts platform.AccountDirectory, report platform.PlayerReport) (playerReportView, bool) {
	target, ok := accounts.GetAccount(report.TargetAccountID)
	if !ok {
		return playerReportView{}, false
	}
	var reviewedBy *platform.PublicAccountProfile
	if resolvedReviewerID := strings.TrimSpace(report.ReviewedByAccountID); resolvedReviewerID != "" {
		if reviewer, ok := accounts.GetAccount(resolvedReviewerID); ok {
			profile := platform.BuildPublicAccountProfile(reviewer, guests)
			reviewedBy = &profile
		}
	}
	return playerReportView{
		ReportID:       report.ReportID,
		Category:       string(report.Category),
		Details:        report.Details,
		Status:         report.Status,
		Target:         platform.BuildPublicAccountProfile(target, guests),
		ReviewedBy:     reviewedBy,
		ReviewedAt:     report.ReviewedAt,
		ResolutionNote: report.ResolutionNote,
		CreatedAt:      report.CreatedAt,
		UpdatedAt:      report.UpdatedAt,
	}, true
}

func buildModerationAdminReportView(guests platform.GuestDirectory, accounts platform.AccountDirectory, report platform.PlayerReport) (moderationAdminReportView, bool) {
	reporter, ok := accounts.GetAccount(report.ReporterAccountID)
	if !ok {
		return moderationAdminReportView{}, false
	}
	target, ok := accounts.GetAccount(report.TargetAccountID)
	if !ok {
		return moderationAdminReportView{}, false
	}
	var reviewedBy *platform.PublicAccountProfile
	if resolvedReviewerID := strings.TrimSpace(report.ReviewedByAccountID); resolvedReviewerID != "" {
		if reviewer, ok := accounts.GetAccount(resolvedReviewerID); ok {
			profile := platform.BuildPublicAccountProfile(reviewer, guests)
			reviewedBy = &profile
		}
	}
	return moderationAdminReportView{
		ReportID:       report.ReportID,
		Category:       string(report.Category),
		Details:        report.Details,
		Status:         report.Status,
		Reporter:       platform.BuildPublicAccountProfile(reporter, guests),
		Target:         platform.BuildPublicAccountProfile(target, guests),
		ReviewedBy:     reviewedBy,
		ReviewedAt:     report.ReviewedAt,
		ResolutionNote: report.ResolutionNote,
		CreatedAt:      report.CreatedAt,
		UpdatedAt:      report.UpdatedAt,
	}, true
}

func buildAccountRestrictionView(guests platform.GuestDirectory, accounts platform.AccountDirectory, restriction platform.AccountRestriction) (accountRestrictionView, bool) {
	account, ok := accounts.GetAccount(restriction.AccountID)
	if !ok {
		return accountRestrictionView{}, false
	}
	var appliedBy *platform.PublicAccountProfile
	if strings.TrimSpace(restriction.AppliedByAccountID) != "" {
		if moderator, ok := accounts.GetAccount(restriction.AppliedByAccountID); ok {
			profile := platform.BuildPublicAccountProfile(moderator, guests)
			appliedBy = &profile
		}
	}
	return accountRestrictionView{
		RestrictionID: restriction.RestrictionID,
		Account:       platform.BuildPublicAccountProfile(account, guests),
		Kind:          restriction.Kind,
		Reason:        restriction.Reason,
		ReportID:      restriction.ReportID,
		AppliedBy:     appliedBy,
		CreatedAt:     restriction.CreatedAt,
		UpdatedAt:     restriction.UpdatedAt,
	}, true
}

func buildModerationActionAuditView(guests platform.GuestDirectory, accounts platform.AccountDirectory, action platform.ModerationActionAudit) (moderationActionAuditView, bool) {
	moderator, ok := accounts.GetAccount(action.ModeratorAccountID)
	if !ok {
		return moderationActionAuditView{}, false
	}
	reporter, ok := accounts.GetAccount(action.ReporterAccountID)
	if !ok {
		return moderationActionAuditView{}, false
	}
	target, ok := accounts.GetAccount(action.TargetAccountID)
	if !ok {
		return moderationActionAuditView{}, false
	}
	return moderationActionAuditView{
		ActionID:       action.ActionID,
		ReportID:       action.ReportID,
		PreviousStatus: action.PreviousStatus,
		NextStatus:     action.NextStatus,
		Action:         action.Action,
		Note:           action.Note,
		Moderator:      platform.BuildPublicAccountProfile(moderator, guests),
		Reporter:       platform.BuildPublicAccountProfile(reporter, guests),
		Target:         platform.BuildPublicAccountProfile(target, guests),
		CreatedAt:      action.CreatedAt,
	}, true
}

func normalizeModerationStatusFilter(status string) string {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case platform.PlayerReportStatusOpen:
		return platform.PlayerReportStatusOpen
	case platform.PlayerReportStatusUnderReview:
		return platform.PlayerReportStatusUnderReview
	case platform.PlayerReportStatusResolvedActioned:
		return platform.PlayerReportStatusResolvedActioned
	case platform.PlayerReportStatusResolvedDismissed:
		return platform.PlayerReportStatusResolvedDismissed
	default:
		return ""
	}
}

func respondChallengeOverview(w http.ResponseWriter, guests platform.GuestDirectory, accounts platform.AccountDirectory, challenges platform.DirectChallengeDirectory, accountID string) {
	viewerAccount, ok := accounts.GetAccount(accountID)
	if !ok {
		http.Error(w, `{"error":"unknown account"}`, http.StatusNotFound)
		return
	}

	overview := challenges.ListOverview(accountID)
	response := challengeOverviewResponse{
		Viewer:   platform.BuildPublicAccountProfile(viewerAccount, guests),
		Incoming: make([]directChallengeView, 0, len(overview.Incoming)),
		Outgoing: make([]directChallengeView, 0, len(overview.Outgoing)),
	}

	for _, challenge := range overview.Incoming {
		if view, ok := buildDirectChallengeView(guests, accounts, challenge, accountID); ok {
			response.Incoming = append(response.Incoming, view)
		}
	}
	for _, challenge := range overview.Outgoing {
		if view, ok := buildDirectChallengeView(guests, accounts, challenge, accountID); ok {
			response.Outgoing = append(response.Outgoing, view)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func writeDirectChallengeView(w http.ResponseWriter, guests platform.GuestDirectory, accounts platform.AccountDirectory, challenge platform.DirectChallenge, viewerAccountID string) {
	view, ok := buildDirectChallengeView(guests, accounts, challenge, viewerAccountID)
	if !ok {
		http.Error(w, `{"error":"unknown direct challenge account"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(view)
}

func buildDirectChallengeView(guests platform.GuestDirectory, accounts platform.AccountDirectory, challenge platform.DirectChallenge, viewerAccountID string) (directChallengeView, bool) {
	opponentAccountID := platform.ChallengeOpponentAccountID(challenge, viewerAccountID)
	account, ok := accounts.GetAccount(opponentAccountID)
	if !ok {
		return directChallengeView{}, false
	}
	return directChallengeView{
		ChallengeID:    challenge.ChallengeID,
		Status:         challenge.Status,
		Account:        platform.BuildPublicAccountProfile(account, guests),
		MatchID:        challenge.MatchID,
		ModeID:         challenge.ModeID,
		ClockSeconds:   challenge.ClockSeconds,
		ChallengerSeat: challenge.ChallengerSeat,
		ViewerSeat:     platform.ChallengeViewerSeat(challenge, viewerAccountID),
		CreatedAt:      challenge.CreatedAt,
		UpdatedAt:      challenge.UpdatedAt,
	}, true
}
