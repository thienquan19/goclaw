package personal

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
)

const (
	pairingDebounce      = 60 * time.Second
	groupPairingDebounce = 24 * time.Hour // Groups: debounce 24h to avoid spamming
)

// checkDMPolicy enforces DM policy for incoming messages.
func (c *Channel) checkDMPolicy(ctx context.Context, senderID, chatID string) bool {
	result := c.CheckDMPolicy(ctx, senderID, c.config.DMPolicy)
	switch result {
	case channels.PolicyAllow:
		return true
	case channels.PolicyNeedsPairing:
		c.sendPairingReply(ctx, senderID, chatID)
		return false
	default:
		slog.Debug("zalo_personal DM rejected by policy", "sender_id", senderID, "policy", c.config.DMPolicy)
		return false
	}
}

// checkGroupPolicy enforces group access policy (allowlist/pairing).
// Returns false if the group is blocked by policy; does NOT check @mention gating.
func (c *Channel) checkGroupPolicy(ctx context.Context, senderID, groupID string) bool {
	result := c.CheckGroupPolicy(ctx, senderID, groupID, c.config.GroupPolicy)
	switch result {
	case channels.PolicyAllow:
		return true
	case channels.PolicyNeedsPairing:
		// Send pairing notification as DM to the individual sender,
		// NOT into the group chat (avoids spamming the group).
		c.sendGroupPairingDM(ctx, senderID, groupID)
		return false
	default:
		slog.Debug("zalo_personal group message rejected by policy", "group_id", groupID, "policy", c.config.GroupPolicy)
		return false
	}
}

// sendGroupPairingDM sends a pairing notification as a DM to the individual
// sender instead of posting into the group chat. The pairing request is still
// registered so the admin can approve it via Web Dashboard or CLI.
func (c *Channel) sendGroupPairingDM(ctx context.Context, senderID, groupID string) {
	ps := c.PairingService()
	sess := c.session()
	if ps == nil || sess == nil {
		return
	}

	groupSenderID := fmt.Sprintf("group:%s", groupID)

	// Use longer debounce for groups (24h) to avoid repeatedly notifying
	// about the same unapproved group.
	if !c.CanSendPairingNotif(groupSenderID, groupPairingDebounce) {
		return
	}

	code, err := ps.RequestPairing(ctx, groupSenderID, c.Name(), groupID, "default", nil)
	if err != nil {
		slog.Debug("zalo_personal group pairing request failed", "group_id", groupID, "error", err)
		return
	}

	replyText := fmt.Sprintf(
		"🔐 Nhóm Zalo (ID: %s) chưa được cấp quyền truy cập.\n\n"+
			"🔑 Mã ghép nối: %s\n\n"+
			"✅ Chủ bot có thể phê duyệt:\n"+
			"  • Web Dashboard → Nodes → Approve\n"+
			"  • CLI: goclaw pairing approve %s",
		groupID, code, code,
	)

	// Send as DM to the individual sender, NOT into the group.
	sendCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := protocol.SendMessage(sendCtx, sess, senderID, protocol.ThreadTypeUser, replyText); err != nil {
		// DM delivery may fail (e.g. user hasn't DM'd the bot before).
		// Still mark as sent to avoid retry spam; the pairing request is
		// already visible in Web Dashboard.
		slog.Warn("zalo_personal: failed to send group pairing DM, pairing request still registered in dashboard",
			"sender_id", senderID, "group_id", groupID, "error", err)
		c.MarkPairingNotifSent(groupSenderID)
	} else {
		c.MarkPairingNotifSent(groupSenderID)
		slog.Info("zalo_personal group pairing DM sent", "sender_id", senderID, "group_id", groupID, "code", code)
	}
}

func (c *Channel) sendPairingReply(ctx context.Context, senderID, chatID string) {
	ps := c.PairingService()
	sess := c.session()
	if ps == nil || sess == nil {
		return
	}

	if !c.CanSendPairingNotif(senderID, pairingDebounce) {
		return
	}

	code, err := ps.RequestPairing(ctx, senderID, c.Name(), chatID, "default", nil)
	if err != nil {
		slog.Debug("zalo_personal pairing request failed", "sender_id", senderID, "error", err)
		return
	}

	replyText := fmt.Sprintf(
		"🔐 Chưa được cấp quyền truy cập.\n\n"+
			"🆔 Zalo ID: %s\n"+
			"🔑 Mã ghép nối: %s\n\n"+
			"✅ Chủ bot phê duyệt:\n"+
			"  • Web Dashboard → Nodes → Approve\n"+
			"  • CLI: goclaw pairing approve %s",
		senderID, code, code,
	)

	sendCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := protocol.SendMessage(sendCtx, sess, chatID, protocol.ThreadTypeUser, replyText); err != nil {
		slog.Warn("zalo_personal: failed to send pairing reply", "error", err)
	} else {
		c.MarkPairingNotifSent(senderID)
		slog.Info("zalo_personal pairing reply sent", "sender_id", senderID, "code", code)
	}
}

// checkBotMentioned reports whether the bot is @mentioned in the message.
func (c *Channel) checkBotMentioned(mentions []*protocol.TMention) bool {
	sess := c.session()
	if sess == nil {
		return false
	}
	return isBotMentioned(sess.UID, mentions)
}

// isBotMentioned checks if the bot's UID is @mentioned in the message.
// Filters out @all mentions (Type=1, UID="-1") — only targeted @bot counts.
func isBotMentioned(botUID string, mentions []*protocol.TMention) bool {
	if botUID == "" {
		return false
	}

	for _, m := range mentions {
		if m == nil {
			continue
		}
		if m.Type == protocol.MentionAll || m.UID == protocol.MentionAllUID {
			continue
		}
		if m.UID == botUID {
			return true
		}
	}
	return false
}
