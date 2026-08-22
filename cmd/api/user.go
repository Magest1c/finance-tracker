package main

import (
	"errors"
	"sync"
)

var errUserAlreadyExists = errors.New("user already exists")

type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	PasswordHash string `json:"-"`
}

type userStore struct {
	mu          sync.Mutex
	userByEmail map[string]User
	nextID      int64
}

func newUserStore() *userStore {
	return &userStore{
		userByEmail: make(map[string]User),
		nextID:      1,
	}
}
func (s *userStore) Create(email, name, passwordHash string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.userByEmail[email]; exists {
		return User{}, errUserAlreadyExists
	}
	user := User{
		ID:           s.nextID,
		Email:        email,
		Name:         name,
		PasswordHash: passwordHash,
	}
	s.userByEmail[email] = user
	s.nextID++

	return user, nil
}
func (s *userStore) GetByEmail(email string) (User, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.userByEmail[email]
	return user, ok
}
