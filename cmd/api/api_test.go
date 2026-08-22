package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type apiHarness struct {
	handler     http.Handler
	activeToken string
	tokens      map[int64]string
}

func newAPIHarness() *apiHarness {
	app := newApplication(config{httpAddr: ":0"}, io.Discard)
	return &apiHarness{handler: app.router(), tokens: make(map[int64]string)}
}

func (h *apiHarness) jsonRequest(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		payload = bytes.NewReader(encoded)
	}

	request := httptest.NewRequest(method, path, payload)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if h.activeToken != "" {
		request.Header.Set("Authorization", "Bearer "+h.activeToken)
	}

	response := httptest.NewRecorder()
	h.handler.ServeHTTP(response, request)
	return response
}

func (h *apiHarness) rawRequest(method, path, contentType, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}

	response := httptest.NewRecorder()
	h.handler.ServeHTTP(response, request)
	return response
}

func decodeResponse[T any](t *testing.T, response *httptest.ResponseRecorder) T {
	t.Helper()

	var value T
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode response %q: %v", response.Body.String(), err)
	}
	return value
}

func assertStatus(t *testing.T, response *httptest.ResponseRecorder, expected int) {
	t.Helper()
	if response.Code != expected {
		t.Fatalf("expected status %d, got %d: %s", expected, response.Code, response.Body.String())
	}
}

func createUser(t *testing.T, h *apiHarness, email string) authRegisterResponse {
	t.Helper()
	response := h.jsonRequest(t, http.MethodPost, "/auth/register", map[string]any{
		"email":    email,
		"password": "StrongPass123!",
		"name":     "QA Candidate",
	})
	assertStatus(t, response, http.StatusCreated)
	user := decodeResponse[authRegisterResponse](t, response)

	loginResponse := h.jsonRequest(t, http.MethodPost, "/auth/login", map[string]any{
		"email":    user.Email,
		"password": "StrongPass123!",
	})
	assertStatus(t, loginResponse, http.StatusOK)
	login := decodeResponse[authLoginResponse](t, loginResponse)
	h.tokens[user.ID] = login.Token
	h.activeToken = login.Token

	return user
}

func (h *apiHarness) useUser(t *testing.T, userID int64) {
	t.Helper()
	token, ok := h.tokens[userID]
	if !ok {
		t.Fatalf("no token stored for user %d", userID)
	}
	h.activeToken = token
}

func createAccount(t *testing.T, h *apiHarness, userID int64, name string) Account {
	t.Helper()
	response := h.jsonRequest(t, http.MethodPost, "/accounts", map[string]any{
		"user_id":  userID,
		"name":     name,
		"currency": "rub",
	})
	assertStatus(t, response, http.StatusCreated)
	return decodeResponse[Account](t, response)
}

func createCategory(t *testing.T, h *apiHarness, userID int64, name, categoryType string) Category {
	t.Helper()
	response := h.jsonRequest(t, http.MethodPost, "/categories", map[string]any{
		"user_id": userID,
		"name":    name,
		"type":    categoryType,
	})
	assertStatus(t, response, http.StatusCreated)
	return decodeResponse[Category](t, response)
}

func TestHealthEndpoint(t *testing.T) {
	h := newAPIHarness()
	response := h.jsonRequest(t, http.MethodGet, "/health", nil)

	assertStatus(t, response, http.StatusOK)
	if contentType := response.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("expected application/json, got %q", contentType)
	}
	if body := decodeResponse[healthResponse](t, response); body.Status != "ok" {
		t.Fatalf("expected healthy status, got %+v", body)
	}
}

func TestConfigurationAndProtocolErrors(t *testing.T) {
	t.Run("uses default address", func(t *testing.T) {
		t.Setenv("HTTP_ADDR", "")
		if cfg := loadConfig(); cfg.httpAddr != ":8080" {
			t.Fatalf("expected default address, got %q", cfg.httpAddr)
		}
	})

	t.Run("trims configured address", func(t *testing.T) {
		t.Setenv("HTTP_ADDR", " 127.0.0.1:9090 ")
		if cfg := loadConfig(); cfg.httpAddr != "127.0.0.1:9090" {
			t.Fatalf("unexpected configured address: %q", cfg.httpAddr)
		}
	})

	h := newAPIHarness()

	t.Run("unknown route returns JSON 404", func(t *testing.T) {
		response := h.jsonRequest(t, http.MethodGet, "/missing", nil)
		assertStatus(t, response, http.StatusNotFound)
		if body := decodeResponse[errorResponse](t, response); body.Error != "route not found" {
			t.Fatalf("unexpected error response: %+v", body)
		}
	})

	t.Run("wrong method returns JSON 405", func(t *testing.T) {
		response := h.jsonRequest(t, http.MethodPost, "/health", nil)
		assertStatus(t, response, http.StatusMethodNotAllowed)
	})

	t.Run("protected route rejects missing token", func(t *testing.T) {
		response := h.jsonRequest(t, http.MethodGet, "/accounts?user_id=1", nil)
		assertStatus(t, response, http.StatusUnauthorized)
	})

	t.Run("protected route rejects unknown token", func(t *testing.T) {
		h.activeToken = "not-issued"
		response := h.jsonRequest(t, http.MethodGet, "/accounts?user_id=1", nil)
		assertStatus(t, response, http.StatusUnauthorized)
	})
}

func TestRegistrationAndLogin(t *testing.T) {
	h := newAPIHarness()

	t.Run("rejects missing JSON content type", func(t *testing.T) {
		response := h.rawRequest(http.MethodPost, "/auth/register", "", `{"email":"qa@example.com"}`)
		assertStatus(t, response, http.StatusUnsupportedMediaType)
	})

	t.Run("rejects malformed JSON", func(t *testing.T) {
		response := h.rawRequest(http.MethodPost, "/auth/register", "application/json", `{"email":`)
		assertStatus(t, response, http.StatusBadRequest)
	})

	t.Run("rejects unknown fields", func(t *testing.T) {
		response := h.rawRequest(http.MethodPost, "/auth/register", "application/json", `{"email":"qa@example.com","password":"StrongPass123!","name":"QA","admin":true}`)
		assertStatus(t, response, http.StatusBadRequest)
	})

	validationCases := []struct {
		name     string
		email    string
		password string
		fullName string
	}{
		{name: "invalid email", email: "not-an-email", password: "StrongPass123!", fullName: "QA"},
		{name: "seven-character password", email: "qa@example.com", password: "1234567", fullName: "QA"},
		{name: "missing name", email: "qa@example.com", password: "StrongPass123!", fullName: "  "},
		{name: "name over 100 characters", email: "qa@example.com", password: "StrongPass123!", fullName: strings.Repeat("N", 101)},
	}

	t.Run("accepts password length 8 and name length 100", func(t *testing.T) {
		response := h.jsonRequest(t, http.MethodPost, "/auth/register", map[string]any{
			"email":    "boundary@example.com",
			"password": "12345678",
			"name":     strings.Repeat("N", 100),
		})
		assertStatus(t, response, http.StatusCreated)
	})

	t.Run("rejects body over one MiB", func(t *testing.T) {
		response := h.rawRequest(
			http.MethodPost,
			"/auth/register",
			"application/json",
			`{"email":"large@example.com","password":"StrongPass123!","name":"`+strings.Repeat("N", maxRequestBodyBytes)+`"}`,
		)
		assertStatus(t, response, http.StatusBadRequest)
	})

	for _, testCase := range validationCases {
		t.Run(testCase.name, func(t *testing.T) {
			response := h.jsonRequest(t, http.MethodPost, "/auth/register", map[string]any{
				"email":    testCase.email,
				"password": testCase.password,
				"name":     testCase.fullName,
			})
			assertStatus(t, response, http.StatusBadRequest)
		})
	}

	registered := createUser(t, h, "  QA.Candidate@Example.com ")
	if registered.ID <= 0 || registered.Email != "qa.candidate@example.com" {
		t.Fatalf("registration response was not normalized: %+v", registered)
	}

	t.Run("rejects duplicate email case-insensitively", func(t *testing.T) {
		response := h.jsonRequest(t, http.MethodPost, "/auth/register", map[string]any{
			"email":    "QA.CANDIDATE@example.com",
			"password": "StrongPass123!",
			"name":     "Another Name",
		})
		assertStatus(t, response, http.StatusConflict)
	})

	t.Run("logs in with normalized email", func(t *testing.T) {
		response := h.jsonRequest(t, http.MethodPost, "/auth/login", map[string]any{
			"email":    " QA.CANDIDATE@EXAMPLE.COM ",
			"password": "StrongPass123!",
		})
		assertStatus(t, response, http.StatusOK)
		body := decodeResponse[authLoginResponse](t, response)
		if body.Email != registered.Email || body.Message != "login successful" {
			t.Fatalf("unexpected login response: %+v", body)
		}
	})

	t.Run("does not disclose whether email or password is wrong", func(t *testing.T) {
		for _, credentials := range []map[string]any{
			{"email": "missing@example.com", "password": "StrongPass123!"},
			{"email": registered.Email, "password": "WrongPass123!"},
		} {
			response := h.jsonRequest(t, http.MethodPost, "/auth/login", credentials)
			assertStatus(t, response, http.StatusUnauthorized)
			body := decodeResponse[errorResponse](t, response)
			if body.Error != "invalid email or password" {
				t.Fatalf("unexpected authentication error: %+v", body)
			}
		}
	})
}

func TestAccountsAndCategoriesHaveStableOrder(t *testing.T) {
	h := newAPIHarness()
	user := createUser(t, h, "order@example.com")

	firstAccount := createAccount(t, h, user.ID, "Main")
	secondAccount := createAccount(t, h, user.ID, "Savings")
	firstCategory := createCategory(t, h, user.ID, "Salary", "income")
	secondCategory := createCategory(t, h, user.ID, "Food", "expense")

	accountsResponse := h.jsonRequest(t, http.MethodGet, "/accounts?user_id=1", nil)
	assertStatus(t, accountsResponse, http.StatusOK)
	accounts := decodeResponse[[]Account](t, accountsResponse)
	if len(accounts) != 2 || accounts[0].ID != firstAccount.ID || accounts[1].ID != secondAccount.ID {
		t.Fatalf("accounts are not ordered by id: %+v", accounts)
	}
	if accounts[0].Currency != "RUB" {
		t.Fatalf("currency was not normalized: %+v", accounts[0])
	}

	categoriesResponse := h.jsonRequest(t, http.MethodGet, "/categories?user_id=1", nil)
	assertStatus(t, categoriesResponse, http.StatusOK)
	categories := decodeResponse[[]Category](t, categoriesResponse)
	if len(categories) != 2 || categories[0].ID != firstCategory.ID || categories[1].ID != secondCategory.ID {
		t.Fatalf("categories are not ordered by id: %+v", categories)
	}
}

func TestTransactionLifecycleFiltersAndMonthlyReport(t *testing.T) {
	h := newAPIHarness()
	user := createUser(t, h, "workflow@example.com")
	account := createAccount(t, h, user.ID, "Main")
	incomeCategory := createCategory(t, h, user.ID, "Salary", "income")
	expenseCategory := createCategory(t, h, user.ID, "Food", "expense")

	createTransaction := func(categoryID, amount int64, occurredAt, description string) Transaction {
		t.Helper()
		response := h.jsonRequest(t, http.MethodPost, "/transactions", map[string]any{
			"user_id":     user.ID,
			"account_id":  account.ID,
			"category_id": categoryID,
			"amount":      amount,
			"description": description,
			"occurred_at": occurredAt,
		})
		assertStatus(t, response, http.StatusCreated)
		return decodeResponse[Transaction](t, response)
	}

	income := createTransaction(incomeCategory.ID, 15000000, "2026-08-01T09:00:00Z", "Salary")
	expense := createTransaction(expenseCategory.ID, 250000, "2026-08-10T18:30:00Z", "Groceries")
	createTransaction(expenseCategory.ID, 100000, "2026-07-31T23:59:59Z", "Previous month")

	listResponse := h.jsonRequest(t, http.MethodGet, "/transactions?user_id=1&type=expense&from=2026-08-01T00:00:00Z&to=2026-09-01T00:00:00Z&limit=10&offset=0", nil)
	assertStatus(t, listResponse, http.StatusOK)
	list := decodeResponse[transactionListResponse](t, listResponse)
	if list.Count != 1 || list.Total != 1 || list.Items[0].ID != expense.ID {
		t.Fatalf("unexpected filtered transactions: %+v", list)
	}

	reportResponse := h.jsonRequest(t, http.MethodGet, "/reports/monthly?user_id=1&account_id=1&month=2026-08", nil)
	assertStatus(t, reportResponse, http.StatusOK)
	report := decodeResponse[MonthlySummary](t, reportResponse)
	if report.Income != 15000000 || report.Expense != 250000 || report.Balance != 14750000 || report.TransactionCount != 2 {
		t.Fatalf("unexpected monthly report: %+v", report)
	}

	patchResponse := h.jsonRequest(t, http.MethodPatch, "/transactions/2?user_id=1", map[string]any{
		"amount":      300000,
		"description": " Updated groceries ",
	})
	assertStatus(t, patchResponse, http.StatusOK)
	updated := decodeResponse[Transaction](t, patchResponse)
	if updated.Amount != 300000 || updated.Description != "Updated groceries" {
		t.Fatalf("transaction was not updated: %+v", updated)
	}

	deleteResponse := h.jsonRequest(t, http.MethodDelete, "/transactions/1?user_id=1", nil)
	assertStatus(t, deleteResponse, http.StatusNoContent)
	if deleteResponse.Body.Len() != 0 {
		t.Fatalf("204 response must not contain a body: %q", deleteResponse.Body.String())
	}

	deletedResponse := h.jsonRequest(t, http.MethodPatch, "/transactions/1?user_id=1", map[string]any{"amount": 1})
	assertStatus(t, deletedResponse, http.StatusNotFound)

	if income.Type != "income" || expense.Type != "expense" {
		t.Fatalf("transaction type must be derived from category: income=%+v expense=%+v", income, expense)
	}
}

func TestTransactionValidationAndOwnership(t *testing.T) {
	h := newAPIHarness()
	owner := createUser(t, h, "owner@example.com")
	other := createUser(t, h, "other@example.com")
	h.useUser(t, owner.ID)
	account := createAccount(t, h, owner.ID, "Owner account")
	category := createCategory(t, h, owner.ID, "Food", "expense")
	transactionResponse := h.jsonRequest(t, http.MethodPost, "/transactions", map[string]any{
		"user_id":     owner.ID,
		"account_id":  account.ID,
		"category_id": category.ID,
		"amount":      100,
		"occurred_at": "2026-08-18T10:00:00Z",
	})
	assertStatus(t, transactionResponse, http.StatusCreated)
	transaction := decodeResponse[Transaction](t, transactionResponse)

	invalidQueries := []string{
		"/transactions?user_id=1&type=transfer",
		"/transactions?user_id=1&from=not-a-date",
		"/transactions?user_id=1&from=2026-08-01T00:00:00Z&to=2026-08-01T00:00:00Z",
		"/transactions?user_id=1&from=2026-09-01T00:00:00Z&to=2026-08-01T00:00:00Z",
		"/transactions?user_id=1&limit=0",
		"/transactions?user_id=1&limit=101",
		"/transactions?user_id=1&offset=-1",
	}

	for _, path := range []string{
		"/transactions?user_id=1&limit=1&offset=0",
		"/transactions?user_id=1&limit=100&offset=0",
	} {
		t.Run("accepts pagination boundary "+path, func(t *testing.T) {
			response := h.jsonRequest(t, http.MethodGet, path, nil)
			assertStatus(t, response, http.StatusOK)
		})
	}

	t.Run("accepts minimum amount", func(t *testing.T) {
		h.useUser(t, owner.ID)
		response := h.jsonRequest(t, http.MethodPost, "/transactions", map[string]any{
			"user_id":     owner.ID,
			"account_id":  account.ID,
			"category_id": category.ID,
			"amount":      1,
			"occurred_at": "2026-08-18T11:00:00Z",
		})
		assertStatus(t, response, http.StatusCreated)
	})
	for _, path := range invalidQueries {
		t.Run(path, func(t *testing.T) {
			response := h.jsonRequest(t, http.MethodGet, path, nil)
			assertStatus(t, response, http.StatusBadRequest)
		})
	}

	t.Run("rejects account owned by another user", func(t *testing.T) {
		h.useUser(t, other.ID)
		response := h.jsonRequest(t, http.MethodPost, "/transactions", map[string]any{
			"user_id":     other.ID,
			"account_id":  account.ID,
			"category_id": category.ID,
			"amount":      100,
			"occurred_at": "2026-08-18T10:00:00Z",
		})
		assertStatus(t, response, http.StatusForbidden)
	})

	t.Run("rejects invalid currency length", func(t *testing.T) {
		h.useUser(t, owner.ID)
		response := h.jsonRequest(t, http.MethodPost, "/accounts", map[string]any{
			"user_id":  owner.ID,
			"name":     "Invalid currency",
			"currency": "RU",
		})
		assertStatus(t, response, http.StatusBadRequest)
	})

	t.Run("rejects non-letter currency", func(t *testing.T) {
		h.useUser(t, owner.ID)
		response := h.jsonRequest(t, http.MethodPost, "/accounts", map[string]any{
			"user_id":  owner.ID,
			"name":     "Invalid currency",
			"currency": "12$",
		})
		assertStatus(t, response, http.StatusBadRequest)
	})

	t.Run("rejects empty patch", func(t *testing.T) {
		h.useUser(t, owner.ID)
		response := h.jsonRequest(t, http.MethodPatch, "/transactions/1?user_id=1", map[string]any{})
		assertStatus(t, response, http.StatusBadRequest)
	})

	t.Run("prevents another user from updating a transaction", func(t *testing.T) {
		h.useUser(t, other.ID)
		response := h.jsonRequest(t, http.MethodPatch, "/transactions/1?user_id=2", map[string]any{"amount": 200})
		assertStatus(t, response, http.StatusForbidden)
		if transaction.ID != 1 {
			t.Fatalf("unexpected transaction id: %d", transaction.ID)
		}
	})
}

func TestResourceValidationErrors(t *testing.T) {
	h := newAPIHarness()
	user := createUser(t, h, "validation@example.com")
	account := createAccount(t, h, user.ID, "Main")
	category := createCategory(t, h, user.ID, "Food", "expense")

	tests := []struct {
		name   string
		method string
		path   string
		body   any
		status int
	}{
		{name: "missing account body fields", method: http.MethodPost, path: "/accounts", body: map[string]any{}, status: http.StatusBadRequest},
		{name: "invalid category type", method: http.MethodPost, path: "/categories", body: map[string]any{"user_id": user.ID, "name": "Transfer", "type": "transfer"}, status: http.StatusBadRequest},
		{name: "missing transaction identifiers", method: http.MethodPost, path: "/transactions", body: map[string]any{"amount": 1, "occurred_at": "2026-08-18T10:00:00Z"}, status: http.StatusBadRequest},
		{name: "zero transaction amount", method: http.MethodPost, path: "/transactions", body: map[string]any{"user_id": user.ID, "account_id": account.ID, "category_id": category.ID, "amount": 0, "occurred_at": "2026-08-18T10:00:00Z"}, status: http.StatusBadRequest},
		{name: "missing transaction timestamp", method: http.MethodPost, path: "/transactions", body: map[string]any{"user_id": user.ID, "account_id": account.ID, "category_id": category.ID, "amount": 1}, status: http.StatusBadRequest},
		{name: "missing account", method: http.MethodPost, path: "/transactions", body: map[string]any{"user_id": user.ID, "account_id": 999, "category_id": category.ID, "amount": 1, "occurred_at": "2026-08-18T10:00:00Z"}, status: http.StatusNotFound},
		{name: "missing category", method: http.MethodPost, path: "/transactions", body: map[string]any{"user_id": user.ID, "account_id": account.ID, "category_id": 999, "amount": 1, "occurred_at": "2026-08-18T10:00:00Z"}, status: http.StatusNotFound},
		{name: "missing list user id", method: http.MethodGet, path: "/transactions", status: http.StatusBadRequest},
		{name: "invalid account filter", method: http.MethodGet, path: "/transactions?user_id=1&account_id=bad", status: http.StatusBadRequest},
		{name: "invalid category filter", method: http.MethodGet, path: "/transactions?user_id=1&category_id=0", status: http.StatusBadRequest},
		{name: "invalid report month", method: http.MethodGet, path: "/reports/monthly?user_id=1&account_id=1&month=2026-13", status: http.StatusBadRequest},
		{name: "missing report account", method: http.MethodGet, path: "/reports/monthly?user_id=1&account_id=999&month=2026-08", status: http.StatusNotFound},
		{name: "invalid patch transaction id", method: http.MethodPatch, path: "/transactions/bad?user_id=1", body: map[string]any{"amount": 1}, status: http.StatusBadRequest},
		{name: "missing patch transaction", method: http.MethodPatch, path: "/transactions/999?user_id=1", body: map[string]any{"amount": 1}, status: http.StatusNotFound},
		{name: "invalid delete transaction id", method: http.MethodDelete, path: "/transactions/0?user_id=1", status: http.StatusBadRequest},
		{name: "missing delete transaction", method: http.MethodDelete, path: "/transactions/999?user_id=1", status: http.StatusNotFound},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			response := h.jsonRequest(t, testCase.method, testCase.path, testCase.body)
			assertStatus(t, response, testCase.status)
		})
	}
}
