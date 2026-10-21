package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"rtchat/internal/auth"
	"rtchat/internal/channels"
	"rtchat/internal/database"
	"rtchat/internal/joincodes"
	"rtchat/internal/memberships"
	"rtchat/internal/messages"
	"rtchat/internal/realtime"
	"rtchat/internal/servers"
	"rtchat/internal/users"

	"github.com/redis/go-redis/v9"
)

func main() {

	ctx := context.Background()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	db, err := database.NewPool(ctx, databaseURL)
	if err != nil {
		log.Printf("Database startup failed: %v", err)
		return
	}
	defer db.Close()

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		log.Printf("REDIS_ADDR is not set")
		return
	}

	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = redisClient.Ping(pingCtx).Err()
	cancel()

	if err != nil {
		_ = redisClient.Close()
		log.Printf("Redis startup failed: %v", err)
		return
	}
	defer redisClient.Close()

	log.Println("Connected to Redis")
	eventBus := realtime.NewRedisEventBus(redisClient)

	userRepository := users.NewUserRepository(db)
	userService := users.NewUserService(userRepository)
	userHandler := users.NewUserHandler(userService)

	sessionRepository := auth.NewSessionRepository(db)
	sessionService := auth.NewSessionService(sessionRepository)
	authService := auth.NewAuthService(userRepository, sessionService)
	authHandler := auth.NewAuthHandler(authService)

	serverRepository := servers.NewServerRepository(db)
	serverService := servers.NewServerService(serverRepository)
	serverHandler := servers.NewServerHandler(serverService)

	channelRepository := channels.NewChannelRepository(db)
	channelService := channels.NewChannelService(channelRepository)
	channelHandler := channels.NewChannelHandler(channelService)

	messageRepository := messages.NewMessageRepository(db)
	messageService := messages.NewMessageService(messageRepository)

	joinCodeRepository := joincodes.NewJoinCodeRepository(db)
	joinCodeService := joincodes.NewJoinCodeService(joinCodeRepository)
	joinCodeHandler := joincodes.NewJoinCodeHandler(joinCodeService)

	membershipRepository := memberships.NewMembershipRepository(db)
	membershipService := memberships.NewMembershipService(membershipRepository)
	membershipHandler := memberships.NewMembershipHandler(membershipService)

	hub := realtime.NewHub()

	subscriberCtx, stopSubscriber := context.WithCancel(context.Background())
	defer stopSubscriber()
	go func() {
		if err := eventBus.Subscribe(subscriberCtx, hub); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("Redis message subscription stopped: %v", err)
		}
	}()

	messageHandler := messages.NewMessageHandler(messageService, eventBus)
	realtimeHandler := realtime.NewHandler(membershipService, messageService, hub, eventBus)

	mux := http.NewServeMux()
	setupRoutes(mux, userHandler, authHandler, serverHandler, channelHandler, messageHandler, joinCodeHandler, membershipHandler, realtimeHandler, sessionService)

	shutdownSignals := make(chan os.Signal, 1)
	signal.Notify(shutdownSignals, os.Interrupt)
	defer signal.Stop(shutdownSignals)

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
	serverErr := make(chan error, 1)

	log.Println("Starting server on :8080")

	go func() {
		serverErr <- server.ListenAndServe()
	}()

	select {
	case <-shutdownSignals:
		log.Println("Shutdown signal received")

	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("Could not listen on :8080: %v", err)
			return
		}

		log.Println("Server stopped")
		return
	}

	shutdownCtx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Shutdown error: %v", err)
	}

	log.Println("Server stopped")
}
