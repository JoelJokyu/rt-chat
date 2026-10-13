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
	//messageHandler := messages.NewMessageHandler(messageService)

	joinCodeRepository := joincodes.NewJoinCodeRepository(db)
	joinCodeService := joincodes.NewJoinCodeService(joinCodeRepository)
	joinCodeHandler := joincodes.NewJoinCodeHandler(joinCodeService)

	membershipRepository := memberships.NewMembershipRepository(db)
	membershipService := memberships.NewMembershipService(membershipRepository)
	membershipHandler := memberships.NewMembershipHandler(membershipService)

	hub := realtime.NewHub()

	messageHandler := messages.NewMessageHandler(messageService, hub)
	realtimeHandler := realtime.NewHandler(membershipService, messageService, hub)

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
