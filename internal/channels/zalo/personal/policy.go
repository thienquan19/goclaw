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
func (c *Channel) checkGroupPolicy(ctx context.Context, senderID, groupID string, isMentioned bool) bool {
	result := c.CheckGroupPolicy(ctx, senderID, groupID, c.config.GroupPolicy)
	switch result {
	case channels.PolicyAllow:
		return true
	case channels.PolicyNeedsPairing:
		// Only register pairing request if the bot was explicitly mentioned.
		// We do NOT send any messages to the group or DM, we just silently
		// register it so the admin can approve via Web Dashboard.
		if isMentioned {
			c.requestGroupPairing(ctx, groupID)
		}
		return false
	default:
		slog.Debug("zalo_personal group message rejected by policy", "group_id", groupID, "policy", c.config.GroupPolicy)
		return false
	}
}

// requestGroupPairing registers a pairing request for a group silently.
// It does NOT send any notification messages to the chat to avoid spam.
// The admin can view and approve the request via Web Dashboard.
func (c *Channel) requestGroupPairing(ctx context.Context, groupID string) {
	ps := c.PairingService()
	if ps == nil {
		return
	}

	groupSenderID := fmt.Sprintf("group:%s", groupID)

	// Use longer debounce for groups (24h) to avoid spamming the pairing database.
	if !c.CanSendPairingNotif(groupSenderID, groupPairingDebounce) {
		return
	}

	_, err := ps.RequestPairing(ctx, groupSenderID, c.Name(), groupID, "default", nil)
	if err != nil {
		slog.Debug("zalo_personal group pairing request failed", "group_id", groupID, "error", err)
		return
	}

	c.MarkPairingNotifSent(groupSenderID)
	slog.Info("zalo_personal group pairing requested silently", "group_id", groupID)
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
