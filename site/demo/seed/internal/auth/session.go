// Package auth looks up sessions by token.
package auth

import (
	"errors"
	"sync"
	"time"
)

var (
	ErrNotFound = errors.New("session not found")
	ErrExpired  = errors.New("session expired")
)

type Session struct {
	User    string
	Expires time.Time
}

type Store struct {
	mu       sync.RWMutex
	sessions map[string]Session
	now      func() time.Time
}

func NewStore() *Store {
	return &Store{sessions: map[string]Session{}, now: time.Now}
}

func (s *Store) Put(token string, sess Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[token] = sess
}

func (s *Store) Lookup(token string) (Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[token]
	if !ok {
		return Session{}, ErrNotFound
	}
	if s.now().After(sess.Expires) {
		return Session{}, ErrExpired
	}
	return sess, nil
}
