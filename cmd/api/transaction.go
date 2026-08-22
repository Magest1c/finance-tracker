package main

import (
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

type Transaction struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"user_id"`
	AccountID   int64     `json:"account_id"`
	CategoryID  int64     `json:"category_id"`
	Type        string    `json:"type"`
	Amount      int64     `json:"amount"`
	Description string    `json:"description"`
	OccurredAt  time.Time `json:"occurred_at"`
}
type transactionStore struct {
	mu           sync.Mutex
	transactions map[int64]Transaction
	nextID       int64
}

func newTransactionStore() *transactionStore {
	return &transactionStore{
		transactions: make(map[int64]Transaction),
		nextID:       1,
	}
}

func (s *transactionStore) Create(userID int64,
	accountID int64,
	categoryID int64,
	transactionType string,
	amount int64,
	description string,
	occurredAt time.Time) (Transaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	transaction := Transaction{
		ID:          s.nextID,
		UserID:      userID,
		AccountID:   accountID,
		CategoryID:  categoryID,
		Type:        transactionType,
		Amount:      amount,
		Description: description,
		OccurredAt:  occurredAt,
	}
	s.transactions[transaction.ID] = transaction
	s.nextID++

	return transaction, nil
}

type createTransactionRequest struct {
	UserID      int64     `json:"user_id"`
	AccountID   int64     `json:"account_id"`
	CategoryID  int64     `json:"category_id"`
	Amount      int64     `json:"amount"`
	Description string    `json:"description"`
	OccurredAt  time.Time `json:"occurred_at"`
}

func createTransactionHandler(
	logger *slog.Logger,
	store *transactionStore,
	userStore *userStore,
	accountStore *accountsStore,
	categoryStore *CategoryStore,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requestHasJSONContentType(r) {
			writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
			return
		}

		var req createTransactionRequest

		if err := readJSON(w, r, &req); err != nil {
			if writeErr := writeError(w, http.StatusBadRequest, "invalid JSON body"); writeErr != nil {
				logger.Error("failed to encode error response", "error", writeErr)
			}
			logger.Error("failed to decode request body", "error", err)
			return
		}

		if req.UserID <= 0 || req.AccountID <= 0 || req.CategoryID <= 0 {
			if err := writeError(w, http.StatusBadRequest, "user_id, account_id and category_id must be positive"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}

		if req.Amount <= 0 {
			if err := writeError(w, http.StatusBadRequest, "amount must be positive"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}
		if !ensureAuthenticatedUser(w, r, req.UserID) {
			return
		}
		if req.OccurredAt.IsZero() {
			if err := writeError(w, http.StatusBadRequest, "occurred_at is required"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}
		if _, ok := userStore.GetByID(req.UserID); !ok {
			if err := writeError(w, http.StatusNotFound, "user not found"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}

		account, ok := accountStore.GetByID(req.AccountID)
		if !ok {
			if err := writeError(w, http.StatusNotFound, "account not found"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}

		category, ok := categoryStore.GetByID(req.CategoryID)
		if !ok {
			if err := writeError(w, http.StatusNotFound, "category not found"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}

		if account.UserID != req.UserID {
			if err := writeError(w, http.StatusForbidden, "account does not belong to user"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}

		if category.UserID != req.UserID {
			if err := writeError(w, http.StatusForbidden, "category does not belong to user"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}
		description := strings.TrimSpace(req.Description)

		transaction, err := store.Create(
			req.UserID,
			req.AccountID,
			req.CategoryID,
			category.Type,
			req.Amount,
			description,
			req.OccurredAt,
		)
		if err != nil {
			if writeErr := writeError(w, http.StatusInternalServerError, "failed to create transaction"); writeErr != nil {
				logger.Error("failed to encode error response", "error", writeErr)
			}
			logger.Error("failed to create transaction", "error", err)
			return
		}

		if err := writeJSON(w, http.StatusCreated, transaction); err != nil {
			logger.Error("failed to encode response", "error", err)
		}
	}
}

func getTransactionsHandler(
	logger *slog.Logger,
	store *transactionStore,
	userStore *userStore,
) http.HandlerFunc {
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
			if err := writeError(w, http.StatusNotFound, "user not found"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}

		transactionType := strings.ToLower(
			strings.TrimSpace(r.URL.Query().Get("type")),
		)

		if transactionType != "" &&
			transactionType != "income" &&
			transactionType != "expense" {
			if err := writeError(w, http.StatusBadRequest, "type must be income or expense"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}
		accountIDParam := r.URL.Query().Get("account_id")

		var accountID int64

		if accountIDParam != "" {
			parsedAccountID, parseErr := strconv.ParseInt(accountIDParam, 10, 64)

			if parseErr != nil || parsedAccountID <= 0 {
				if err := writeError(w, http.StatusBadRequest, "account_id must be a positive integer"); err != nil {
					logger.Error("failed to encode error response", "error", err)
				}
				return
			}

			accountID = parsedAccountID
		}
		categoryIDParam := r.URL.Query().Get("category_id")

		var categoryID int64

		if categoryIDParam != "" {
			parsedCategoryID, parseErr := strconv.ParseInt(categoryIDParam, 10, 64)
			if parseErr != nil || parsedCategoryID <= 0 {
				if err := writeError(w, http.StatusBadRequest, "category_id must be a positive integer"); err != nil {
					logger.Error("failed to encode error response", "error", err)
				}
				return
			}

			categoryID = parsedCategoryID
		}
		fromParam := r.URL.Query().Get("from")

		var from *time.Time

		if fromParam != "" {
			parsedFrom, parseErr := time.Parse(time.RFC3339, fromParam)
			if parseErr != nil {
				if err := writeError(w, http.StatusBadRequest, "from must be a valid RFC3339 timestamp"); err != nil {
					logger.Error("failed to encode error response", "error", err)
				}
				return
			}

			from = &parsedFrom
		}
		toParam := r.URL.Query().Get("to")

		var to *time.Time

		if toParam != "" {
			parsedTo, parseErr := time.Parse(time.RFC3339, toParam)
			if parseErr != nil {
				if err := writeError(w, http.StatusBadRequest, "to must be a valid RFC3339 timestamp"); err != nil {
					logger.Error("failed to encode error response", "error", err)
				}
				return
			}

			to = &parsedTo
		}
		if from != nil && to != nil && !from.Before(*to) {
			if err := writeError(w, http.StatusBadRequest, "from must be before to"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}
		limit := 20
		limitParam := r.URL.Query().Get("limit")

		if limitParam != "" {
			parsedLimit, parseErr := strconv.Atoi(limitParam)
			if parseErr != nil || parsedLimit <= 0 || parsedLimit > 100 {
				if err := writeError(w, http.StatusBadRequest, "limit must be between 1 and 100"); err != nil {
					logger.Error("failed to encode error response", "error", err)
				}
				return
			}

			limit = parsedLimit
		}
		offset := 0
		offsetParam := r.URL.Query().Get("offset")

		if offsetParam != "" {
			parsedOffset, parseErr := strconv.Atoi(offsetParam)
			if parseErr != nil || parsedOffset < 0 {
				if err := writeError(w, http.StatusBadRequest, "offset must be a non-negative integer"); err != nil {
					logger.Error("failed to encode error response", "error", err)
				}
				return
			}

			offset = parsedOffset
		}

		filter := transactionFilter{
			UserID:     userID,
			Type:       transactionType,
			AccountID:  accountID,
			CategoryID: categoryID,
			From:       from,
			To:         to,
			Limit:      limit,
			Offset:     offset,
		}

		transactions, total := store.List(filter)

		response := transactionListResponse{
			Items:  transactions,
			Count:  len(transactions),
			Total:  total,
			Limit:  limit,
			Offset: offset,
		}

		if err := writeJSON(w, http.StatusOK, response); err != nil {
			logger.Error("failed to encode response", "error", err)
		}
	}
}

type updateTransactionRequest struct {
	Amount      *int64     `json:"amount"`
	Description *string    `json:"description"`
	OccurredAt  *time.Time `json:"occurred_at"`
}

func (s *transactionStore) Update(id int64, amount *int64, description *string, occurredAt *time.Time) (Transaction, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	transaction, ok := s.transactions[id]

	if !ok {
		return Transaction{}, false
	}
	if amount != nil {
		transaction.Amount = *amount
	}
	if description != nil {
		transaction.Description = strings.TrimSpace(*description)
	}
	if occurredAt != nil {
		transaction.OccurredAt = *occurredAt
	}
	s.transactions[id] = transaction

	return transaction, true
}
func (s *transactionStore) GetByID(id int64) (Transaction, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	transaction, ok := s.transactions[id]

	return transaction, ok
}
func updateTransactionHandler(
	logger *slog.Logger,
	store *transactionStore,
	userStore *userStore,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requestHasJSONContentType(r) {
			writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
			return
		}

		transactionIDParam := chi.URLParam(r, "id")

		transactionID, err := strconv.ParseInt(transactionIDParam, 10, 64)
		if err != nil || transactionID <= 0 {
			if err := writeError(w, http.StatusBadRequest, "transaction id must be a positive integer"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}

		userIDParam := r.URL.Query().Get("user_id")

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
			if err := writeError(w, http.StatusNotFound, "user not found"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}
		transaction, ok := store.GetByID(transactionID)
		if !ok {
			if err := writeError(w, http.StatusNotFound, "transaction not found"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}
		if transaction.UserID != userID {
			if err := writeError(w, http.StatusForbidden, "transaction does not belong to user"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}
		var req updateTransactionRequest

		if err := readJSON(w, r, &req); err != nil {
			if writeErr := writeError(w, http.StatusBadRequest, "invalid JSON body"); writeErr != nil {
				logger.Error("failed to encode error response", "error", writeErr)
			}
			logger.Error("failed to decode request body", "error", err)
			return
		}
		if req.Amount == nil && req.Description == nil && req.OccurredAt == nil {
			if err := writeError(w, http.StatusBadRequest, "at least one field must be provided"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}
		if req.Amount != nil && *req.Amount <= 0 {
			if err := writeError(w, http.StatusBadRequest, "amount must be positive"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}
		if req.OccurredAt != nil && req.OccurredAt.IsZero() {
			if err := writeError(w, http.StatusBadRequest, "occurred_at must not be zero"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}
		updatedTransaction, ok := store.Update(
			transactionID,
			req.Amount,
			req.Description,
			req.OccurredAt,
		)
		if !ok {
			if err := writeError(w, http.StatusNotFound, "transaction not found"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}
		if err := writeJSON(w, http.StatusOK, updatedTransaction); err != nil {
			logger.Error("failed to encode response", "error", err)
		}

	}
}
func (s *transactionStore) Delete(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.transactions[id]; !ok {
		return false
	}
	delete(s.transactions, id)
	return true
}
func deleteTransactionHandler(
	logger *slog.Logger,
	store *transactionStore,
	userStore *userStore,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		transactionIDParam := chi.URLParam(r, "id")

		transactionID, err := strconv.ParseInt(transactionIDParam, 10, 64)
		if err != nil || transactionID <= 0 {
			if err := writeError(w, http.StatusBadRequest, "transaction id must be a positive integer"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}

		userIDParam := r.URL.Query().Get("user_id")

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
			if err := writeError(w, http.StatusNotFound, "user not found"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}
		transaction, ok := store.GetByID(transactionID)
		if !ok {
			if err := writeError(w, http.StatusNotFound, "transaction not found"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}
		if transaction.UserID != userID {
			if err := writeError(w, http.StatusForbidden, "transaction does not belong to user"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}
		if deleted := store.Delete(transactionID); !deleted {
			if err := writeError(w, http.StatusNotFound, "transaction not found"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

type transactionFilter struct {
	UserID     int64
	Type       string
	AccountID  int64
	CategoryID int64
	From       *time.Time
	To         *time.Time
	Limit      int
	Offset     int
}
type transactionListResponse struct {
	Items  []Transaction `json:"items"`
	Count  int           `json:"count"`
	Total  int           `json:"total"`
	Limit  int           `json:"limit"`
	Offset int           `json:"offset"`
}

func (s *transactionStore) List(filter transactionFilter) ([]Transaction, int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	transactions := make([]Transaction, 0)

	for _, transaction := range s.transactions {
		if transaction.UserID != filter.UserID {
			continue
		}
		if filter.Type != "" && transaction.Type != filter.Type {
			continue
		}

		if filter.AccountID > 0 && transaction.AccountID != filter.AccountID {
			continue
		}

		if filter.CategoryID > 0 && transaction.CategoryID != filter.CategoryID {
			continue
		}
		if filter.From != nil &&
			transaction.OccurredAt.Before(*filter.From) {
			continue
		}

		if filter.To != nil &&
			!transaction.OccurredAt.Before(*filter.To) {
			continue
		}
		transactions = append(transactions, transaction)
	}
	sort.Slice(transactions, func(i, j int) bool {
		return transactions[i].OccurredAt.After(transactions[j].OccurredAt)
	})
	total := len(transactions)
	start := filter.Offset

	if start < 0 {
		start = 0
	}

	if start >= len(transactions) {
		return make([]Transaction, 0), total
	}

	end := len(transactions)

	if filter.Limit > 0 && start+filter.Limit < end {
		end = start + filter.Limit
	}

	return transactions[start:end], total
}
