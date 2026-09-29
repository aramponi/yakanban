package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Handler answers with the user behind the bearer token.
func Handler(store *Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		sess, err := store.Lookup(token)
		switch {
		case errors.Is(err, ErrNotFound):
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		case err != nil:
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		fmt.Fprintln(w, sess.User)
	})
}
