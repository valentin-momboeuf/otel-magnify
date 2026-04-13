package api

import (
	"encoding/json"
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	return string(hash), err
}

func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "invalid JSON")
		return
	}
	if req.Email == "" || req.Password == "" {
		respondError(w, 400, "email and password are required")
		return
	}

	user, err := a.db.GetUserByEmail(req.Email)
	if err != nil {
		respondError(w, 401, "invalid credentials")
		return
	}

	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		respondError(w, 401, "invalid credentials")
		return
	}

	token, err := a.auth.GenerateToken(user.ID, user.Email, user.Role)
	if err != nil {
		respondError(w, 500, "failed to generate token")
		return
	}

	respondJSON(w, 200, map[string]string{"token": token})
}
