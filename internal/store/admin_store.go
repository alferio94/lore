package store

// admin_store.go — Phase 2 store methods for the web admin backend.
//
// Contains: ErrNotFound sentinel, DeleteSkill (soft-delete), UpsertUser,
// GetUserByEmail, GetUserByID, ListUsers, UpdateUserRole.

import (
	"database/sql"
	"errors"
	"strings"
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

func scanUserRow(scanner interface{ Scan(dest ...any) error }, u *User) error {
	return scanner.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Status, &u.AvatarURL, &u.Provider, &u.CreatedAt, &u.UpdatedAt)
}

func scanUserAuthRow(scanner interface{ Scan(dest ...any) error }, u *UserAuth) error {
	return scanner.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Status, &u.AvatarURL, &u.Provider, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt)
}

func (s *Store) getUserByEmail(queryDB queryer, email string) (*User, error) {
	rows, err := s.queryHook(queryDB, `
		SELECT id, email, name, role, status, avatar_url, provider, created_at, updated_at
		FROM users WHERE email = ?`, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if rows.Next() {
		var u User
		if err := scanUserRow(rows, &u); err != nil {
			return nil, err
		}
		return &u, rows.Err()
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return nil, ErrNotFound
}

func (s *Store) getUserAuthByEmail(queryDB queryer, email string) (*UserAuth, error) {
	rows, err := s.queryHook(queryDB, `
		SELECT id, email, name, role, status, avatar_url, provider, password_hash, created_at, updated_at
		FROM users WHERE email = ?`, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if rows.Next() {
		var u UserAuth
		if err := scanUserAuthRow(rows, &u); err != nil {
			return nil, err
		}
		return &u, rows.Err()
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return nil, ErrNotFound
}

// ─── UpsertUser ──────────────────────────────────────────────────────────────

func (s *Store) UpsertUser(email, name, avatarURL, provider string) (*User, error) {
	var user *User
	err := s.withTx(func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
			return err
		}

		role := UserRoleDeveloper
		if count == 0 {
			role = UserRoleAdmin
		}

		_, err := s.execHook(tx, `
			INSERT INTO users (email, name, role, status, avatar_url, provider, password_hash)
			VALUES (?, ?, ?, ?, ?, ?, '')
			ON CONFLICT(email) DO UPDATE SET
				name       = excluded.name,
				avatar_url = excluded.avatar_url,
				provider   = excluded.provider,
				updated_at = datetime('now')`,
			email, name, role, UserStatusActive, avatarURL, provider,
		)
		if err != nil {
			return err
		}

		user, err = s.getUserByEmail(tx, email)
		return err
	})
	return user, err
}

func (s *Store) CreatePendingUser(email, name, passwordHash string) (*User, error) {
	var user *User
	err := s.withTx(func(tx *sql.Tx) error {
		_, err := s.execHook(tx, `
			INSERT INTO users (email, name, role, status, password_hash, avatar_url, provider)
			VALUES (?, ?, ?, ?, ?, '', 'password')`,
			email, name, UserRoleNA, UserStatusPending, passwordHash,
		)
		if err != nil {
			return err
		}
		user, err = s.getUserByEmail(tx, email)
		return err
	})
	return user, err
}

// ─── GetUserByEmail ───────────────────────────────────────────────────────────

// GetUserByEmail returns the user matching the given email.
// Returns ErrNotFound if no user exists with that email.
func (s *Store) GetUserByEmail(email string) (*User, error) {
	return s.getUserByEmail(s.db, email)
}

func (s *Store) GetUserAuthByEmail(email string) (*UserAuth, error) {
	return s.getUserAuthByEmail(s.db, email)
}

// ─── GetUserByID ──────────────────────────────────────────────────────────────

// GetUserByID returns the user matching the given ID.
// Returns ErrNotFound if no user exists with that ID.
func (s *Store) GetUserByID(id int64) (*User, error) {
	rows, err := s.queryHook(s.db, `
		SELECT id, email, name, role, status, avatar_url, provider, created_at, updated_at
		FROM users WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if rows.Next() {
		var u User
		if err := scanUserRow(rows, &u); err != nil {
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
		SELECT id, email, name, role, status, avatar_url, provider, created_at, updated_at
		FROM users ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []User
	for rows.Next() {
		var u User
		if err := scanUserRow(rows, &u); err != nil {
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
	current, err := s.GetUserByID(id)
	if err != nil {
		return nil, err
	}
	return s.UpdateUserStatusRole(id, current.Status, role)
}

func (s *Store) UpdateUserStatusRole(id int64, status, role string) (*User, error) {
	var user *User
	err := s.withTx(func(tx *sql.Tx) error {
		result, err := s.execHook(tx,
			`UPDATE users SET role = ?, status = ?, updated_at = datetime('now') WHERE id = ?`,
			role, status, id,
		)
		if err != nil {
			return err
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return ErrNotFound
		}

		dbRows, err := s.queryHook(tx, `
			SELECT id, email, name, role, status, avatar_url, provider, created_at, updated_at
			FROM users WHERE id = ?`, id)
		if err != nil {
			return err
		}
		defer dbRows.Close()
		if !dbRows.Next() {
			if err := dbRows.Err(); err != nil {
				return err
			}
			return ErrNotFound
		}
		var updated User
		if err := scanUserRow(dbRows, &updated); err != nil {
			return err
		}
		user = &updated
		return dbRows.Err()
	})
	return user, err
}

func (s *Store) BootstrapAdmin(email, name, passwordHash string) (*User, error) {
	email = strings.TrimSpace(email)
	if email == "" || strings.TrimSpace(passwordHash) == "" {
		return nil, nil
	}

	var user *User
	err := s.withTx(func(tx *sql.Tx) error {
		rows, err := s.queryHook(tx, `
			SELECT id, email, name, role, status, avatar_url, provider, created_at, updated_at
			FROM users WHERE role = ? ORDER BY id ASC LIMIT 1`, UserRoleAdmin)
		if err != nil {
			return err
		}
		defer rows.Close()
		if rows.Next() {
			var existing User
			if err := scanUserRow(rows, &existing); err != nil {
				return err
			}
			user = &existing
			return rows.Err()
		}
		if err := rows.Err(); err != nil {
			return err
		}

		_, err = s.execHook(tx, `
			INSERT INTO users (email, name, role, status, password_hash, avatar_url, provider)
			VALUES (?, ?, ?, ?, ?, '', 'bootstrap')
			ON CONFLICT(email) DO UPDATE SET
				name = CASE WHEN users.name = '' THEN excluded.name ELSE users.name END,
				role = ?,
				status = ?,
				password_hash = CASE WHEN users.password_hash = '' THEN excluded.password_hash ELSE users.password_hash END,
				updated_at = datetime('now')`,
			email, name, UserRoleAdmin, UserStatusActive, passwordHash, UserRoleAdmin, UserStatusActive,
		)
		if err != nil {
			return err
		}
		user, err = s.getUserByEmail(tx, email)
		return err
	})
	return user, err
}
