package main

import (
	"encoding/json"
	"net/http"
	"rtchat/internal/auth"
	"rtchat/internal/channels"
	"rtchat/internal/joincodes"
	"rtchat/internal/memberships"
	"rtchat/internal/messages"
	"rtchat/internal/realtime"
	"rtchat/internal/servers"
	"rtchat/internal/users"
)

func setupRoutes(mux *http.ServeMux, userHandler *users.UserHandler, authHandler *auth.AuthHandler, serverHandler *servers.ServerHandler, channelHandler *channels.ChannelHandler, messageHandler *messages.MessageHandler, joinCodeHandler *joincodes.JoinCodeHandler, membershipHandler *memberships.MembershipHandler, realtimeHandler *realtime.Handler, sessionService *auth.SessionService) {
	requireAuth := auth.AuthMiddleware(sessionService)

	mux.HandleFunc("GET /health", healthHandler)
	mux.HandleFunc("POST /users", userHandler.RegisterUserHandler)
	mux.HandleFunc("POST /login", authHandler.LoginUserHandler)
	mux.Handle("POST /logout", requireAuth(http.HandlerFunc(authHandler.LogoutHandler)))
	mux.Handle("POST /servers", requireAuth(http.HandlerFunc(serverHandler.CreateServerHandler)))
	mux.Handle("GET /servers/{id}", requireAuth(http.HandlerFunc(serverHandler.GetServerForMemberHandler)))
	mux.Handle("DELETE /servers/{id}", requireAuth(http.HandlerFunc(serverHandler.DeleteServerHandler)))
	mux.Handle("PATCH /servers/{id}", requireAuth(http.HandlerFunc(serverHandler.UpdateServerNameHandler)))
	mux.Handle("GET /servers", requireAuth(http.HandlerFunc(serverHandler.ListForMemberHandler)))
	mux.Handle("POST /servers/{id}/channels", requireAuth(http.HandlerFunc(channelHandler.CreateChannelHandler)))
	mux.Handle("GET /servers/{id}/channels", requireAuth(http.HandlerFunc(channelHandler.ListForMemberHandler)))
	mux.Handle("POST /channels/{id}/messages", requireAuth(http.HandlerFunc(messageHandler.CreateMessageHandler)))
	mux.Handle("GET /channels/{id}/messages", requireAuth(http.HandlerFunc(messageHandler.ListMessagesHandler)))
	mux.Handle("PATCH /messages/{id}", requireAuth(http.HandlerFunc(messageHandler.UpdateMessageHandler)))
	mux.Handle("DELETE /messages/{id}", requireAuth(http.HandlerFunc(messageHandler.DeleteMessageHandler)))
	mux.Handle("POST /servers/{id}/join-codes", requireAuth(http.HandlerFunc(joinCodeHandler.CreateJoinCodeHandler)))
	mux.Handle("POST /join", requireAuth(http.HandlerFunc(joinCodeHandler.JoinServerViaCodeHandler)))
	mux.Handle("DELETE /servers/{id}/join-codes/{codeID}", requireAuth(http.HandlerFunc(joinCodeHandler.RevokeJoinCodeHandler)))
	mux.Handle("GET /servers/{id}/members", requireAuth(http.HandlerFunc(membershipHandler.ListMembersHandler)))
	mux.Handle("PATCH /servers/{id}/members/{userID}", requireAuth(http.HandlerFunc(membershipHandler.UpdateMemberRoleHandler)))
	mux.Handle("DELETE /servers/{id}/members/{userID}", requireAuth(http.HandlerFunc(membershipHandler.RemoveMemberHandler)))
	mux.Handle("GET /channels/{id}/ws", requireAuth(http.HandlerFunc(realtimeHandler.ChatHandler)))
	mux.Handle("GET /me", requireAuth(http.HandlerFunc(authHandler.MeHandler)))
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	json.NewEncoder(w).Encode(map[string]string{
		"message": "healthy",
	})
}
