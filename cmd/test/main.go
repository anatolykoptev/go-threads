package main

import (
	"context"
	"fmt"
	"log"
	"time"

	threads "github.com/anatolykoptev/go-threads"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := threads.NewClient(threads.Config{Timeout: 15})
	if err != nil {
		log.Fatal(err)
	}

	// Test GetThread — instagram post DU_lhpBkinl
	fmt.Println("=== GetThread ===")
	main, replies, err := client.GetThread(ctx, "instagram", "DU_lhpBkinl")
	if err != nil {
		log.Fatal("GetThread:", err)
	}

	fmt.Printf("Main thread: %d items\n", len(main.Items))
	for i, p := range main.Items {
		text := p.Text
		if len(text) > 80 {
			text = text[:80] + "..."
		}
		fmt.Printf("  [%d] @%s: %s (likes=%d, replies=%d)\n", i, p.Author.Username, text, p.LikeCount, p.ReplyCount)
	}

	fmt.Printf("\nReplies: %d threads\n", len(replies))
	for i, r := range replies {
		if i >= 5 {
			fmt.Printf("  ... and %d more\n", len(replies)-5)
			break
		}
		for _, p := range r.Items {
			text := p.Text
			if len(text) > 60 {
				text = text[:60] + "..."
			}
			fmt.Printf("  [%d] @%s: %s (likes=%d)\n", i, p.Author.Username, text, p.LikeCount)
		}
	}
}
