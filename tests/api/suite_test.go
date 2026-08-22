package api_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var (
	projectRoot string
	serverURL   string
	emailNumber uint64
)

type apiClient struct {
	baseURL string
	token   string
	http    *http.Client
}

type apiResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

type userResponse struct {
	Message string `json:"message"`
	ID      int64  `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
}

type loginResponse struct {
	Message string `json:"message"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Token   string `json:"token"`
}

type accountResponse struct {
	ID       int64  `json:"id"`
	UserID   int64  `json:"user_id"`
	Name     string `json:"name"`
	Currency string `json:"currency"`
}

type categoryResponse struct {
	ID     int64  `json:"id"`
	UserID int64  `json:"user_id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
}

type transactionResponse struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"user_id"`
	AccountID   int64     `json:"account_id"`
	CategoryID  int64     `json:"category_id"`
	Type        string    `json:"type"`
	Amount      int64     `json:"amount"`
	Description string    `json:"description"`
	OccurredAt  time.Time `json:"occurred_at"`
}

type transactionListResponse struct {
	Items  []transactionResponse `json:"items"`
	Count  int                   `json:"count"`
	Total  int                   `json:"total"`
	Limit  int                   `json:"limit"`
	Offset int                   `json:"offset"`
}

type monthlySummaryResponse struct {
	Month            string `json:"month"`
	AccountID        int64  `json:"account_id"`
	Currency         string `json:"currency"`
	Income           int64  `json:"income"`
	Expense          int64  `json:"expense"`
	Balance          int64  `json:"balance"`
	TransactionCount int    `json:"transaction_count"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func TestMain(m *testing.M) {
	root, err := locateProjectRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	projectRoot = root

	cacheRoot := filepath.Join(projectRoot, ".cache")
	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "create cache directory: %v\n", err)
		os.Exit(1)
	}
	tempDir, err := os.MkdirTemp(cacheRoot, "go-blackbox-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create black-box temp directory: %v\n", err)
		os.Exit(1)
	}

	binaryName := "finance-tracker"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binaryPath := filepath.Join(tempDir, binaryName)
	goCache := filepath.Join(cacheRoot, "go-build")
	goTemp := filepath.Join(cacheRoot, "go-tmp")
	if err := os.MkdirAll(goCache, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "create Go cache: %v\n", err)
		_ = os.RemoveAll(tempDir)
		os.Exit(1)
	}
	if err := os.MkdirAll(goTemp, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "create Go temp directory: %v\n", err)
		_ = os.RemoveAll(tempDir)
		os.Exit(1)
	}

	build := exec.Command("go", "build", "-trimpath", "-o", binaryPath, "./cmd/api")
	build.Dir = projectRoot
	build.Env = append(os.Environ(), "GOCACHE="+goCache, "GOTMPDIR="+goTemp)
	if output, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build API binary: %v\n%s\n", err, output)
		_ = os.RemoveAll(tempDir)
		os.Exit(1)
	}

	address, err := freeAddress()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		_ = os.RemoveAll(tempDir)
		os.Exit(1)
	}
	serverURL = "http://" + address

	var serverLogs bytes.Buffer
	server := exec.Command(binaryPath)
	server.Dir = projectRoot
	server.Env = append(os.Environ(), "HTTP_ADDR="+address)
	server.Stdout = &serverLogs
	server.Stderr = &serverLogs
	if err := server.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "start API binary: %v\n", err)
		_ = os.RemoveAll(tempDir)
		os.Exit(1)
	}

	if err := waitForHealth(server, serverURL); err != nil {
		_ = server.Process.Kill()
		_ = server.Wait()
		fmt.Fprintf(os.Stderr, "%v\nserver output:\n%s\n", err, serverLogs.String())
		_ = os.RemoveAll(tempDir)
		os.Exit(1)
	}

	code := m.Run()
	if err := server.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		fmt.Fprintf(os.Stderr, "stop API binary: %v\n", err)
		code = 1
	}
	_ = server.Wait()
	if err := os.RemoveAll(tempDir); err != nil {
		fmt.Fprintf(os.Stderr, "remove black-box temp directory: %v\n", err)
		code = 1
	}
	os.Exit(code)
}

func locateProjectRoot() (string, error) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("locate black-box test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..")), nil
}

func freeAddress() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("select free port: %w", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		return "", fmt.Errorf("release selected port: %w", err)
	}
	return address, nil
}

func waitForHealth(server *exec.Cmd, baseURL string) error {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if server.ProcessState != nil && server.ProcessState.Exited() {
			return errors.New("API process exited before becoming healthy")
		}
		response, err := client.Get(baseURL + "/health")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("API did not become healthy within 30 seconds")
}

func newClient() *apiClient {
	return &apiClient{
		baseURL: serverURL,
		http:    &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *apiClient) request(t *testing.T, method, path string, body any) apiResponse {
	t.Helper()

	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode request body: %v", err)
		}
		reader = bytes.NewReader(payload)
	}

	request, err := http.NewRequest(method, c.baseURL+path, reader)
	if err != nil {
		t.Fatalf("create %s %s request: %v", method, path, err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}

	response, err := c.http.Do(request)
	if err != nil {
		t.Fatalf("send %s %s request: %v", method, path, err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s %s response: %v", method, path, err)
	}
	return apiResponse{StatusCode: response.StatusCode, Header: response.Header.Clone(), Body: payload}
}

func (r apiResponse) decode(t *testing.T, target any) {
	t.Helper()
	if err := json.Unmarshal(r.Body, target); err != nil {
		t.Fatalf("decode response %q: %v", r.Body, err)
	}
}

func assertStatus(t *testing.T, response apiResponse, expected int) {
	t.Helper()
	if response.StatusCode != expected {
		t.Fatalf("expected status %d, got %d: %s", expected, response.StatusCode, response.Body)
	}
}

func uniqueEmail() string {
	number := atomic.AddUint64(&emailNumber, 1)
	return fmt.Sprintf("qa-%d-%d@example.com", time.Now().UnixNano(), number)
}

func (c *apiClient) register(t *testing.T, email, password, name string) (apiResponse, userResponse) {
	t.Helper()
	response := c.request(t, http.MethodPost, "/auth/register", map[string]any{
		"email": email, "password": password, "name": name,
	})
	var user userResponse
	if response.StatusCode == http.StatusCreated {
		response.decode(t, &user)
	}
	return response, user
}

func (c *apiClient) login(t *testing.T, email, password string) (apiResponse, loginResponse) {
	t.Helper()
	response := c.request(t, http.MethodPost, "/auth/login", map[string]any{
		"email": email, "password": password,
	})
	var login loginResponse
	if response.StatusCode == http.StatusOK {
		response.decode(t, &login)
		c.token = login.Token
	}
	return response, login
}

func registerAndLogin(t *testing.T, client *apiClient) userResponse {
	t.Helper()
	email := uniqueEmail()
	registration, user := client.register(t, email, "StrongPass123!", "QA Candidate")
	assertStatus(t, registration, http.StatusCreated)
	login, _ := client.login(t, email, "StrongPass123!")
	assertStatus(t, login, http.StatusOK)
	return user
}

func (c *apiClient) createAccount(t *testing.T, userID int64, name, currency string) (apiResponse, accountResponse) {
	t.Helper()
	response := c.request(t, http.MethodPost, "/accounts", map[string]any{
		"user_id": userID, "name": name, "currency": currency,
	})
	var account accountResponse
	if response.StatusCode == http.StatusCreated {
		response.decode(t, &account)
	}
	return response, account
}

func (c *apiClient) createCategory(t *testing.T, userID int64, name, categoryType string) (apiResponse, categoryResponse) {
	t.Helper()
	response := c.request(t, http.MethodPost, "/categories", map[string]any{
		"user_id": userID, "name": name, "type": categoryType,
	})
	var category categoryResponse
	if response.StatusCode == http.StatusCreated {
		response.decode(t, &category)
	}
	return response, category
}

func (c *apiClient) createTransaction(
	t *testing.T,
	userID, accountID, categoryID, amount int64,
	occurredAt, description string,
) (apiResponse, transactionResponse) {
	t.Helper()
	response := c.request(t, http.MethodPost, "/transactions", map[string]any{
		"user_id": userID, "account_id": accountID, "category_id": categoryID,
		"amount": amount, "occurred_at": occurredAt, "description": description,
	})
	var transaction transactionResponse
	if response.StatusCode == http.StatusCreated {
		response.decode(t, &transaction)
	}
	return response, transaction
}

func assertError(t *testing.T, response apiResponse, expected string) {
	t.Helper()
	var body errorResponse
	response.decode(t, &body)
	if body.Error != expected {
		t.Fatalf("expected error %q, got %q", expected, body.Error)
	}
}

func containsJSONContentType(header http.Header) bool {
	return strings.HasPrefix(strings.ToLower(header.Get("Content-Type")), "application/json")
}
