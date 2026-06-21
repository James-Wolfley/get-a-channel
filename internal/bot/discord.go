package bot

import (
	"errors"
	"fmt"

	"github.com/bwmarrin/discordgo"
)

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
