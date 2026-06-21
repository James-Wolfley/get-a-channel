package main

import (
	"log"

	"get-a-channel-bot/internal/bot"
)

func main() {
	if err := bot.Run(); err != nil {
		log.Fatal(err)
	}
}
