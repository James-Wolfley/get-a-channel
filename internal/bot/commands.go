package bot

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/bwmarrin/discordgo"
)

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

func int64Ptr(value int64) *int64 {
	return &value
}
