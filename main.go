package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	_ "modernc.org/sqlite"
)

const (
	defaultDatabasePath        = "/data/bot.db"
	defaultCleanupIntervalSecs = 300
)

type bot struct {
	db *sql.DB
	s  *discordgo.Session
	mu sync.Mutex
}

type trackedChannel struct {
	ChannelID   string
	GuildID     string
	OwnerUserID string
}

type memberOrder struct {
	UserID    string
	JoinOrder int64
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	token := strings.TrimSpace(os.Getenv("DISCORD_TOKEN"))
	if token == "" {
		return errors.New("DISCORD_TOKEN is required")
	}

	dbPath := envString("DATABASE_PATH", defaultDatabasePath)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	if err := initDB(db); err != nil {
		return err
	}

	s, err := discordgo.New("Bot " + token)
	if err != nil {
		return fmt.Errorf("create discord session: %w", err)
	}

	s.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildVoiceStates

	b := &bot{db: db, s: s}
	s.AddHandler(b.handleInteraction)
	s.AddHandler(b.handleVoiceStateUpdate)

	if err := s.Open(); err != nil {
		return fmt.Errorf("open discord session: %w", err)
	}
	defer s.Close()

	if err := b.registerCommands(); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cleanupInterval := time.Duration(envInt("CLEANUP_INTERVAL_SECONDS", defaultCleanupIntervalSecs)) * time.Second
	go b.cleanupLoop(ctx, cleanupInterval)

	log.Println("get-a-channel bot is running")
	<-ctx.Done()
	log.Println("shutting down")
	return nil
}

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

func (b *bot) registerCommands() error {
	commands := []*discordgo.ApplicationCommand{
		{
			Name:                     "set-get-a-channel",
			Description:              "Set your current voice channel as the get-a-channel trigger.",
			DefaultMemberPermissions: int64Ptr(discordgo.PermissionManageGuild),
		},
		{
			Name:                     "get-a-channel-status",
			Description:              "Show get-a-channel configuration and tracked channel count.",
			DefaultMemberPermissions: int64Ptr(discordgo.PermissionManageGuild),
		},
		{
			Name:                     "unset-get-a-channel",
			Description:              "Remove this server's get-a-channel trigger channel.",
			DefaultMemberPermissions: int64Ptr(discordgo.PermissionManageGuild),
		},
	}

	appID, err := b.applicationID()
	if err != nil {
		return err
	}
	for _, cmd := range commands {
		if _, err := b.s.ApplicationCommandCreate(appID, "", cmd); err != nil {
			return fmt.Errorf("register global command %s: %w", cmd.Name, err)
		}
	}
	log.Println("registered global commands")

	return nil
}

func (b *bot) applicationID() (string, error) {
	if b.s.State != nil && b.s.State.User != nil && b.s.State.User.ID != "" {
		return b.s.State.User.ID, nil
	}

	user, err := b.s.User("@me")
	if err != nil {
		return "", fmt.Errorf("read bot user: %w", err)
	}
	if user.ID == "" {
		return "", errors.New("bot user ID was empty")
	}
	return user.ID, nil
}

func (b *bot) handleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand || i.GuildID == "" {
		return
	}

	var content string
	var err error

	switch i.ApplicationCommandData().Name {
	case "set-get-a-channel":
		content, err = b.setGetAChannel(i)
	case "get-a-channel-status":
		content, err = b.status(i.GuildID)
	case "unset-get-a-channel":
		content, err = b.unsetGetAChannel(i.GuildID)
	default:
		return
	}

	if err != nil {
		content = "Error: " + err.Error()
	}

	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	}); err != nil {
		log.Printf("respond to interaction: %v", err)
	}
}

func (b *bot) setGetAChannel(i *discordgo.InteractionCreate) (string, error) {
	voiceState, err := b.findVoiceState(i.GuildID, i.Member.User.ID)
	if err != nil {
		return "", err
	}

	channel, err := b.s.Channel(voiceState.ChannelID)
	if err != nil {
		return "", fmt.Errorf("read voice channel: %w", err)
	}
	if channel.Type != discordgo.ChannelTypeGuildVoice {
		return "", errors.New("you must be connected to a voice channel")
	}

	_, err = b.db.Exec(`
INSERT INTO guild_configs (guild_id, trigger_channel_id, updated_at)
VALUES (?, ?, ?)
ON CONFLICT(guild_id) DO UPDATE SET trigger_channel_id = excluded.trigger_channel_id, updated_at = excluded.updated_at
`, i.GuildID, voiceState.ChannelID, time.Now().Unix())
	if err != nil {
		return "", fmt.Errorf("save config: %w", err)
	}

	return fmt.Sprintf("Configured <#%s> as the get-a-channel trigger.", voiceState.ChannelID), nil
}

func (b *bot) status(guildID string) (string, error) {
	triggerID, ok, err := b.getTriggerChannel(guildID)
	if err != nil {
		return "", err
	}

	var count int
	if err := b.db.QueryRow(`SELECT COUNT(*) FROM dynamic_channels WHERE guild_id = ?`, guildID).Scan(&count); err != nil {
		return "", fmt.Errorf("count dynamic channels: %w", err)
	}

	if !ok {
		return fmt.Sprintf("No trigger channel configured. Tracked dynamic channels: %d.", count), nil
	}
	return fmt.Sprintf("Trigger channel: <#%s>. Tracked dynamic channels: %d.", triggerID, count), nil
}

func (b *bot) unsetGetAChannel(guildID string) (string, error) {
	if _, err := b.db.Exec(`DELETE FROM guild_configs WHERE guild_id = ?`, guildID); err != nil {
		return "", fmt.Errorf("remove config: %w", err)
	}
	return "Removed this server's get-a-channel trigger. Existing tracked channels are unchanged.", nil
}

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

func (b *bot) cleanupLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	b.cleanupOnce()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.cleanupOnce()
		}
	}
}

func (b *bot) cleanupOnce() {
	b.mu.Lock()
	defer b.mu.Unlock()

	channels, err := b.getAllDynamicChannels()
	if err != nil {
		log.Printf("cleanup list channels: %v", err)
		return
	}

	for _, ch := range channels {
		if err := b.reconcileDynamicChannel(ch); err != nil {
			log.Printf("cleanup channel %s: %v", ch.ChannelID, err)
		}
	}
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

func (b *bot) connectedMembers(guildID, channelID string) (map[string]bool, bool, error) {
	if _, err := b.s.Channel(channelID); err != nil {
		if isDiscordNotFound(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read discord channel: %w", err)
	}

	guild, err := b.s.State.Guild(guildID)
	if err != nil {
		guild, err = b.s.Guild(guildID)
		if err != nil {
			return nil, true, fmt.Errorf("read guild: %w", err)
		}
	}

	connected := make(map[string]bool)
	for _, vs := range guild.VoiceStates {
		if vs.ChannelID == channelID {
			connected[vs.UserID] = true
		}
	}
	return connected, true, nil
}

func (b *bot) findVoiceState(guildID, userID string) (*discordgo.VoiceState, error) {
	guild, err := b.s.State.Guild(guildID)
	if err != nil {
		return nil, errors.New("could not read guild voice states; try again after the bot is fully online")
	}
	for _, vs := range guild.VoiceStates {
		if vs.UserID == userID && vs.ChannelID != "" {
			return vs, nil
		}
	}
	return nil, errors.New("you must be connected to a voice channel")
}

func (b *bot) channelName(guildID, userID string) string {
	name := b.displayName(guildID, userID)
	if name == "" {
		name = "Someone"
	}
	return name + "'s Channel"
}

func (b *bot) displayName(guildID, userID string) string {
	if member, err := b.s.State.Member(guildID, userID); err == nil {
		return memberDisplayName(member)
	}
	if member, err := b.s.GuildMember(guildID, userID); err == nil {
		return memberDisplayName(member)
	}
	if user, err := b.s.User(userID); err == nil {
		if user.GlobalName != "" {
			return user.GlobalName
		}
		return user.Username
	}
	return userID
}

func memberDisplayName(member *discordgo.Member) string {
	if member.Nick != "" {
		return member.Nick
	}
	if member.User != nil {
		if member.User.GlobalName != "" {
			return member.User.GlobalName
		}
		return member.User.Username
	}
	return ""
}

func isDiscordNotFound(err error) bool {
	var restErr *discordgo.RESTError
	if errors.As(err, &restErr) && restErr.Response != nil {
		return restErr.Response.StatusCode == 404
	}
	return false
}

func envString(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		log.Printf("invalid positive integer %s=%q, using %d", key, value, fallback)
		return fallback
	}
	return parsed
}

func int64Ptr(value int64) *int64 {
	return &value
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
