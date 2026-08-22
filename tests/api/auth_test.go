package api_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestUserCanRegisterAndLogin(t *testing.T) {
	client := newClient()
	email := uniqueEmail()
	registration, user := client.register(t, "  "+strings.ToUpper(email)+"  ", "StrongPass123!", "QA Candidate")
	assertStatus(t, registration, http.StatusCreated)
	if user.ID <= 0 || user.Email != email || user.Name != "QA Candidate" || user.Message != "user registered" {
		t.Fatalf("unexpected registration response: %+v", user)
	}
	if strings.Contains(string(registration.Body), "password") {
		t.Fatalf("registration response must not expose password data: %s", registration.Body)
	}

	login, body := client.login(t, strings.ToUpper(email), "StrongPass123!")
	assertStatus(t, login, http.StatusOK)
	if body.Message != "login successful" || body.Email != email || len(body.Token) < 40 {
		t.Fatalf("unexpected login response: %+v", body)
	}
}

func TestProtectedEndpointRequiresBearerToken(t *testing.T) {
	response := newClient().request(t, http.MethodGet, "/accounts?user_id=1", nil)
	assertStatus(t, response, http.StatusUnauthorized)
	assertError(t, response, "valid Bearer token is required")
}

func TestRegistrationValidation(t *testing.T) {
	tests := []struct {
		name          string
		email         string
		password      string
		fullName      string
		expectedError string
	}{
		{name: "invalid email", email: "bad", password: "StrongPass123!", fullName: "QA", expectedError: "email must be valid"},
		{name: "short password", email: uniqueEmail(), password: "1234567", fullName: "QA", expectedError: "password must contain at least 8 characters"},
		{name: "missing name", email: uniqueEmail(), password: "StrongPass123!", fullName: "", expectedError: "email, password and name are required"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			response, _ := newClient().register(t, testCase.email, testCase.password, testCase.fullName)
			assertStatus(t, response, http.StatusBadRequest)
			assertError(t, response, testCase.expectedError)
		})
	}
}

func TestUnknownJSONFieldIsRejected(t *testing.T) {
	response := newClient().request(t, http.MethodPost, "/auth/register", map[string]any{
		"email": uniqueEmail(), "password": "StrongPass123!", "name": "QA", "is_admin": true,
	})
	assertStatus(t, response, http.StatusBadRequest)
	assertError(t, response, "invalid JSON body")
}

func TestDuplicateEmailIsCaseInsensitive(t *testing.T) {
	client := newClient()
	email := uniqueEmail()
	first, _ := client.register(t, email, "StrongPass123!", "QA Candidate")
	assertStatus(t, first, http.StatusCreated)
	duplicate, _ := client.register(t, strings.ToUpper(email), "StrongPass123!", "QA Candidate")
	assertStatus(t, duplicate, http.StatusConflict)
	assertError(t, duplicate, "user with this email already exists")
}
