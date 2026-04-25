package store

// Phase 2 — Admin Store tests (TDD)
// All new test code lives here to avoid conflicts with Phase 1 work.

import (
	"database/sql"
	"testing"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

func seedSkillForAdmin(t *testing.T, s *Store, name string) *Skill {
	t.Helper()
	sk, err := s.CreateSkill(CreateSkillParams{
		Name:        name,
		DisplayName: name,
		Triggers:    "trigger",
		Content:     "content for " + name,
		ChangedBy:   "test",
	})
	if err != nil {
		t.Fatalf("seedSkillForAdmin %q: %v", name, err)
	}
	return sk
}

// ─── Task 2.1/2.2 — DeleteSkill ──────────────────────────────────────────────

func TestDeleteSkillSoftDeletesSetsIsActiveFalse(t *testing.T) {
	s := newTestStore(t)
	seedSkillForAdmin(t, s, "my-skill")

	if err := s.DeleteSkill("my-skill", "alice"); err != nil {
		t.Fatalf("DeleteSkill: %v", err)
	}

	// The raw skill row must still exist with is_active=false for audit reads.
	sk, err := s.GetSkillForAudit("my-skill")
	if err != nil {
		t.Fatalf("GetSkillForAudit after delete: %v", err)
	}
	if sk.IsActive {
		t.Fatal("expected IsActive=false after soft-delete, got true")
	}
}

func TestDeleteSkillExcludedFromList(t *testing.T) {
	s := newTestStore(t)
	seedSkillForAdmin(t, s, "visible")
	seedSkillForAdmin(t, s, "gone")

	if err := s.DeleteSkill("gone", "alice"); err != nil {
		t.Fatalf("DeleteSkill: %v", err)
	}

	skills, err := s.ListSkills(ListSkillsParams{})
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}

	for _, sk := range skills {
		if sk.Name == "gone" {
			t.Fatal("expected deleted skill to be excluded from ListSkills, but it was included")
		}
	}

	found := false
	for _, sk := range skills {
		if sk.Name == "visible" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected 'visible' skill to remain in ListSkills after deleting 'gone'")
	}
}

func TestDeleteSkillUnknownNameReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)

	err := s.DeleteSkill("ghost-skill", "alice")
	if err == nil {
		t.Fatal("expected error for unknown skill name, got nil")
	}
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ─── Task 2.3/2.4 — UpsertUser (insert + update) ─────────────────────────────

func TestUpsertUserInsertsNewUser(t *testing.T) {
	s := newTestStore(t)

	user, err := s.UpsertUser("alice@example.com", "Alice", "https://example.com/a.png", "google")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	if user == nil {
		t.Fatal("expected non-nil user")
	}
	if user.ID == 0 {
		t.Fatal("expected non-zero user ID")
	}
	if user.Email != "alice@example.com" {
		t.Fatalf("expected email 'alice@example.com', got %q", user.Email)
	}
	if user.Name != "Alice" {
		t.Fatalf("expected name 'Alice', got %q", user.Name)
	}
	if user.AvatarURL != "https://example.com/a.png" {
		t.Fatalf("expected avatarURL 'https://example.com/a.png', got %q", user.AvatarURL)
	}
	if user.Provider != "google" {
		t.Fatalf("expected provider 'google', got %q", user.Provider)
	}
	if user.Status != UserStatusActive {
		t.Fatalf("expected status %q, got %q", UserStatusActive, user.Status)
	}
}

func TestUpsertUserUpdatesNameAndPreservesRole(t *testing.T) {
	s := newTestStore(t)

	// First insert
	u1, err := s.UpsertUser("alice@example.com", "Alice", "", "google")
	if err != nil {
		t.Fatalf("initial UpsertUser: %v", err)
	}

	// Promote to tech_lead
	if _, err := s.UpdateUserRole(u1.ID, "tech_lead"); err != nil {
		t.Fatalf("UpdateUserRole: %v", err)
	}

	// Upsert again with updated name
	u2, err := s.UpsertUser("alice@example.com", "Alice Renamed", "https://new.png", "google")
	if err != nil {
		t.Fatalf("second UpsertUser: %v", err)
	}

	if u2.Name != "Alice Renamed" {
		t.Fatalf("expected name updated to 'Alice Renamed', got %q", u2.Name)
	}
	// Role/status must be preserved.
	if u2.Role != "tech_lead" {
		t.Fatalf("expected role 'tech_lead' preserved, got %q", u2.Role)
	}
	if u2.Status != UserStatusActive {
		t.Fatalf("expected status %q preserved, got %q", UserStatusActive, u2.Status)
	}
	// Same ID
	if u2.ID != u1.ID {
		t.Fatalf("expected same user ID %d, got %d", u1.ID, u2.ID)
	}
}

func TestCreatePendingUserDefaultsToPendingNAAndAuthLookup(t *testing.T) {
	s := newTestStore(t)

	created, err := s.CreatePendingUser("pending@example.com", "Pending User", "hash-123")
	if err != nil {
		t.Fatalf("CreatePendingUser: %v", err)
	}
	if created.Role != UserRoleNA {
		t.Fatalf("role = %q, want %q", created.Role, UserRoleNA)
	}
	if created.Status != UserStatusPending {
		t.Fatalf("status = %q, want %q", created.Status, UserStatusPending)
	}

	authUser, err := s.GetUserAuthByEmail("pending@example.com")
	if err != nil {
		t.Fatalf("GetUserAuthByEmail: %v", err)
	}
	if authUser.PasswordHash != "hash-123" {
		t.Fatalf("PasswordHash = %q, want hash-123", authUser.PasswordHash)
	}
	if authUser.Role != UserRoleNA {
		t.Fatalf("auth role = %q, want %q", authUser.Role, UserRoleNA)
	}
	if authUser.Status != UserStatusPending {
		t.Fatalf("auth status = %q, want %q", authUser.Status, UserStatusPending)
	}
}

func TestGetUserAuthByEmailNotFoundReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)

	_, err := s.GetUserAuthByEmail("ghost@example.com")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ─── Task 2.5/2.6 — GetUserByEmail + GetUserByID ─────────────────────────────

func TestGetUserByEmailFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.UpsertUser("bob@example.com", "Bob", "", "github")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	user, err := s.GetUserByEmail("bob@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if user.Email != "bob@example.com" {
		t.Fatalf("expected email 'bob@example.com', got %q", user.Email)
	}
	if user.Name != "Bob" {
		t.Fatalf("expected name 'Bob', got %q", user.Name)
	}
	if user.Status != UserStatusActive {
		t.Fatalf("status = %q, want %q", user.Status, UserStatusActive)
	}
}

func TestGetUserByEmailNotFoundReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)

	_, err := s.GetUserByEmail("nobody@example.com")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetUserByIDFound(t *testing.T) {
	s := newTestStore(t)
	created, err := s.UpsertUser("carol@example.com", "Carol", "", "google")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	user, err := s.GetUserByID(created.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if user.ID != created.ID {
		t.Fatalf("expected ID %d, got %d", created.ID, user.ID)
	}
	if user.Email != "carol@example.com" {
		t.Fatalf("expected email 'carol@example.com', got %q", user.Email)
	}
	if user.Status != UserStatusActive {
		t.Fatalf("status = %q, want %q", user.Status, UserStatusActive)
	}
}

func TestGetUserByIDNotFoundReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)

	_, err := s.GetUserByID(99999)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ─── Task 2.7/2.8 — ListUsers + UpdateUserRole ───────────────────────────────

func TestListUsersReturnsAllRows(t *testing.T) {
	s := newTestStore(t)

	_, _ = s.UpsertUser("u1@example.com", "User One", "", "google")
	_, _ = s.UpsertUser("u2@example.com", "User Two", "", "github")
	_, _ = s.UpsertUser("u3@example.com", "User Three", "", "dev")

	users, err := s.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 3 {
		t.Fatalf("expected 3 users, got %d", len(users))
	}
	for i, user := range users {
		if user.Status != UserStatusActive {
			t.Fatalf("users[%d].Status = %q, want %q", i, user.Status, UserStatusActive)
		}
	}
}

func TestListUsersEmptyReturnsEmptySlice(t *testing.T) {
	s := newTestStore(t)

	users, err := s.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers on empty table: %v", err)
	}
	if len(users) != 0 {
		t.Fatalf("expected 0 users on empty table, got %d", len(users))
	}
}

func TestUpdateUserRoleSetsRole(t *testing.T) {
	s := newTestStore(t)
	created, _ := s.UpsertUser("dave@example.com", "Dave", "", "google")

	updated, err := s.UpdateUserRole(created.ID, "admin")
	if err != nil {
		t.Fatalf("UpdateUserRole: %v", err)
	}
	if updated.Role != "admin" {
		t.Fatalf("expected role 'admin', got %q", updated.Role)
	}
	if updated.ID != created.ID {
		t.Fatalf("expected ID %d, got %d", created.ID, updated.ID)
	}
	if updated.Status != UserStatusActive {
		t.Fatalf("expected status %q, got %q", UserStatusActive, updated.Status)
	}
}

func TestUpdateUserRoleUnknownIDReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)

	_, err := s.UpdateUserRole(99999, "admin")
	if err == nil {
		t.Fatal("expected error for unknown user ID, got nil")
	}
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ─── Task 2.9/2.10 — First-user auto-admin ───────────────────────────────────

func TestUpsertUserFirstUserGetsAdminRole(t *testing.T) {
	s := newTestStore(t)

	first, err := s.UpsertUser("first@example.com", "First", "", "google")
	if err != nil {
		t.Fatalf("UpsertUser first: %v", err)
	}
	if first.Role != "admin" {
		t.Fatalf("expected first user to get role 'admin', got %q", first.Role)
	}
}

func TestUpsertUserSecondUserGetsViewerRole(t *testing.T) {
	s := newTestStore(t)

	_, err := s.UpsertUser("first@example.com", "First", "", "google")
	if err != nil {
		t.Fatalf("UpsertUser first: %v", err)
	}

	second, err := s.UpsertUser("second@example.com", "Second", "", "github")
	if err != nil {
		t.Fatalf("UpsertUser second: %v", err)
	}
	if second.Role != UserRoleDeveloper {
		t.Fatalf("expected second user to get role %q, got %q", UserRoleDeveloper, second.Role)
	}
	if second.Status != UserStatusActive {
		t.Fatalf("expected second user to get status %q, got %q", UserStatusActive, second.Status)
	}
}

func TestUpsertUserFirstUserAdminOnRelogin(t *testing.T) {
	s := newTestStore(t)

	// Insert first user (gets admin)
	first, err := s.UpsertUser("first@example.com", "First", "", "google")
	if err != nil {
		t.Fatalf("UpsertUser first: %v", err)
	}
	if first.Role != "admin" {
		t.Fatalf("first insert: expected admin, got %q", first.Role)
	}

	// Re-login (upsert same email) — role must remain admin
	relogin, err := s.UpsertUser("first@example.com", "First Updated", "", "google")
	if err != nil {
		t.Fatalf("UpsertUser relogin: %v", err)
	}
	if relogin.Role != "admin" {
		t.Fatalf("re-login: expected admin role preserved, got %q", relogin.Role)
	}
	if relogin.Status != UserStatusActive {
		t.Fatalf("re-login: expected status %q preserved, got %q", UserStatusActive, relogin.Status)
	}

	// sql.ErrNoRows is not used for users — ErrNotFound is canonical
	_ = sql.ErrNoRows
}

func TestUpdateUserStatusRoleSetsCanonicalFields(t *testing.T) {
	s := newTestStore(t)
	created, err := s.CreatePendingUser("approval@example.com", "Approval", "hash-approval")
	if err != nil {
		t.Fatalf("CreatePendingUser: %v", err)
	}

	updated, err := s.UpdateUserStatusRole(created.ID, UserStatusDisabled, UserRoleTechLead)
	if err != nil {
		t.Fatalf("UpdateUserStatusRole: %v", err)
	}
	if updated.Status != UserStatusDisabled {
		t.Fatalf("status = %q, want %q", updated.Status, UserStatusDisabled)
	}
	if updated.Role != UserRoleTechLead {
		t.Fatalf("role = %q, want %q", updated.Role, UserRoleTechLead)
	}

	reloaded, err := s.GetUserByID(created.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if reloaded.Status != UserStatusDisabled || reloaded.Role != UserRoleTechLead {
		t.Fatalf("reloaded user = %+v, want disabled tech_lead", reloaded)
	}
}

func TestBootstrapAdminCreatesSingleActiveAdminWithoutOverwrite(t *testing.T) {
	s := newTestStore(t)

	bootstrapped, err := s.BootstrapAdmin("admin@example.com", "Bootstrap Admin", "hash-admin")
	if err != nil {
		t.Fatalf("BootstrapAdmin(first): %v", err)
	}
	if bootstrapped == nil {
		t.Fatal("expected bootstrap result")
	}
	if bootstrapped.Role != UserRoleAdmin {
		t.Fatalf("role = %q, want %q", bootstrapped.Role, UserRoleAdmin)
	}
	if bootstrapped.Status != UserStatusActive {
		t.Fatalf("status = %q, want %q", bootstrapped.Status, UserStatusActive)
	}

	authUser, err := s.GetUserAuthByEmail("admin@example.com")
	if err != nil {
		t.Fatalf("GetUserAuthByEmail(first): %v", err)
	}
	if authUser.PasswordHash != "hash-admin" {
		t.Fatalf("PasswordHash = %q, want hash-admin", authUser.PasswordHash)
	}

	again, err := s.BootstrapAdmin("admin@example.com", "Mutated Name", "replacement-hash")
	if err != nil {
		t.Fatalf("BootstrapAdmin(second): %v", err)
	}
	if again.ID != bootstrapped.ID {
		t.Fatalf("second bootstrap id = %d, want %d", again.ID, bootstrapped.ID)
	}
	if again.Name != "Bootstrap Admin" {
		t.Fatalf("second bootstrap name = %q, want original", again.Name)
	}

	reloaded, err := s.GetUserAuthByEmail("admin@example.com")
	if err != nil {
		t.Fatalf("GetUserAuthByEmail(second): %v", err)
	}
	if reloaded.PasswordHash != "hash-admin" {
		t.Fatalf("reloaded PasswordHash = %q, want hash-admin", reloaded.PasswordHash)
	}

	// sql.ErrNoRows is not used for users — ErrNotFound is canonical
	_ = sql.ErrNoRows
}
