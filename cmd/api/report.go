package main

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

type MonthlySummary struct {
	Month            string `json:"month"`
	AccountID        int64  `json:"account_id"`
	Currency         string `json:"currency"`
	Income           int64  `json:"income"`
	Expense          int64  `json:"expense"`
	Balance          int64  `json:"balance"`
	TransactionCount int    `json:"transaction_count"`
}

func (s *transactionStore) SumByPeriod(
	userID int64,
	accountID int64,
	from time.Time,
	to time.Time,
) (income int64, expense int64, count int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, transaction := range s.transactions {
		if transaction.UserID != userID {
			continue
		}

		if transaction.AccountID != accountID {
			continue
		}

		if transaction.OccurredAt.Before(from) {
			continue
		}

		if !transaction.OccurredAt.Before(to) {
			continue
		}

		switch transaction.Type {
		case "income":
			income += transaction.Amount
		case "expense":
			expense += transaction.Amount
		default:
			continue
		}

		count++
	}

	return income, expense, count
}
func monthlySummaryHandler(
	logger *slog.Logger,
	transactionStore *transactionStore,
	userStore *userStore,
	accountStore *accountsStore,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		accountIDParam := r.URL.Query().Get("account_id")
		accountID, err := strconv.ParseInt(accountIDParam, 10, 64)

		if err != nil || accountID <= 0 {
			if err := writeError(w, http.StatusBadRequest, "account_id must be a positive integer"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}
		monthParam := r.URL.Query().Get("month")

		monthStart, err := time.Parse("2006-01", monthParam)
		if err != nil {
			if writeErr := writeError(w, http.StatusBadRequest, "month must have YYYY-MM format"); writeErr != nil {
				logger.Error("failed to encode error response", "error", writeErr)
			}
			return
		}

		monthEnd := monthStart.AddDate(0, 1, 0)

		if _, ok := userStore.GetByID(userID); !ok {
			if err := writeError(w, http.StatusNotFound, "user not found"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}

		account, ok := accountStore.GetByID(accountID)
		if !ok {
			if err := writeError(w, http.StatusNotFound, "account not found"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}

		if account.UserID != userID {
			if err := writeError(w, http.StatusForbidden, "account does not belong to user"); err != nil {
				logger.Error("failed to encode error response", "error", err)
			}
			return
		}
		income, expense, count := transactionStore.SumByPeriod(
			userID,
			accountID,
			monthStart,
			monthEnd,
		)
		summary := MonthlySummary{
			Month:            monthParam,
			AccountID:        account.ID,
			Currency:         account.Currency,
			Income:           income,
			Expense:          expense,
			Balance:          income - expense,
			TransactionCount: count,
		}

		if err := writeJSON(w, http.StatusOK, summary); err != nil {
			logger.Error("failed to encode response", "error", err)
		}
	}
}
