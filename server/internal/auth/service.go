package auth

import (
	"context"
	"errors"
	"fmt"
	"rtchat/internal/users"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

var ErrInvalidInput = errors.New("invalid input")
var ErrInvalidCredentials = errors.New("invalid credentials")

type SessionService struct {
	repository *SessionRepository
}

func NewSessionService(repository *SessionRepository) *SessionService {
	return &SessionService{
		repository: repository,
	}
}

type AuthService struct {
	userRepository *users.UserRepository
	sessionService *SessionService
}

func NewAuthService(userRepository *users.UserRepository, sessionService *SessionService) *AuthService {
	return &AuthService{
		userRepository: userRepository,
		sessionService: sessionService,
	}
}

func (s *SessionService) FindByTokenHash(ctx context.Context, tokenHash string) (*Session, error) {
	return s.repository.FindByTokenHash(ctx, tokenHash)
}

type LoginInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *SessionService) CreateSession(ctx context.Context, userID int64, expiresAt time.Time) (string, error) {
	if userID <= 0 {
		return "", fmt.Errorf("%w: invalid user ID", ErrInvalidInput)
	}

	rawToken, err := GenerateToken()
	if err != nil {
		return "", err
	}

	session := &Session{
		UserID:    userID,
		TokenHash: HashToken(rawToken),
		ExpiresAt: expiresAt,
	}

	if err := s.repository.Insert(ctx, session); err != nil {
		return "", err
	}

	return rawToken, nil
}

func (s *AuthService) Login(ctx context.Context, input *LoginInput) (*users.User, string, error) {
	if input == nil {
		return nil, "", ErrInvalidInput
	}

	email := strings.ToLower(strings.TrimSpace(input.Email))

	if email == "" || input.Password == "" {
		return nil, "", ErrInvalidInput
	}

	user, err := s.userRepository.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", ErrInvalidCredentials
		}

		return nil, "", err
	}

	if err := bcrypt.CompareHashAndPassword(
		[]byte(user.PasswordHash),
		[]byte(input.Password),
	); err != nil {
		return nil, "", ErrInvalidCredentials
	}

	expiresAt := time.Now().Add(72 * time.Hour)

	rawToken, err := s.sessionService.CreateSession(
		ctx,
		user.ID,
		expiresAt,
	)
	if err != nil {
		return nil, "", err
	}

	return user, rawToken, nil
}

func AuthenticateSession(ctx context.Context, sessionService *SessionService, token string) (*Session, error) {
	if token == "" {
		return nil, fmt.Errorf("%w: token cannot be empty", ErrInvalidInput)
	}

	tokenHash := HashToken(token)

	session, err := sessionService.FindByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: session not found or expired", ErrInvalidCredentials)
		}
		return nil, err
	}

	return session, nil
}

func (s *SessionService) Revoke(ctx context.Context, sessionID int64) error {
	if sessionID <= 0 {
		return fmt.Errorf("%w: invalid session id", ErrInvalidInput)
	}
	if s.repository == nil {
		return fmt.Errorf("session repository is nil")
	}

	return s.repository.Revoke(ctx, sessionID)
}

func (s *AuthService) Logout(ctx context.Context, sessionID int64) error {
	if s.sessionService == nil {
		return fmt.Errorf("session service is nil")
	}

	return s.sessionService.Revoke(ctx, sessionID)
}
