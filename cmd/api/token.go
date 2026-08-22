package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"sync"
)

type tokenStore struct {
	mu          sync.RWMutex
	userByToken map[string]int64
}

func newTokenStore() *tokenStore {
	return &tokenStore{userByToken: make(map[string]int64)}
}

func (s *tokenStore) Issue(userID int64) (string, error) {
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}

	token := base64.RawURLEncoding.EncodeToString(randomBytes)
	s.mu.Lock()
	s.userByToken[token] = userID
	s.mu.Unlock()
	return token, nil
}

func (s *tokenStore) UserID(token string) (int64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	userID, ok := s.userByToken[token]
	return userID, ok
}

type authenticatedUserIDKey struct{}

func authenticate(tokens *tokenStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authorization := strings.TrimSpace(r.Header.Get("Authorization"))
			parts := strings.Fields(authorization)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				writeError(w, http.StatusUnauthorized, "valid Bearer token is required")
				return
			}

			userID, ok := tokens.UserID(parts[1])
			if !ok {
				writeError(w, http.StatusUnauthorized, "valid Bearer token is required")
				return
			}

			ctx := context.WithValue(r.Context(), authenticatedUserIDKey{}, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func authenticatedUserID(r *http.Request) int64 {
	userID, _ := r.Context().Value(authenticatedUserIDKey{}).(int64)
	return userID
}

func ensureAuthenticatedUser(w http.ResponseWriter, r *http.Request, requestedUserID int64) bool {
	if authenticatedUserID(r) != requestedUserID {
		writeError(w, http.StatusForbidden, "access to another user's resources is forbidden")
		return false
	}
	return true
}
