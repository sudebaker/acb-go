package api

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

	"github.com/sudebaker/acb-go/internal/db"
)

// adminToken is read once from ACB_ADMIN_TOKEN env var. Used as bootstrap
// token when no agents exist in the DB (chicken-and-egg on fresh installs).
var adminToken string

func init() {
	adminToken = os.Getenv("ACB_ADMIN_TOKEN")
}

func AuthMiddleware(repo *db.AgentRepo) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/health" || r.URL.Path == "/health/" {
				next.ServeHTTP(w, r)
				return
			}

			auth := r.Header.Get("Authorization")
			if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
				WriteError(w, 401, "unauthorized", "missing or invalid authorization header")
				return
			}

			token := strings.TrimPrefix(auth, "Bearer ")

			// Try agent token first (normal flow)
			agent, err := repo.GetByToken(token)
			if err == nil && agent != nil {
				r.Header.Set("X-Agent-Name", agent.Name)
				next.ServeHTTP(w, r)
				return
			}

			// Fallback: admin token bootstrap (for registration when DB is empty/corrupt)
			if adminToken != "" && subtle.ConstantTimeCompare([]byte(token), []byte(adminToken)) == 1 {
				r.Header.Set("X-Agent-Name", "admin")
				next.ServeHTTP(w, r)
				return
			}

			WriteError(w, 401, "unauthorized", "invalid token")
		})
	}
}