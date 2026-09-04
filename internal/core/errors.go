package core

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Sentinel errors that adapters are expected to wrap so the CLI can map them
// onto exit codes and human-readable messages.
var (
	ErrNotFound      = errors.New("task not found")
	ErrNotConfigured = errors.New("no yakanban board configured")
	ErrUnsupported   = errors.New("operation not supported by this provider")
	ErrUnknownStatus = errors.New("unknown status")
	ErrClaimed       = errors.New("task is claimed by another agent")
	ErrAuth          = errors.New("authentication failed")
	ErrInvalidInput  = errors.New("invalid input")
)

// InvalidValueError reports a value outside the set allowed by the board.
type InvalidValueError struct {
	Field   string
	Value   string
	Allowed []string
}

func (e *InvalidValueError) Error() string {
	if len(e.Allowed) == 0 {
		return fmt.Sprintf("invalid %s %q", e.Field, e.Value)
	}
	return fmt.Sprintf("invalid %s %q (allowed: %s)", e.Field, e.Value, strings.Join(e.Allowed, ", "))
}

func (e *InvalidValueError) Unwrap() error { return ErrInvalidInput }

func parseInt(s string) (int, error) { return strconv.Atoi(strings.TrimSpace(s)) }
