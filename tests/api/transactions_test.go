package api_test

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

func prepareFinanceData(t *testing.T, client *apiClient, userID int64) (accountResponse, categoryResponse, categoryResponse) {
	t.Helper()
	accountResult, account := client.createAccount(t, userID, "Main", "rub")
	assertStatus(t, accountResult, http.StatusCreated)
	incomeResult, income := client.createCategory(t, userID, "Salary", "income")
	assertStatus(t, incomeResult, http.StatusCreated)
	expenseResult, expense := client.createCategory(t, userID, "Food", "expense")
	assertStatus(t, expenseResult, http.StatusCreated)
	return account, income, expense
}

func TestTransactionWorkflowAndMonthlyReport(t *testing.T) {
	client := newClient()
	user := registerAndLogin(t, client)
	account, incomeCategory, expenseCategory := prepareFinanceData(t, client, user.ID)

	incomeResult, income := client.createTransaction(t, user.ID, account.ID, incomeCategory.ID, 15_000_000, "2026-08-01T09:00:00Z", "Salary")
	assertStatus(t, incomeResult, http.StatusCreated)
	expenseResult, expense := client.createTransaction(t, user.ID, account.ID, expenseCategory.ID, 250_000, "2026-08-10T18:30:00Z", "Groceries")
	assertStatus(t, expenseResult, http.StatusCreated)
	if income.Type != "income" || expense.Type != "expense" {
		t.Fatalf("transaction type must be derived from category: income=%+v expense=%+v", income, expense)
	}

	query := url.Values{
		"user_id": {fmt.Sprint(user.ID)},
		"type":    {"expense"},
		"from":    {"2026-08-01T00:00:00Z"},
		"to":      {"2026-09-01T00:00:00Z"},
		"limit":   {"10"},
		"offset":  {"0"},
	}
	filteredResponse := client.request(t, http.MethodGet, "/transactions?"+query.Encode(), nil)
	assertStatus(t, filteredResponse, http.StatusOK)
	var filtered transactionListResponse
	filteredResponse.decode(t, &filtered)
	if filtered.Count != 1 || filtered.Total != 1 || len(filtered.Items) != 1 || filtered.Items[0].ID != expense.ID {
		t.Fatalf("unexpected filtered transactions: %+v", filtered)
	}

	reportQuery := url.Values{
		"user_id":    {fmt.Sprint(user.ID)},
		"account_id": {fmt.Sprint(account.ID)},
		"month":      {"2026-08"},
	}
	reportResponse := client.request(t, http.MethodGet, "/reports/monthly?"+reportQuery.Encode(), nil)
	assertStatus(t, reportResponse, http.StatusOK)
	var report monthlySummaryResponse
	reportResponse.decode(t, &report)
	if report.Month != "2026-08" || report.AccountID != account.ID || report.Currency != "RUB" ||
		report.Income != 15_000_000 || report.Expense != 250_000 || report.Balance != 14_750_000 || report.TransactionCount != 2 {
		t.Fatalf("unexpected monthly report: %+v", report)
	}

	updatePath := fmt.Sprintf("/transactions/%d?user_id=%d", expense.ID, user.ID)
	updatedResponse := client.request(t, http.MethodPatch, updatePath, map[string]any{
		"amount": 300_000, "description": " Updated groceries ",
	})
	assertStatus(t, updatedResponse, http.StatusOK)
	var updated transactionResponse
	updatedResponse.decode(t, &updated)
	if updated.Amount != 300_000 || updated.Description != "Updated groceries" {
		t.Fatalf("transaction was not updated: %+v", updated)
	}

	deleted := client.request(t, http.MethodDelete, updatePath, nil)
	assertStatus(t, deleted, http.StatusNoContent)
	if len(deleted.Body) != 0 {
		t.Fatalf("204 response must not contain a body: %q", deleted.Body)
	}
}

func TestTransactionFilterBoundaries(t *testing.T) {
	client := newClient()
	user := registerAndLogin(t, client)
	tests := []url.Values{
		{"type": {"transfer"}},
		{"from": {"not-a-date"}},
		{"from": {"2026-09-01T00:00:00Z"}, "to": {"2026-08-01T00:00:00Z"}},
		{"limit": {"0"}},
		{"limit": {"101"}},
		{"offset": {"-1"}},
	}

	for index, query := range tests {
		t.Run(fmt.Sprintf("case-%d", index+1), func(t *testing.T) {
			query.Set("user_id", fmt.Sprint(user.ID))
			response := client.request(t, http.MethodGet, "/transactions?"+query.Encode(), nil)
			assertStatus(t, response, http.StatusBadRequest)
			var body errorResponse
			response.decode(t, &body)
			if body.Error == "" {
				t.Fatal("expected a non-empty validation error")
			}
		})
	}
}

func TestUserCannotCreateTransactionWithForeignResources(t *testing.T) {
	client := newClient()
	ownerEmail := uniqueEmail()
	ownerRegistration, owner := client.register(t, ownerEmail, "StrongPass123!", "Owner")
	assertStatus(t, ownerRegistration, http.StatusCreated)
	ownerLogin, _ := client.login(t, ownerEmail, "StrongPass123!")
	assertStatus(t, ownerLogin, http.StatusOK)
	account, _, expenseCategory := prepareFinanceData(t, client, owner.ID)

	otherEmail := uniqueEmail()
	otherRegistration, other := client.register(t, otherEmail, "StrongPass123!", "Other")
	assertStatus(t, otherRegistration, http.StatusCreated)
	otherLogin, _ := client.login(t, otherEmail, "StrongPass123!")
	assertStatus(t, otherLogin, http.StatusOK)

	response, _ := client.createTransaction(t, other.ID, account.ID, expenseCategory.ID, 100, "2026-08-18T10:00:00Z", "")
	assertStatus(t, response, http.StatusForbidden)
	assertError(t, response, "account does not belong to user")
}
