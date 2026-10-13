package users

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

var ErrInvalidInput = errors.New("invalid input")
var ErrEmailTaken = errors.New("email is already registered")

type UserService struct {
	repository *UserRepository
}

func NewUserService(repository *UserRepository) *UserService {
	return &UserService{
		repository: repository,
	}
}

type RegisterInput struct {
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Password    string `json:"password"`
}

func (s *UserService) Register(ctx context.Context, input *RegisterInput) (*User, error) {
	if input == nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidInput, "input cannot be nil")
	}
	normalizedInput := &RegisterInput{
		DisplayName: strings.TrimSpace(input.DisplayName),
		Email:       strings.TrimSpace(strings.ToLower(input.Email)),
		Password:    input.Password,
	}

	if err := validateRegisterInput(normalizedInput); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	passwordHash, err := hashPassword(normalizedInput.Password)
	if err != nil {
		return nil, err
	}
	user := &User{
		DisplayName:  normalizedInput.DisplayName,
		Email:        normalizedInput.Email,
		PasswordHash: passwordHash,
	}
	err = s.repository.Insert(ctx, user)
	if err != nil {
		var pgErr *pgconn.PgError

		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "users_email_key" {
			return nil, ErrEmailTaken
		}
		return nil, err
	}

	return user, nil
}

func validateRegisterInput(input *RegisterInput) error {
	if input == nil {
		return errors.New("input cannot be nil")
	}
	displayName := strings.TrimSpace(input.DisplayName)
	email := strings.TrimSpace(input.Email)
	email = strings.ToLower(email)
	if displayName == "" {
		return errors.New("display name cannot be empty")
	}
	if utf8.RuneCountInString(displayName) > 16 {
		return errors.New("display name cannot exceed 16 characters")
	}
	if email == "" {
		return errors.New("email cannot be empty")
	}

	parsedEmail, err := mail.ParseAddress(email)
	if err != nil || parsedEmail.Address != email {
		return errors.New("invalid email format")
	}
	if len(input.Password) < 8 {
		return errors.New("password must be at least 8 characters long")
	}

	return nil
}

func hashPassword(password string) (string, error) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(passwordHash), nil
}
