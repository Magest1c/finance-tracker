package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (app *application) router() http.Handler {
	router := chi.NewRouter()
	router.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "route not found")
	})
	router.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	})

	router.Get("/health", healthHandler(app.logger))

	router.Post("/auth/register", authRegisterHandler(app.logger, app.users))

	router.Post("/auth/login", authLoginHandler(app.logger, app.users, app.tokens))

	router.Group(func(protected chi.Router) {
		protected.Use(authenticate(app.tokens))
		protected.Post("/accounts", createAccountHandler(app.logger, app.accounts, app.users))
		protected.Get("/accounts", getAccountsHandler(app.logger, app.accounts, app.users))
		protected.Post("/categories", createCategoryHandler(app.logger, app.categories, app.users))
		protected.Get("/categories", getCategoriesHandler(app.logger, app.categories, app.users))
		protected.Post("/transactions", createTransactionHandler(app.logger, app.transactions, app.users, app.accounts, app.categories))
		protected.Get("/transactions", getTransactionsHandler(app.logger, app.transactions, app.users))
		protected.Patch("/transactions/{id}", updateTransactionHandler(app.logger, app.transactions, app.users))
		protected.Delete("/transactions/{id}", deleteTransactionHandler(app.logger, app.transactions, app.users))
		protected.Get("/reports/monthly", monthlySummaryHandler(app.logger, app.transactions, app.users, app.accounts))
	})

	return router
}
