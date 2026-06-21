package bot

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	_ "modernc.org/sqlite"
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

func Run() error {
	token := envString("DISCORD_TOKEN", "")
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
