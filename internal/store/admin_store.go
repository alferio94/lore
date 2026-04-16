package store

// admin_store.go — Phase 2 store methods for the web admin backend.
//
// Contains: ErrNotFound sentinel, DeleteSkill (soft-delete), UpsertUser,
// GetUserByEmail, GetUserByID, ListUsers, UpdateUserRole.

import (
	"database/sql"
	"errors"
)

// ErrNotFound is returned by store methods when the requested row does not exist.
var ErrNotFound = errors.New("lore: not found")

// ─── DeleteSkill ─────────────────────────────────────────────────────────────

// DeleteSkill soft-deletes a skill by setting is_active=false and recording
// who requested the deletion via changedBy. Returns ErrNotFound when no skill
// with the given name exists.
func (s *Store) DeleteSkill(name, changedBy string) error {
	if changedBy == "" {
		changedBy = "system"
	}

	result, err := s.execHook(s.db,
		`UPDATE skills SET is_active = 0, changed_by = ?, updated_at = datetime('now') WHERE name = ?`,
		changedBy, name,
	)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// ─── UpsertUser ──────────────────────────────────────────────────────────────

// UpsertUser inserts a new user or updates the name, avatar_url, and provider
// of an existing user identified by email. The role is never overwritten on
// update, preserving any role that was set externally.
//
// First-user bootstrap: when the users table is empty before the insert, the
// new user is assigned role=admin automatically. All subsequent users get the
// default role=viewer.
func (s *Store) UpsertUser(email, name, avatarURL, provider string) (*User, error) {
	var user *User
	err := s.withTx(func(tx *sql.Tx) error {
		// Count existing users to determine first-user bootstrap
		var count int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
			return err
		}

		role := "viewer"
		if count == 0 {
			role = "admin"
		}

		// INSERT ON CONFLICT: update name/avatar/provider but preserve role.
		_, err := s.execHook(tx, `
			INSERT INTO users (email, name, role, avatar_url, provider)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(email) DO UPDATE SET
				name       = excluded.name,
				avatar_url = excluded.avatar_url,
				provider   = excluded.provider,
				updated_at = datetime('now')`,
			email, name, role, avatarURL, provider,
		)
		if err != nil {
			return err
		}

		// Read back the full row (role comes from DB, not local variable)
		rows, err := s.queryHook(tx, `
			SELECT id, email, name, role, avatar_url, provider, created_at, updated_at
			FROM users WHERE email = ?`, email)
		if err != nil {
			return err
		}
		defer rows.Close()

		if rows.Next() {
			var u User
			if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.AvatarURL, &u.Provider, &u.CreatedAt, &u.UpdatedAt); err != nil {
				return err
			}
			user = &u
		}
		return rows.Err()
	})
	return user, err
}

// ─── GetUserByEmail ───────────────────────────────────────────────────────────

// GetUserByEmail returns the user matching the given email.
// Returns ErrNotFound if no user exists with that email.
func (s *Store) GetUserByEmail(email string) (*User, error) {
	rows, err := s.queryHook(s.db, `
		SELECT id, email, name, role, avatar_url, provider, created_at, updated_at
		FROM users WHERE email = ?`, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.AvatarURL, &u.Provider, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		return &u, rows.Err()
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return nil, ErrNotFound
}

// ─── GetUserByID ──────────────────────────────────────────────────────────────

// GetUserByID returns the user matching the given ID.
// Returns ErrNotFound if no user exists with that ID.
func (s *Store) GetUserByID(id int64) (*User, error) {
	rows, err := s.queryHook(s.db, `
		SELECT id, email, name, role, avatar_url, provider, created_at, updated_at
		FROM users WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.AvatarURL, &u.Provider, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		return &u, rows.Err()
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return nil, ErrNotFound
}

// ─── ListUsers ───────────────────────────────────────────────────────────────

// ListUsers returns all users ordered by created_at ASC.
func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.queryItHook(s.db, `
		SELECT id, email, name, role, avatar_url, provider, created_at, updated_at
		FROM users ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.AvatarURL, &u.Provider, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		results = append(results, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if results == nil {
		results = []User{}
	}
	return results, nil
}

// ─── UpdateUserRole ───────────────────────────────────────────────────────────

// UpdateUserRole sets the role of the user with the given ID and returns the
// updated user. Returns ErrNotFound if no user exists with that ID.
func (s *Store) UpdateUserRole(id int64, role string) (*User, error) {
	var user *User
	err := s.withTx(func(tx *sql.Tx) error {
		result, err := s.execHook(tx,
			`UPDATE users SET role = ?, updated_at = datetime('now') WHERE id = ?`,
			role, id,
		)
		if err != nil {
			return err
		}

		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return ErrNotFound
		}

		// Read back the updated row
		dbRows, err := s.queryHook(tx, `
			SELECT id, email, name, role, avatar_url, provider, created_at, updated_at
			FROM users WHERE id = ?`, id)
		if err != nil {
			return err
		}
		defer dbRows.Close()

		if dbRows.Next() {
			var u User
			if err := dbRows.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.AvatarURL, &u.Provider, &u.CreatedAt, &u.UpdatedAt); err != nil {
				return err
			}
			user = &u
		}
		return dbRows.Err()
	})
	return user, err
}
