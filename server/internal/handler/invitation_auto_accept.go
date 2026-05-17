package handler

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func (h *Handler) autoAcceptPendingInvitations(ctx context.Context, user db.User) db.User {
	if !h.cfg.AutoAcceptInvitationsOnLogin || h.TxStarter == nil {
		return user
	}

	rows, err := h.Queries.ListPendingInvitationsForUser(ctx, db.ListPendingInvitationsForUserParams{
		InviteeUserID: user.ID,
		InviteeEmail:  user.Email,
	})
	if err != nil {
		slog.Warn("auto-accept invitations: list failed", "error", err, "user_id", uuidToString(user.ID), "email", user.Email)
		return user
	}

	for _, inv := range rows {
		accepted, member, updatedUser, ok := h.autoAcceptInvitation(ctx, user, inv.ID)
		if !ok {
			continue
		}
		user = updatedUser

		wsID := uuidToString(accepted.WorkspaceID)
		userID := uuidToString(user.ID)
		memberResp := memberWithUserResponse(member, user)
		eventPayload := map[string]any{"member": memberResp}
		if ws, err := h.Queries.GetWorkspace(ctx, accepted.WorkspaceID); err == nil {
			eventPayload["workspace_name"] = ws.Name
		}
		h.publish(protocol.EventMemberAdded, wsID, "member", userID, eventPayload)
		h.publish(protocol.EventInvitationAccepted, wsID, "member", userID, map[string]any{
			"invitation_id": uuidToString(accepted.ID),
			"member":        memberResp,
		})

		var daysSinceInvite int64
		if inv.CreatedAt.Valid {
			daysSinceInvite = int64(time.Since(inv.CreatedAt.Time).Hours() / 24)
		}
		h.Analytics.Capture(analytics.TeamInviteAccepted(userID, wsID, daysSinceInvite))
		slog.Info("invitation auto-accepted", "invitation_id", uuidToString(accepted.ID), "user_id", userID, "workspace_id", wsID)
	}

	return user
}

func (h *Handler) autoAcceptInvitation(ctx context.Context, user db.User, invitationID pgtype.UUID) (db.WorkspaceInvitation, db.Member, db.User, bool) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		slog.Warn("auto-accept invitation: begin failed", "error", err, "invitation_id", uuidToString(invitationID))
		return db.WorkspaceInvitation{}, db.Member{}, user, false
	}
	defer tx.Rollback(ctx)

	qtx := h.Queries.WithTx(tx)
	accepted, err := qtx.AcceptInvitation(ctx, invitationID)
	if err != nil {
		if !isNotFound(err) {
			slog.Warn("auto-accept invitation: accept failed", "error", err, "invitation_id", uuidToString(invitationID))
		}
		return db.WorkspaceInvitation{}, db.Member{}, user, false
	}

	member, err := qtx.CreateMember(ctx, db.CreateMemberParams{
		WorkspaceID: accepted.WorkspaceID,
		UserID:      user.ID,
		Role:        accepted.Role,
	})
	if err != nil {
		if !isUniqueViolation(err) {
			slog.Warn("auto-accept invitation: create membership failed", "error", err, "invitation_id", uuidToString(invitationID), "workspace_id", uuidToString(accepted.WorkspaceID))
			return db.WorkspaceInvitation{}, db.Member{}, user, false
		}
		member, err = qtx.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
			UserID:      user.ID,
			WorkspaceID: accepted.WorkspaceID,
		})
		if err != nil {
			slog.Warn("auto-accept invitation: load existing membership failed", "error", err, "invitation_id", uuidToString(invitationID), "workspace_id", uuidToString(accepted.WorkspaceID))
			return db.WorkspaceInvitation{}, db.Member{}, user, false
		}
	}

	updatedUser, err := qtx.MarkUserOnboarded(ctx, user.ID)
	if err != nil {
		slog.Warn("auto-accept invitation: mark onboarded failed", "error", err, "invitation_id", uuidToString(invitationID), "user_id", uuidToString(user.ID))
		return db.WorkspaceInvitation{}, db.Member{}, user, false
	}

	if err := tx.Commit(ctx); err != nil {
		slog.Warn("auto-accept invitation: commit failed", "error", err, "invitation_id", uuidToString(invitationID))
		return db.WorkspaceInvitation{}, db.Member{}, user, false
	}

	return accepted, member, updatedUser, true
}
