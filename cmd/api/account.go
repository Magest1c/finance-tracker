package main

import (
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type Account struct {
	ID       int64  `json:"id"`
	UserID   int64  `json:"user_id"`
	Name     string `json:"name"`
	Currency string `json:"currency"`
}
type accountsStore struct {
	mu       sync.Mutex
	accounts map[int64]Account
	nextId   int64
}

func newAccountStore() *accountsStore {
	return &accountsStore{
		accounts: make(map[int64]Account),
		nextId:   1,
	}
}
func (s *accountsStore) Create(userID int64, name string, currency string) (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	account := Account{
		ID:       s.nextId,
		UserID:   userID,
		Name:     name,
		Currency: currency,
	}
	s.accounts[account.ID] = account
	s.nextId++
	return account, nil
}

type createAccountRequest struct {
	UserID   int64  `json:"user_id"`
	Name     string `json:"name"`
	Currency string `json:"currency"`
}

func createAccountHandler(logger *slog.Logger, accountStore *accountsStore, userStore *userStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requestHasJSONContentType(r) {
			writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
			return
		}

		var req createAccountRequest

		err := readJSON(w, r, &req)

		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		name := strings.TrimSpace(req.Name)
		currency := strings.ToUpper(strings.TrimSpace(req.Currency))

		if name == "" || currency == "" || req.UserID <= 0 {
			writeError(w, http.StatusBadRequest, "user_id, name and currency are required")
			return
		}
		if !isThreeLetterCurrency(currency) {
			writeError(w, http.StatusBadRequest, "currency must be a three-letter ISO 4217 code")
			return
		}
		if !ensureAuthenticatedUser(w, r, req.UserID) {
			return
		}
		if _, ok := userStore.GetByID(req.UserID); !ok {
			if err := writeError(w, http.StatusBadRequest, "user not found"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}

			return
		}

		account, err := accountStore.Create(req.UserID, name, currency)

		if err != nil {
			writeError(w, http.StatusInternalServerError, "bad server")
			return
		}

		writeJSON(w, http.StatusCreated, account)

	}
}

func isThreeLetterCurrency(currency string) bool {
	if len(currency) != 3 {
		return false
	}
	for _, character := range currency {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	return true
}
func (s *accountsStore) GetByID(id int64) (Account, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	account, ok := s.accounts[id]

	return account, ok
}
func (s *accountsStore) ListByUserID(userID int64) []Account {
	s.mu.Lock()
	defer s.mu.Unlock()

	accounts := make([]Account, 0)

	for _, account := range s.accounts {
		if account.UserID == userID {
			accounts = append(accounts, account)
		}
	}
	sort.Slice(accounts, func(i, j int) bool {
		return accounts[i].ID < accounts[j].ID
	})
	return accounts
}
func getAccountsHandler(logger *slog.Logger, accountsStore *accountsStore, userStore *userStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		userIDParam := r.URL.Query().Get("user_id")

		if userIDParam == "" {
			if err := writeError(w, http.StatusBadRequest, "user_id is required"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}
		userID, err := strconv.ParseInt(userIDParam, 10, 64)

		if err != nil || userID <= 0 {
			if err := writeError(w, http.StatusBadRequest, "user_id must be a positive integer"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}
		if !ensureAuthenticatedUser(w, r, userID) {
			return
		}
		if _, ok := userStore.GetByID(userID); !ok {
			if err := writeError(w, http.StatusBadRequest, "user not found"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}
		accounts := accountsStore.ListByUserID(userID)

		if err := writeJSON(w, http.StatusOK, accounts); err != nil {
			logger.Error("failed to encode response", "error", err)
		}
	}
}
