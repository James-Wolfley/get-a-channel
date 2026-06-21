package bot

import (
	"fmt"
	"log"
	"time"

	"github.com/bwmarrin/discordgo"
)

func (b *bot) handleVoiceStateUpdate(s *discordgo.Session, v *discordgo.VoiceStateUpdate) {
	if v.GuildID == "" || v.UserID == s.State.User.ID {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if v.BeforeUpdate != nil && v.BeforeUpdate.ChannelID != "" && v.BeforeUpdate.ChannelID != v.ChannelID {
		if err := b.handleDynamicLeave(v.GuildID, v.BeforeUpdate.ChannelID, v.UserID); err != nil {
			log.Printf("handle leave from %s: %v", v.BeforeUpdate.ChannelID, err)
		}
	}

	if v.ChannelID == "" || (v.BeforeUpdate != nil && v.BeforeUpdate.ChannelID == v.ChannelID) {
		return
	}

	if err := b.handleDynamicJoin(v.GuildID, v.ChannelID, v.UserID); err != nil {
		log.Printf("handle dynamic join to %s: %v", v.ChannelID, err)
	}

	triggerID, ok, err := b.getTriggerChannel(v.GuildID)
	if err != nil {
		log.Printf("read trigger channel: %v", err)
		return
	}
	if ok && v.ChannelID == triggerID {
		if err := b.createChannelForMember(v.GuildID, triggerID, v.UserID); err != nil {
			log.Printf("create channel for user %s: %v", v.UserID, err)
		}
	}
}

func (b *bot) createChannelForMember(guildID, triggerChannelID, userID string) error {
	trigger, err := b.s.Channel(triggerChannelID)
	if err != nil {
		return fmt.Errorf("read trigger channel: %w", err)
	}

	name := b.channelName(guildID, userID)
	newChannel, err := b.s.GuildChannelCreateComplex(guildID, discordgo.GuildChannelCreateData{
		Name:     name,
		Type:     discordgo.ChannelTypeGuildVoice,
		ParentID: trigger.ParentID,
	})
	if err != nil {
		return fmt.Errorf("create voice channel: %w", err)
	}

	now := time.Now().Unix()
	tx, err := b.db.Begin()
	if err != nil {
		return fmt.Errorf("begin create transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`INSERT INTO dynamic_channels (channel_id, guild_id, owner_user_id, created_at) VALUES (?, ?, ?, ?)`, newChannel.ID, guildID, userID, now); err != nil {
		return fmt.Errorf("save dynamic channel: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO channel_members (channel_id, user_id, join_order, joined_at) VALUES (?, ?, 1, ?)`, newChannel.ID, userID, now); err != nil {
		return fmt.Errorf("save initial member: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit create transaction: %w", err)
	}

	if err := b.s.GuildMemberMove(guildID, userID, &newChannel.ID); err != nil {
		if delErr := b.deleteDynamicChannel(newChannel.ID); delErr != nil {
			log.Printf("cleanup failed channel after move error %s: %v", newChannel.ID, delErr)
		}
		return fmt.Errorf("move member: %w", err)
	}

	log.Printf("created dynamic channel %s for user %s in guild %s", newChannel.ID, userID, guildID)
	return nil
}

func (b *bot) handleDynamicJoin(guildID, channelID, userID string) error {
	tracked, ok, err := b.getDynamicChannel(channelID)
	if err != nil || !ok {
		return err
	}
	if tracked.GuildID != guildID {
		return nil
	}

	nextOrder, err := b.nextJoinOrder(channelID)
	if err != nil {
		return err
	}
	_, err = b.db.Exec(`
INSERT INTO channel_members (channel_id, user_id, join_order, joined_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(channel_id, user_id) DO NOTHING
`, channelID, userID, nextOrder, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("save joined member: %w", err)
	}
	return nil
}

func (b *bot) handleDynamicLeave(guildID, channelID, userID string) error {
	tracked, ok, err := b.getDynamicChannel(channelID)
	if err != nil || !ok {
		return err
	}
	if tracked.GuildID != guildID {
		return nil
	}

	if _, err := b.db.Exec(`DELETE FROM channel_members WHERE channel_id = ? AND user_id = ?`, channelID, userID); err != nil {
		return fmt.Errorf("remove leaving member: %w", err)
	}

	return b.reconcileDynamicChannel(tracked)
}

func (b *bot) reconcileDynamicChannel(ch trackedChannel) error {
	connected, exists, err := b.connectedMembers(ch.GuildID, ch.ChannelID)
	if err != nil {
		return err
	}
	if !exists {
		return b.deleteDynamicChannelRecords(ch.ChannelID)
	}
	if len(connected) == 0 {
		return b.deleteDynamicChannel(ch.ChannelID)
	}

	if err := b.syncMemberRows(ch.ChannelID, connected); err != nil {
		return err
	}

	members, err := b.getOrderedMembers(ch.ChannelID)
	if err != nil {
		return err
	}
	if len(members) == 0 {
		return b.deleteDynamicChannel(ch.ChannelID)
	}

	ownerPresent := false
	for _, member := range members {
		if member.UserID == ch.OwnerUserID {
			ownerPresent = true
			break
		}
	}
	if ownerPresent {
		return nil
	}

	newOwnerID := members[0].UserID
	name := b.channelName(ch.GuildID, newOwnerID)
	if _, err := b.s.ChannelEdit(ch.ChannelID, &discordgo.ChannelEdit{Name: name}); err != nil {
		return fmt.Errorf("rename dynamic channel: %w", err)
	}
	if _, err := b.db.Exec(`UPDATE dynamic_channels SET owner_user_id = ? WHERE channel_id = ?`, newOwnerID, ch.ChannelID); err != nil {
		return fmt.Errorf("save new owner: %w", err)
	}
	return nil
}

func (b *bot) syncMemberRows(channelID string, connected map[string]bool) error {
	members, err := b.getOrderedMembers(channelID)
	if err != nil {
		return err
	}

	known := make(map[string]bool, len(members))
	for _, member := range members {
		known[member.UserID] = true
		if !connected[member.UserID] {
			if _, err := b.db.Exec(`DELETE FROM channel_members WHERE channel_id = ? AND user_id = ?`, channelID, member.UserID); err != nil {
				return fmt.Errorf("remove stale member row: %w", err)
			}
		}
	}

	for userID := range connected {
		if known[userID] {
			continue
		}
		nextOrder, err := b.nextJoinOrder(channelID)
		if err != nil {
			return err
		}
		if _, err := b.db.Exec(`INSERT INTO channel_members (channel_id, user_id, join_order, joined_at) VALUES (?, ?, ?, ?)`, channelID, userID, nextOrder, time.Now().Unix()); err != nil {
			return fmt.Errorf("add missing member row: %w", err)
		}
	}
	return nil
}

func (b *bot) deleteDynamicChannel(channelID string) error {
	if _, err := b.s.ChannelDelete(channelID); err != nil && !isDiscordNotFound(err) {
		return fmt.Errorf("delete discord channel: %w", err)
	}
	if err := b.deleteDynamicChannelRecords(channelID); err != nil {
		return err
	}
	log.Printf("deleted dynamic channel %s", channelID)
	return nil
}

func (b *bot) deleteDynamicChannelRecords(channelID string) error {
	if _, err := b.db.Exec(`DELETE FROM dynamic_channels WHERE channel_id = ?`, channelID); err != nil {
		return fmt.Errorf("delete dynamic channel records: %w", err)
	}
	return nil
}
