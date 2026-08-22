package main

import "log/slog"

type application struct {
	config       config
	logger       *slog.Logger
	users        *userStore
	tokens       *tokenStore
	accounts     *accountsStore
	categories   *CategoryStore
	transactions *transactionStore
}
