# Get A Channel Bot

A Go Discord bot that creates temporary voice channels when members join a configured trigger voice channel.

## Behavior

- An admin joins a voice channel and runs `/set-get-a-channel`.
- When a member joins that trigger channel, the bot creates a new voice channel in the same category.
- The new channel is named `{DisplayName}'s Channel`.
- The member is moved into the new channel.
- Anyone can join the dynamic channel; no custom permission overrides are added.
- Ownership only controls the channel name.
- If the named owner leaves, ownership transfers to the earliest remaining member by join order.
- Empty dynamic channels are deleted immediately.
- Dynamic channels and join order are persisted in SQLite.
- A periodic cleanup reconciles the database with Discord in case channels were manually deleted or events were missed.

## Slash Commands

- `/set-get-a-channel`: admin-only; sets your current voice channel as this server's trigger channel.
- `/get-a-channel-status`: admin-only; shows the configured trigger channel and tracked dynamic channel count.
- `/unset-get-a-channel`: admin-only; removes this server's trigger channel config.

Commands are registered globally by default and also registered immediately to the dev guild `605392489439821835`.

## Discord Setup

Create a Discord application and bot at <https://discord.com/developers/applications>.

Enable these bot gateway intents:

- Server Members Intent
- Server Voice States Intent

Invite the bot with scopes:

- `bot`
- `applications.commands`

Required bot permissions:

- View Channels
- Manage Channels
- Move Members
- Connect

The bot also needs access to the category/channel where the trigger channel lives.

## Configuration

Environment variables:

- `DISCORD_TOKEN`: required Discord bot token.
- `DATABASE_PATH`: SQLite DB path, default `/data/bot.db`.
- `CLEANUP_INTERVAL_SECONDS`: cleanup interval, default `300`.
- `DEV_GUILD_ID`: guild for instant command registration, default `605392489439821835`. Set to empty to disable.
- `REGISTER_GLOBAL_COMMANDS`: whether to register global commands, default `true`.

## Run With Podman Compose

Create a `.env` file:

```env
DISCORD_TOKEN=your_token_here
```

Start the bot:

```sh
podman compose up -d --build
```

View logs:

```sh
podman compose logs -f
```

SQLite data is stored in `./data/bot.db`.

## Run With Podman

Build the image:

```sh
podman build -t get-a-channel-bot .
```

Run the bot:

```sh
mkdir -p data
podman run -d \
  --name get-a-channel-bot \
  --restart unless-stopped \
  --env DISCORD_TOKEN=your_token_here \
  --volume ./data:/data:Z \
  get-a-channel-bot
```

## Run Locally

```sh
export DISCORD_TOKEN=your_token_here
export DATABASE_PATH=./data/bot.db
go run .
```
