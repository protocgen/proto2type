// Example Go API server using proto2type domain types.
//
// This demonstrates the full workflow:
//
//	.proto with constraints → buf generate → domain types with Validate() → HTTP handler → SQLite
//
// Run:
//
//	go run .
//
// Then:
//
//	curl -s localhost:8080/users -d '{"email":"bad"}' | jq        # → 400 validation error
//	curl -s localhost:8080/users -d '{"email":"alice@example.com","display_name":"Alice","age":25,"role":1}' | jq  # → 201
//	curl -s localhost:8080/users/1 | jq                           # → 200
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	domain "github.com/protocgen/proto2type/examples/go-api-server/gen/user/v1/domain"

	_ "modernc.org/sqlite"
)

func main() {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE users (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		email        TEXT    NOT NULL UNIQUE,
		display_name TEXT    NOT NULL,
		age          INTEGER NOT NULL,
		role         INTEGER NOT NULL DEFAULT 0,
		bio          TEXT
	)`)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /users", handleCreateUser(db))
	mux.HandleFunc("GET /users/{id}", handleGetUser(db))
	mux.HandleFunc("GET /users", handleListUsers(db))

	fmt.Println("Listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

// handleCreateUser decodes JSON into a domain type, validates, and persists.
//
// The key insight: domain.User is a plain Go struct with json tags.
// No proto imports, no reflection, no runtime deps — just Validate().
func handleCreateUser(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var user domain.User
		if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "invalid JSON: " + err.Error(),
			})
			return
		}

		// One line. Checks email format, name length, age range, enum validity.
		if err := user.Validate(); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": err.Error(),
			})
			return
		}

		// user is a clean Go struct — use directly with database/sql.
		result, err := db.Exec(
			`INSERT INTO users (email, display_name, age, role, bio) VALUES (?, ?, ?, ?, ?)`,
			user.Email, user.DisplayName, user.Age, user.Role, user.Bio,
		)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				writeJSON(w, http.StatusConflict, map[string]string{
					"error": "email already exists",
				})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": err.Error(),
			})
			return
		}

		id, _ := result.LastInsertId()
		writeJSON(w, http.StatusCreated, map[string]any{
			"id":   id,
			"user": user,
		})
	}
}

func handleGetUser(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")

		var user domain.User
		var bio sql.NullString
		err := db.QueryRow(
			`SELECT email, display_name, age, role, bio FROM users WHERE id = ?`, id,
		).Scan(&user.Email, &user.DisplayName, &user.Age, &user.Role, &bio)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error": "user not found",
			})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": err.Error(),
			})
			return
		}
		if bio.Valid {
			user.Bio = &bio.String
		}

		writeJSON(w, http.StatusOK, user)
	}
}

func handleListUsers(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`SELECT email, display_name, age, role, bio FROM users ORDER BY rowid`)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": err.Error(),
			})
			return
		}
		defer rows.Close()

		var users []domain.User
		for rows.Next() {
			var u domain.User
			var bio sql.NullString
			if err := rows.Scan(&u.Email, &u.DisplayName, &u.Age, &u.Role, &bio); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{
					"error": err.Error(),
				})
				return
			}
			if bio.Valid {
				u.Bio = &bio.String
			}
			users = append(users, u)
		}

		writeJSON(w, http.StatusOK, users)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
