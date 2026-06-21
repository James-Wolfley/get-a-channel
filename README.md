# Get A Channel Bot

A Discord bot that creates temporary voice channels when members join a configured trigger voice channel.

## How It Works

- A server admin joins a voice channel and runs `/set-get-a-channel`.
- When a member joins that voice channel, the bot creates a new voice channel in the same category.
- The member is moved into the new channel automatically.
- The channel is named `{DisplayName}'s Channel`.
- Other members can join the temporary channel normally.
- If the named owner leaves, the channel is renamed for the earliest remaining member.
- Empty temporary channels are deleted automatically.
- Channel state is stored in SQLite so the bot can recover after restarts.

## Slash Commands

- `/set-get-a-channel`: sets your current voice channel as this server's trigger channel.
- `/get-a-channel-status`: shows the configured trigger channel and active temporary channel count.
- `/unset-get-a-channel`: removes this server's trigger channel configuration.

These commands require the `Manage Server` permission.

## Discord Setup

Create a Discord application and bot at <https://discord.com/developers/applications>.

Enable this gateway intent for the bot:

- Server Voice States Intent

Invite the bot with these scopes:

- `bot`
- `applications.commands`

Give the bot these permissions:

- View Channels
- Manage Channels
- Move Members
- Connect

The bot must also be able to see and connect to the voice channel used as the trigger channel.

Slash commands are registered globally when the bot starts. Discord can take time to show new or changed global commands.

## Configuration

Environment variables:

- `DISCORD_TOKEN`: required Discord bot token.
- `DATABASE_PATH`: SQLite database path. Defaults to `/data/bot.db`.
- `CLEANUP_INTERVAL_SECONDS`: cleanup interval for reconciling Discord state. Defaults to `300`.

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
mkdir -p data
export DISCORD_TOKEN=your_token_here
export DATABASE_PATH=./data/bot.db
go run .
```
