# Get A Channel Bot

A Discord bot that creates temporary voice channels when members join a configured trigger voice channel.

## Get Started

Add the public bot to your Discord server:

[Add Get A Channel Bot to your server](https://discord.com/oauth2/authorize?client_id=1516926194040443035&permissions=17826832&scope=bot%20applications.commands)

The bot needs these permissions:

- View Channels
- Manage Channels
- Move Members
- Connect

The bot must also be able to see and connect to the voice channel you want to use as the trigger channel.

## Server Setup

1. Add the bot to your server.
2. Join the voice channel you want people to use as the trigger channel.
3. Run `/set-get-a-channel`.
4. When someone joins that voice channel, the bot will create a temporary voice channel for them and move them into it.

Slash commands can take time to appear after the bot is first added to a server.

## Slash Commands

- `/set-get-a-channel`: sets your current voice channel as this server's trigger channel.
- `/get-a-channel-status`: shows the configured trigger channel and active temporary channel count.
- `/unset-get-a-channel`: removes this server's trigger channel configuration.

These commands require the `Manage Server` permission.

## Run Your Own Instance

You can also run your own instance with your own Discord application and token.

Create a Discord application and bot at <https://discord.com/developers/applications>.

Enable this gateway intent for the bot:

- Server Voice States Intent

Invite your bot with these scopes:

- `bot`
- `applications.commands`

Give your bot these permissions:

- View Channels
- Manage Channels
- Move Members
- Connect

Commands are registered globally when the bot starts. Discord can take time to show new or changed global commands.

## Container Image

The published image is available from GitHub Container Registry:

```text
ghcr.io/james-wolfley/get-a-channel:latest
ghcr.io/james-wolfley/get-a-channel:v1.0.0
```

Run the published image with Podman:

```sh
mkdir -p data
podman run -d \
  --name get-a-channel-bot \
  --restart unless-stopped \
  --env DISCORD_TOKEN=your_token_here \
  --volume ./data:/data:Z \
  ghcr.io/james-wolfley/get-a-channel:latest
```

SQLite data is stored in `./data/bot.db`.

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

Start the bot from source:

```sh
podman compose up -d --build
```

View logs:

```sh
podman compose logs -f
```

SQLite data is stored in `./data/bot.db`.

## Build Locally

Build the image:

```sh
podman build -t get-a-channel-bot .
```

Run the locally built image:

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

## How It Works

- Temporary channels are created in the same category as the trigger voice channel.
- The channel is named `{DisplayName}'s Channel`.
- Other members can join the temporary channel normally.
- If the named owner leaves, the channel is renamed for the earliest remaining member.
- Empty temporary channels are deleted automatically.
- Channel state is stored in SQLite so the bot can recover after restarts.
