package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Arandomsprinkle/gator/internal/database"
	"github.com/google/uuid"
)

func middlewareLoggedIn(handler func(s *state, cmd command, user database.User) error) func(*state, command) error {
	return func(s *state, cmd command) error {
		user, err := s.db.GetUser(context.Background(), s.cfg.CurrentUserName)
		if err != nil {
			return fmt.Errorf("error getting current user: %w", err)
		}
		return handler(s, cmd, user)
	}
}

func handlerLogin(s *state, cmd command) error {
	if len(cmd.Args) != 1 {
		return fmt.Errorf("usage: %s <name>", cmd.Name)
	}
	name := cmd.Args[0]
	_, err := s.db.GetUser(context.Background(), name)
	if err != nil {
		fmt.Printf("User '%s' does not exist\n", name)
		return fmt.Errorf("user does not exist")
	}
	err = s.cfg.SetUser(name)
	if err != nil {
		return fmt.Errorf("couldn't set current user: %w", err)
	}
	fmt.Println("User switched successfully!")
	return nil
}

func handlerRegister(s *state, cmd command) error {
	if len(cmd.Args) != 1 {
		return fmt.Errorf("usage: %s <name>", cmd.Name)
	}
	name := cmd.Args[0]
	params := database.CreateUserParams{
		ID:        uuid.New(),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Name:      name,
	}

	_, err := s.db.CreateUser(context.Background(), params)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			fmt.Printf("User '%s' already exists\n", name)
			return fmt.Errorf("user already exists")
		}
		return fmt.Errorf("couldn't register user: %w", err)
	}
	s.cfg.CurrentUserName = name
	if err := s.cfg.SetUser(name); err != nil {
		return fmt.Errorf("couldn't save config: %w", err)
	}

	fmt.Println("User registered successfully!")
	return nil
}

func handlerReset(s *state, cmd command) error {
	err := s.db.Reset(context.Background())
	if err != nil {
		return fmt.Errorf("couldn't reset user list: %w", err)
	}
	fmt.Println("Users reset successfully!")
	return nil
}

func handlerGetUsers(s *state, cmd command) error {
	users, err := s.db.GetUsers(context.Background())
	if err != nil {
		return fmt.Errorf("couldn't retrieve user list: %w", err)
	}
	for _, user := range users {
		if user.Name == s.cfg.CurrentUserName {
			fmt.Printf("* %s (current)\n", user.Name)
		} else {
			fmt.Printf("* %s\n", user.Name)
		}
	}
	fmt.Println("Users found successfully!")
	return nil
}

type RSSFeed struct {
	Channel struct {
		Title       string    `xml:"title"`
		Link        string    `xml:"link"`
		Description string    `xml:"description"`
		Item        []RSSItem `xml:"item"`
	} `xml:"channel"`
}

type RSSItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
}

func handlerAgg(s *state, cmd command) error {
	if len(cmd.Args) != 1 {
		return fmt.Errorf("agg needs exactly one argument")
	}
	timeBetweenRequests, err := time.ParseDuration(cmd.Args[0])
	if err != nil {
		return fmt.Errorf("argument needs to be in a #s format for time")
	}
	minInterval := 1 * time.Second
	if timeBetweenRequests < minInterval {
		fmt.Printf("Warning: Requested interval %v is too short. Using %v instead.\n",
			timeBetweenRequests, minInterval)
		timeBetweenRequests = minInterval
	}
	fmt.Printf("Collecting feeds every %v\n", timeBetweenRequests)

	ticker := time.NewTicker(timeBetweenRequests)
	for ; ; <-ticker.C {
		if err := scrapeFeeds(s); err != nil {
			fmt.Printf("Error scraping feeds: %v\n", err)
		}
	}
	//Should not reach return.
	return nil
}

func fetchFeed(ctx context.Context, feedURL string) (*RSSFeed, error) {
	request, err := http.NewRequestWithContext(ctx, "GET", feedURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "gator")
	client := &http.Client{}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	var feed RSSFeed
	err = xml.Unmarshal(bodyBytes, &feed)
	if err != nil {
		return nil, err
	}
	feed.Channel.Title = html.UnescapeString(feed.Channel.Title)
	feed.Channel.Description = html.UnescapeString(feed.Channel.Description)
	for i := range feed.Channel.Item {
		feed.Channel.Item[i].Title = html.UnescapeString(feed.Channel.Item[i].Title)
		feed.Channel.Item[i].Description = html.UnescapeString(feed.Channel.Item[i].Description)
	}
	return &feed, nil
}

func handlerAddFeed(s *state, args command, user database.User) error {
	ctx := context.Background()
	if len(args.Args) < 2 {
		return fmt.Errorf("not enough arguments")
	}
	feedName := args.Args[0]
	feedURL := args.Args[1]
	_, err := fetchFeed(ctx, feedURL)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	params := database.CreateFeedParams{
		ID:        uuid.New(),
		CreatedAt: now,
		UpdatedAt: now,
		Name:      feedName,
		Url:       feedURL,
		UserID:    user.ID,
	}
	feed, err := s.db.CreateFeed(ctx, params)
	if err != nil {
		return err
	}
	fmt.Printf("Feed '%s' added successfully\n", feed.Name)
	feedFollow, err := s.db.CreateFeedFollow(ctx, database.CreateFeedFollowParams{
		ID:        uuid.New(),
		CreatedAt: now,
		UpdatedAt: now,
		UserID:    user.ID,
		FeedID:    feed.ID,
	})
	if err != nil {
		return fmt.Errorf("error following feed: %w", err)
	}

	fmt.Printf("You are now following '%s'\n", feedFollow[0].FeedName)

	return nil
}

func handlerFeeds(s *state, args command) error {
	feeds, err := s.db.ListFeedsWithUser(context.Background())
	if err != nil {
		return fmt.Errorf("couldn't retrieve user list: %w", err)
	}
	for _, feed := range feeds {
		fmt.Printf("* %s : %s : %s \n", feed.Name, feed.Name_2, feed.Url)
	}
	return nil
}

func handlerFollow(s *state, args command, user database.User) error {
	ctx := context.Background()
	if len(args.Args) < 1 {
		return fmt.Errorf("not enough arguments")
	}
	feedName := args.Args[0]
	feedURL := args.Args[1]
	feed, err := s.db.GetFeedByURL(ctx, feedURL)
	if err != nil {
		feedID := uuid.New()
		now := time.Now().UTC()
		feed, err = s.db.CreateFeed(ctx, database.CreateFeedParams{
			ID:        feedID,
			CreatedAt: now,
			UpdatedAt: now,
			Name:      feedName,
			Url:       feedURL,
			UserID:    user.ID,
		})
		if err != nil {
			return fmt.Errorf("error creating feed: %w", err)
		}
	}
	followID := uuid.New()
	now := time.Now().UTC()
	feedFollow, err := s.db.CreateFeedFollow(ctx, database.CreateFeedFollowParams{
		ID:        followID,
		CreatedAt: now,
		UpdatedAt: now,
		UserID:    user.ID,
		FeedID:    feed.ID,
	})
	if err != nil {
		return fmt.Errorf("error creating feed follow: %w", err)
	}
	fmt.Printf("You are now following '%s'\n", feedFollow[0].FeedName)
	return nil
}

func handlerFollowing(s *state, args command, user database.User) error {
	ctx := context.Background()
	feedFollows, err := s.db.GetFeedFollowsForUser(ctx, user.ID)
	if err != nil {
		return fmt.Errorf("error getting feed: %w", err)
	}
	if len(feedFollows) == 0 {
		fmt.Println("You're not following any feeds")
		return nil
	}
	for _, follow := range feedFollows {
		fmt.Printf("* %s\n", follow.FeedName)
	}
	return nil
}

func handlerUnfollow(s *state, args command, user database.User) error {
	ctx := context.Background()
	if len(args.Args) < 1 {
		return fmt.Errorf("not enough arguments")
	}
	feedURL := args.Args[0]
	feed, err := s.db.GetFeedByURL(ctx, feedURL)
	if err != nil {
		return fmt.Errorf("error getting feed: %w", err)
	}
	unfollowFeed := database.DeleteFeedFollowRecordParams{
		ID:  user.ID,
		Url: feedURL,
	}
	err = s.db.DeleteFeedFollowRecord(ctx, unfollowFeed)
	if err != nil {
		// Check if it's a "no rows" type of error
		if strings.Contains(err.Error(), "no rows") {
			return fmt.Errorf("no feed follow found to delete")
		}
		return fmt.Errorf("error deleting feed follow: %w", err)
	}
	fmt.Printf("You are no longer following '%s'\n", feed.Name)
	return nil
}

func parsePublishedAt(pubDateStr string) (time.Time, error) {
	layouts := []string{
		time.RFC1123Z,          // "Mon, 02 Jan 2006 15:04:05 -0700"
		time.RFC1123,           // "Mon, 02 Jan 2006 15:04:05 MST"
		time.RFC3339,           // "2006-01-02T15:04:05Z07:00"
		"2006-01-02T15:04:05Z", // Common ISO8601 without timezone
		"2006-01-02T15:04:05",  // ISO8601 without timezone and Z
		"Mon, 02 Jan 2006",     // Date only
		"2006-01-02",           // ISO date only
		"02 Jan 2006 15:04:05 -0700",
		"02 Jan 2006 15:04:05",
		"January 2, 2006",
		"Jan 2, 2006",
	}

	for _, layout := range layouts {
		t, err := time.Parse(layout, pubDateStr)
		if err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse date: %s", pubDateStr)
}

func scrapeFeeds(s *state) error {
	ctx := context.Background()
	feed, err := s.db.GetNextFeedToFetch(ctx)
	if err != nil {
		return fmt.Errorf("error getting next feed: %w", err)
	}
	fmt.Printf("Fetching feed: %s\n", feed.Url)
	_, err = s.db.MarkFeedFetched(ctx, feed.ID)
	if err != nil {
		return fmt.Errorf("error marking feed as fetched: %w", err)
	}
	rssFeed, err := fetchFeed(ctx, feed.Url)
	if err != nil {
		return fmt.Errorf("error fetching feed: %w", err)
	}
	for _, item := range rssFeed.Channel.Item {
		publishedAt, err := parsePublishedAt(item.PubDate)
		if err != nil {
			fmt.Printf("Error parsing date %s: %v\n Skipping to next.", item.PubDate, err)
			continue
		}
		_, err = s.db.CreatePost(ctx, database.CreatePostParams{
			ID:          uuid.New(),
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
			Title:       item.Title,
			Url:         item.Link,
			Description: item.Description,
			PublishedAt: publishedAt,
			FeedID:      feed.ID,
		})
		if err != nil {
			if strings.Contains(err.Error(), "duplicate key value violates unique constraint") {
				fmt.Println("Duplicate key found, passing")
				continue
			}
			fmt.Printf("Error creating post: %v\n", err)
			continue
		}
	}
	return nil
}

func handlerBrowse(s *state, cmd command, user database.User) error {
	ctx := context.Background()
	var limit int32 = 2 // Default
	if len(cmd.Args) > 0 {
		i, err := strconv.Atoi(cmd.Args[0])
		if err == nil {
			limit = int32(i)
		}
	}
	postParams := database.GetPostsForUserParams{
		UserID: user.ID,
		Limit:  limit,
	}
	posts, err := s.db.GetPostsForUser(ctx, postParams)
	if err != nil {
		return fmt.Errorf("error getting posts: %w", err)
	}
	for _, post := range posts {
		fmt.Println("\n---------------------------")
		fmt.Printf("Title: %s\n", post.Title)
		fmt.Printf("Published: %s\n", post.PublishedAt.Format("2006-01-02 15:04:05"))
		fmt.Printf("URL: %s\n", post.Url)
		fmt.Println("\nDescription:")
		fmt.Println(wrapText(post.Description, 80))
		fmt.Println("---------------------------")
	}
	return nil
}

func wrapText(text string, lineWidth int) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}

	wrapped := words[0]
	spaceLeft := lineWidth - len(wrapped)

	for _, word := range words[1:] {
		if len(word)+1 > spaceLeft {
			wrapped += "\n" + word
			spaceLeft = lineWidth - len(word)
		} else {
			wrapped += " " + word
			spaceLeft -= len(word) + 1
		}
	}

	return wrapped
}
