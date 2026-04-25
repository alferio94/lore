package store

import (
	"context"
	"fmt"
)

// Contract is the backend-neutral store boundary consumed by the entrypoints.
// SQLite remains the only implementation today, but callers should depend on
// this contract instead of concrete persistence details.
type Contract interface {
	Close() error
	Ping(ctx context.Context) error
	MaxObservationLength() int
	CreateSession(id, project, directory string) error
	EndSession(id, summary string) error
	GetSession(id string) (*Session, error)
	RecentSessions(project string, limit int) ([]SessionSummary, error)
	AllSessions(project string, limit int) ([]SessionSummary, error)
	AllObservations(project, scope string, limit int) ([]Observation, error)
	SessionObservations(sessionID string, limit int) ([]Observation, error)
	AddObservation(AddObservationParams) (int64, error)
	PassiveCapture(PassiveCaptureParams) (*PassiveCaptureResult, error)
	RecentObservations(project, scope string, limit int) ([]Observation, error)
	Search(query string, opts SearchOptions) ([]SearchResult, error)
	SearchWithMetadata(query string, opts SearchOptions) (*SearchWithMetadataResult, error)
	GetObservation(id int64) (*Observation, error)
	UpdateObservation(id int64, p UpdateObservationParams) (*Observation, error)
	DeleteObservation(id int64, hardDelete bool) error
	Timeline(observationID int64, before, after int) (*TimelineResult, error)
	AddPrompt(AddPromptParams) (int64, error)
	RecentPrompts(project string, limit int) ([]Prompt, error)
	SearchPrompts(query string, project string, limit int) ([]Prompt, error)
	FormatContext(project, scope string) (string, error)
	Stats() (*Stats, error)
	Export() (*ExportData, error)
	Import(data *ExportData) (*ImportResult, error)
	GetSyncedChunks() (map[string]bool, error)
	RecordSyncedChunk(chunkID string) error
	MigrateProject(oldName, newName string) (*MigrateResult, error)
	ListProjectNames() ([]string, error)
	ListProjectsWithStats() ([]ProjectStats, error)
	CountObservationsForProject(name string) (int, error)
	PruneProject(project string) (*PruneResult, error)
	MergeProjects(sources []string, canonical string) (*MergeResult, error)
	ListSkills(params ListSkillsParams) ([]Skill, error)
	GetSkill(name string) (*Skill, error)
	CreateSkill(CreateSkillParams) (*Skill, error)
	UpdateSkill(name string, params UpdateSkillParams) (*Skill, error)
	DeleteSkill(name, changedBy string) error
	ListStacks() ([]Stack, error)
	CreateStack(name, displayName string) (*Stack, error)
	DeleteStack(id int64) error
	ListCategories() ([]Category, error)
	CreateCategory(name, displayName string) (*Category, error)
	DeleteCategory(id int64) error
	AdminStats() (AdminStats, error)
	UpsertUser(email, name, avatarURL, provider string) (*User, error)
	CreatePendingUser(email, name, passwordHash string) (*User, error)
	GetUserByEmail(email string) (*User, error)
	GetUserAuthByEmail(email string) (*UserAuth, error)
	GetUserByID(id int64) (*User, error)
	ListUsers() ([]User, error)
	UpdateUserRole(id int64, role string) (*User, error)
	UpdateUserStatusRole(id int64, status, role string) (*User, error)
	BootstrapAdmin(email, name, passwordHash string) (*User, error)
}

func Open(cfg Config) (Contract, error) {
	switch cfg.SelectedBackend() {
	case BackendSQLite:
		return openSQLiteStore(cfg)
	case BackendPostgreSQL:
		pg, err := newPostgresStore(cfg)
		if err != nil {
			return nil, err
		}
		return pg, nil
	default:
		return nil, ErrUnsupportedBackend{Backend: cfg.SelectedBackend()}
	}
}

var (
	openSQLiteStore = func(cfg Config) (Contract, error) { return New(cfg) }
)

type ErrUnsupportedBackend struct {
	Backend Backend
}

func (e ErrUnsupportedBackend) Error() string {
	return "lore: unsupported store backend " + string(e.Backend)
}

type ErrUnsupportedBackendFeature struct {
	Backend Backend
	Feature string
}

func (e ErrUnsupportedBackendFeature) Error() string {
	feature := e.Feature
	if feature == "" {
		feature = "requested feature"
	}
	return fmt.Sprintf("lore: backend %s does not support %s in this slice", e.Backend, feature)
}
