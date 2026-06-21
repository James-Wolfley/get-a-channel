package bot

import (
	"database/sql"
	"errors"
	"fmt"
)

func initDB(db *sql.DB) error {
	_, err := db.Exec(`
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS guild_configs (
  guild_id TEXT PRIMARY KEY,
  trigger_channel_id TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS dynamic_channels (
  channel_id TEXT PRIMARY KEY,
  guild_id TEXT NOT NULL,
  owner_user_id TEXT NOT NULL,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS channel_members (
  channel_id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  join_order INTEGER NOT NULL,
  joined_at INTEGER NOT NULL,
  PRIMARY KEY (channel_id, user_id),
  FOREIGN KEY (channel_id) REFERENCES dynamic_channels(channel_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_dynamic_channels_guild_id ON dynamic_channels(guild_id);
CREATE INDEX IF NOT EXISTS idx_channel_members_channel_order ON channel_members(channel_id, join_order);
`)
	if err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}
	return nil
}

func (b *bot) getTriggerChannel(guildID string) (string, bool, error) {
	var channelID string
	err := b.db.QueryRow(`SELECT trigger_channel_id FROM guild_configs WHERE guild_id = ?`, guildID).Scan(&channelID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read guild config: %w", err)
	}
	return channelID, true, nil
}

func (b *bot) getDynamicChannel(channelID string) (trackedChannel, bool, error) {
	var ch trackedChannel
	err := b.db.QueryRow(`SELECT channel_id, guild_id, owner_user_id FROM dynamic_channels WHERE channel_id = ?`, channelID).Scan(&ch.ChannelID, &ch.GuildID, &ch.OwnerUserID)
	if errors.Is(err, sql.ErrNoRows) {
		return trackedChannel{}, false, nil
	}
	if err != nil {
		return trackedChannel{}, false, fmt.Errorf("read dynamic channel: %w", err)
	}
	return ch, true, nil
}

func (b *bot) getAllDynamicChannels() ([]trackedChannel, error) {
	rows, err := b.db.Query(`SELECT channel_id, guild_id, owner_user_id FROM dynamic_channels`)
	if err != nil {
		return nil, fmt.Errorf("query dynamic channels: %w", err)
	}
	defer rows.Close()

	var channels []trackedChannel
	for rows.Next() {
		var ch trackedChannel
		if err := rows.Scan(&ch.ChannelID, &ch.GuildID, &ch.OwnerUserID); err != nil {
			return nil, fmt.Errorf("scan dynamic channel: %w", err)
		}
		channels = append(channels, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dynamic channels: %w", err)
	}
	return channels, nil
}

func (b *bot) getOrderedMembers(channelID string) ([]memberOrder, error) {
	rows, err := b.db.Query(`SELECT user_id, join_order FROM channel_members WHERE channel_id = ? ORDER BY join_order ASC`, channelID)
	if err != nil {
		return nil, fmt.Errorf("query ordered members: %w", err)
	}
	defer rows.Close()

	var members []memberOrder
	for rows.Next() {
		var member memberOrder
		if err := rows.Scan(&member.UserID, &member.JoinOrder); err != nil {
			return nil, fmt.Errorf("scan ordered member: %w", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ordered members: %w", err)
	}
	return members, nil
}

func (b *bot) nextJoinOrder(channelID string) (int64, error) {
	var next sql.NullInt64
	if err := b.db.QueryRow(`SELECT COALESCE(MAX(join_order), 0) + 1 FROM channel_members WHERE channel_id = ?`, channelID).Scan(&next); err != nil {
		return 0, fmt.Errorf("read next join order: %w", err)
	}
	if !next.Valid {
		return 1, nil
	}
	return next.Int64, nil
}
