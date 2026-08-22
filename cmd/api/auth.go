package main

import (
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

type authRegisterResponse struct {
	Message string `json:"message"`
	ID      int64  `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
}
type authRegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}
type authLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}
type authLoginResponse struct {
	Message string `json:"message"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Token   string `json:"token"`
}

func authRegisterHandler(logger *slog.Logger, store *userStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requestHasJSONContentType(r) {
			writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
			return
		}

		var req authRegisterRequest
		if err := readJSON(w, r, &req); err != nil {
			if writeErr := writeError(w, http.StatusBadRequest, "invalid JSON body"); writeErr != nil {
				logger.Error("failed to encode error response", "error", writeErr)
			}
			logger.Error("failed to decode request body", "error", err)
			return

		}

		email := strings.ToLower(strings.TrimSpace(req.Email))
		name := strings.TrimSpace(req.Name)

		if email == "" || name == "" || req.Password == "" {
			if err := writeError(w, http.StatusBadRequest, "email, password and name are required"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}
		parsedEmail, err := mail.ParseAddress(email)
		if err != nil || parsedEmail.Address != email {
			writeError(w, http.StatusBadRequest, "email must be valid")
			return
		}
		if len(req.Password) < 8 {
			writeError(w, http.StatusBadRequest, "password must contain at least 8 characters")
			return
		}
		if len(name) > 100 {
			writeError(w, http.StatusBadRequest, "name must not exceed 100 characters")
			return
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			if writeErr := writeError(w, http.StatusInternalServerError, "failed to hash password"); writeErr != nil {
				logger.Error("failed to encode error response", "error", writeErr)
			}
			logger.Error("failed to hash password", "error", err)
			return
		}
		user, err := store.Create(email, name, string(hash))
		if errors.Is(err, errUserAlreadyExists) {
			if writeErr := writeError(w, http.StatusConflict, "user with this email already exists"); writeErr != nil {
				logger.Error("failed to encode error response", "error", writeErr)
			}
			return
		}

		if err != nil {
			if writeErr := writeError(w, http.StatusInternalServerError, "internal server error"); writeErr != nil {
				logger.Error("failed to encode error response", "error", writeErr)
			}
			logger.Error("failed to create user", "error", err)
			return
		}
		resp := authRegisterResponse{
			Email:   user.Email,
			ID:      user.ID,
			Name:    user.Name,
			Message: "user registered",
		}
		if err := writeJSON(w, http.StatusCreated, resp); err != nil {
			logger.Error("failed to encode response", "error", err)
		}
	}
}
func authLoginHandler(logger *slog.Logger, store *userStore, tokens *tokenStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requestHasJSONContentType(r) {
			writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
			return
		}

		var req authLoginRequest
		err := readJSON(w, r, &req)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		email := strings.ToLower(strings.TrimSpace(req.Email))

		if email == "" || req.Password == "" {
			writeError(w, http.StatusBadRequest, "email and password are required")
			return
		}
		user, ok := store.GetByEmail(email)
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid email or password")
			return
		}
		err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password))

		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid email or password")
			return
		}
		token, err := tokens.Issue(user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create session")
			logger.Error("failed to issue session token", "error", err)
			return
		}
		resp := authLoginResponse{
			Message: "login successful",
			Email:   user.Email,
			Name:    user.Name,
			Token:   token,
		}
		writeJSON(w, http.StatusOK, resp)

	}
}

func (s *userStore) GetByID(id int64) (User, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, user := range s.userByEmail {
		if user.ID == id {
			return user, true
		}
	}
	return User{}, false
}
