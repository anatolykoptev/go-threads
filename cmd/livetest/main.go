package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"
	"time"

	threads "github.com/anatolykoptev/go-threads"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cfg := threads.Config{Timeout: 30}

	// Session cookies from env
	cfg.SessionID = os.Getenv("THREADS_SESSION_ID")
	cfg.CSRFToken = os.Getenv("THREADS_CSRF")
	cfg.DSUserID = os.Getenv("THREADS_DS_USER_ID")
	cfg.IGDID = os.Getenv("THREADS_IG_DID")
	cfg.MID = os.Getenv("THREADS_MID")

	if cfg.CSRFToken != "" {
		fmt.Printf("Auth: cookies set (ds_user_id=%s)\n", cfg.DSUserID)
	} else {
		fmt.Println("Auth: anonymous")
	}

	client, err := threads.NewClient(cfg)
	if err != nil {
		log.Fatal(err)
	}

	username := "zuck"
	if len(os.Args) > 1 {
		username = os.Args[1]
	}

	// --- SSR ---
	fmt.Printf("\n=== GetUser @%s ===\n", username)
	user, err := client.GetUser(ctx, username)
	if err != nil {
		fmt.Printf("  ERROR: %v\n", err)
	} else {
		printUser(user)
	}

	fmt.Printf("\n=== GetUserThreads @%s (3) ===\n", username)
	userThreads, err := client.GetUserThreads(ctx, username, 3)
	if err != nil {
		fmt.Printf("  ERROR: %v\n", err)
	} else {
		fmt.Printf("  %d threads\n", len(userThreads))
		for i, t := range userThreads {
			if len(t.Items) > 0 {
				p := t.Items[0]
				fmt.Printf("  [%d] ID=%s likes=%d text=%q\n", i, p.ID, p.LikeCount, trunc(p.Text, 60))
			}
		}
	}

	fmt.Printf("\n=== GetUserReplies @%s (3) ===\n", username)
	replies, err := client.GetUserReplies(ctx, username, 3)
	if err != nil {
		fmt.Printf("  ERROR: %v\n", err)
	} else {
		fmt.Printf("  %d reply threads\n", len(replies))
		for i, t := range replies {
			if len(t.Items) > 0 {
				fmt.Printf("  [%d] @%s: %q\n", i, t.Items[0].Author.Username, trunc(t.Items[0].Text, 60))
			}
		}
	}

	// GetThread from first post
	var threadID string
	if len(userThreads) > 0 && len(userThreads[0].Items) > 0 {
		p := userThreads[0].Items[0]
		threadID = p.ID
		fmt.Printf("\n=== GetThread @%s/post/%s ===\n", username, p.Code)
		main, reps, err := client.GetThread(ctx, username, p.Code)
		if err != nil {
			fmt.Printf("  ERROR: %v\n", err)
		} else {
			fmt.Printf("  Main: %q likes=%d\n", trunc(main.Items[0].Text, 60), main.Items[0].LikeCount)
			fmt.Printf("  Replies: %d\n", len(reps))
		}
	}

	// --- GraphQL ---
	if threadID != "" {
		fmt.Printf("\n=== GetThreadLikers (%s) ===\n", threadID)
		likers, err := client.GetThreadLikers(ctx, threadID, 5)
		if err != nil {
			fmt.Printf("  ERROR: %v\n", err)
		} else {
			fmt.Printf("  %d likers\n", len(likers))
			for i, u := range likers {
				fmt.Printf("  [%d] @%s (%s) verified=%v\n", i, u.Username, u.FullName, u.IsVerified)
			}
		}
	}

	fmt.Println("\n=== SearchUsers ('threads') ===")
	searchResults, err := client.SearchUsers(ctx, "threads", 5)
	if err != nil {
		fmt.Printf("  ERROR: %v\n", err)
	} else {
		fmt.Printf("  %d results\n", len(searchResults))
		for i, u := range searchResults {
			fmt.Printf("  [%d] @%s (%s) verified=%v followers=%d\n", i, u.Username, u.FullName, u.IsVerified, u.FollowerCount)
		}
	}

	fmt.Println("\n=== Done ===")
}

func printUser(u *threads.ThreadsUser) {
	fmt.Printf("  @%s (%s) ID=%s\n", u.Username, u.FullName, u.ID)
	fmt.Printf("  Verified=%v Followers=%d Following=%d\n", u.IsVerified, u.FollowerCount, u.FollowingCount)
	fmt.Printf("  Bio: %s\n", trunc(u.Bio, 80))
	for _, bl := range u.BioLinks {
		fmt.Printf("  Link: %s\n", bl.URL)
	}
}

func trunc(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
