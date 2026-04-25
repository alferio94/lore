package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Session struct {
	ID        string  `json:"id"`
	Project   string  `json:"project"`
	Directory string  `json:"directory"`
	StartedAt string  `json:"started_at"`
	EndedAt   *string `json:"ended_at,omitempty"`
	Summary   *string `json:"summary,omitempty"`
}

type Observation struct {
	ID             int64   `json:"id"`
	SyncID         string  `json:"sync_id"`
	SessionID      string  `json:"session_id"`
	Type           string  `json:"type"`
	Title          string  `json:"title"`
	Content        string  `json:"content"`
	ToolName       *string `json:"tool_name,omitempty"`
	Project        *string `json:"project,omitempty"`
	Scope          string  `json:"scope"`
	TopicKey       *string `json:"topic_key,omitempty"`
	RevisionCount  int     `json:"revision_count"`
	DuplicateCount int     `json:"duplicate_count"`
	LastSeenAt     *string `json:"last_seen_at,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	DeletedAt      *string `json:"deleted_at,omitempty"`
}

type SearchResult struct {
	Observation
	Rank float64 `json:"rank"`
}

type SearchMetadata struct {
	FallbackUsed     bool     `json:"fallback_used"`
	FallbackProjects []string `json:"fallback_projects"`
}

type SearchWithMetadataResult struct {
	Results  []SearchResult `json:"results"`
	Metadata SearchMetadata `json:"metadata"`
}

type SessionSummary struct {
	ID               string  `json:"id"`
	Project          string  `json:"project"`
	StartedAt        string  `json:"started_at"`
	EndedAt          *string `json:"ended_at,omitempty"`
	Summary          *string `json:"summary,omitempty"`
	ObservationCount int     `json:"observation_count"`
}

type Stats struct {
	TotalSessions     int      `json:"total_sessions"`
	TotalObservations int      `json:"total_observations"`
	TotalPrompts      int      `json:"total_prompts"`
	Projects          []string `json:"projects"`
}

type AdminStats struct {
	ActiveProjects       int `json:"active_projects"`
	ActiveSkills         int `json:"active_skills"`
	ObservationsThisWeek int `json:"observations_this_week"`
	SessionsThisWeek     int `json:"sessions_this_week"`
}

type TimelineEntry struct {
	ID             int64   `json:"id"`
	SessionID      string  `json:"session_id"`
	Type           string  `json:"type"`
	Title          string  `json:"title"`
	Content        string  `json:"content"`
	ToolName       *string `json:"tool_name,omitempty"`
	Project        *string `json:"project,omitempty"`
	Scope          string  `json:"scope"`
	TopicKey       *string `json:"topic_key,omitempty"`
	RevisionCount  int     `json:"revision_count"`
	DuplicateCount int     `json:"duplicate_count"`
	LastSeenAt     *string `json:"last_seen_at,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	DeletedAt      *string `json:"deleted_at,omitempty"`
	IsFocus        bool    `json:"is_focus"`
}

type TimelineResult struct {
	Focus        Observation     `json:"focus"`
	Before       []TimelineEntry `json:"before"`
	After        []TimelineEntry `json:"after"`
	SessionInfo  *Session        `json:"session_info"`
	TotalInRange int             `json:"total_in_range"`
}

type SearchOptions struct {
	Type    string `json:"type,omitempty"`
	Project string `json:"project,omitempty"`
	Scope   string `json:"scope,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

type AddObservationParams struct {
	SessionID string `json:"session_id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	ToolName  string `json:"tool_name,omitempty"`
	Project   string `json:"project,omitempty"`
	Scope     string `json:"scope,omitempty"`
	TopicKey  string `json:"topic_key,omitempty"`
}

type UpdateObservationParams struct {
	Type     *string `json:"type,omitempty"`
	Title    *string `json:"title,omitempty"`
	Content  *string `json:"content,omitempty"`
	Project  *string `json:"project,omitempty"`
	Scope    *string `json:"scope,omitempty"`
	TopicKey *string `json:"topic_key,omitempty"`
}

type Prompt struct {
	ID        int64  `json:"id"`
	SyncID    string `json:"sync_id"`
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
	Project   string `json:"project,omitempty"`
	CreatedAt string `json:"created_at"`
}

type AddPromptParams struct {
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
	Project   string `json:"project,omitempty"`
}

type Stack struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

type Category struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

type StackRef = Stack

type CategoryRef = Category

type Skill struct {
	ID           int64         `json:"id"`
	Name         string        `json:"name"`
	DisplayName  string        `json:"display_name"`
	Stacks       []StackRef    `json:"stacks"`
	Categories   []CategoryRef `json:"categories"`
	Triggers     string        `json:"triggers"`
	Content      string        `json:"content"`
	CompactRules string        `json:"compact_rules"`
	Version      int           `json:"version"`
	IsActive     bool          `json:"is_active"`
	ReviewState  string        `json:"review_state"`
	CreatedBy    string        `json:"created_by"`
	ReviewedBy   string        `json:"reviewed_by"`
	ReviewedAt   *string       `json:"reviewed_at,omitempty"`
	ReviewNotes  string        `json:"review_notes"`
	ChangedBy    string        `json:"changed_by"`
	CreatedAt    string        `json:"created_at"`
	UpdatedAt    string        `json:"updated_at"`
}

type SkillVersion struct {
	ID           int64  `json:"id"`
	SkillID      int64  `json:"skill_id"`
	Version      int    `json:"version"`
	Content      string `json:"content"`
	CompactRules string `json:"compact_rules"`
	ChangedBy    string `json:"changed_by"`
	CreatedAt    string `json:"created_at"`
}

type User struct {
	ID        int64  `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	Status    string `json:"status"`
	AvatarURL string `json:"avatar_url"`
	Provider  string `json:"provider"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type UserAuth struct {
	User
	PasswordHash string `json:"-"`
}

const (
	SkillReviewStateDraft         = "draft"
	SkillReviewStatePendingReview = "pending_review"
	SkillReviewStateApproved      = "approved"
	SkillReviewStateRejected      = "rejected"

	UserRoleAdmin     = "admin"
	UserRoleTechLead  = "tech_lead"
	UserRoleDeveloper = "developer"
	UserRoleNA        = "na"

	LegacyUserRoleViewer = "viewer"

	UserStatusPending  = "pending"
	UserStatusActive   = "active"
	UserStatusDisabled = "disabled"
)

type ListSkillsParams struct {
	StackID    *int64 `json:"stack_id,omitempty"`
	CategoryID *int64 `json:"category_id,omitempty"`
	Query      string `json:"query,omitempty"`
}

type CreateSkillParams struct {
	Name         string  `json:"name"`
	DisplayName  string  `json:"display_name"`
	StackIDs     []int64 `json:"stack_ids"`
	CategoryIDs  []int64 `json:"category_ids"`
	Triggers     string  `json:"triggers"`
	Content      string  `json:"content"`
	CompactRules string  `json:"compact_rules"`
	ChangedBy    string  `json:"changed_by"`
}

type UpdateSkillParams struct {
	DisplayName  *string  `json:"display_name,omitempty"`
	StackIDs     *[]int64 `json:"stack_ids,omitempty"`
	CategoryIDs  *[]int64 `json:"category_ids,omitempty"`
	Triggers     *string  `json:"triggers,omitempty"`
	Content      *string  `json:"content,omitempty"`
	CompactRules *string  `json:"compact_rules,omitempty"`
	ChangedBy    string   `json:"changed_by"`
}

const (
	DefaultSyncTargetKey = "cloud"

	SyncLifecycleIdle     = "idle"
	SyncLifecyclePending  = "pending"
	SyncLifecycleRunning  = "running"
	SyncLifecycleHealthy  = "healthy"
	SyncLifecycleDegraded = "degraded"

	SyncEntitySession     = "session"
	SyncEntityObservation = "observation"
	SyncEntityPrompt      = "prompt"

	SyncOpUpsert = "upsert"
	SyncOpDelete = "delete"

	SyncSourceLocal  = "local"
	SyncSourceRemote = "remote"
)

type SyncState struct {
	TargetKey           string  `json:"target_key"`
	Lifecycle           string  `json:"lifecycle"`
	LastEnqueuedSeq     int64   `json:"last_enqueued_seq"`
	LastAckedSeq        int64   `json:"last_acked_seq"`
	LastPulledSeq       int64   `json:"last_pulled_seq"`
	ConsecutiveFailures int     `json:"consecutive_failures"`
	BackoffUntil        *string `json:"backoff_until,omitempty"`
	LeaseOwner          *string `json:"lease_owner,omitempty"`
	LeaseUntil          *string `json:"lease_until,omitempty"`
	LastError           *string `json:"last_error,omitempty"`
	UpdatedAt           string  `json:"updated_at"`
}

type SyncMutation struct {
	Seq        int64   `json:"seq"`
	TargetKey  string  `json:"target_key"`
	Entity     string  `json:"entity"`
	EntityKey  string  `json:"entity_key"`
	Op         string  `json:"op"`
	Payload    string  `json:"payload"`
	Source     string  `json:"source"`
	Project    string  `json:"project"`
	OccurredAt string  `json:"occurred_at"`
	AckedAt    *string `json:"acked_at,omitempty"`
}

type EnrolledProject struct {
	Project    string `json:"project"`
	EnrolledAt string `json:"enrolled_at"`
}

type syncSessionPayload struct {
	ID        string  `json:"id"`
	Project   string  `json:"project"`
	Directory string  `json:"directory"`
	EndedAt   *string `json:"ended_at,omitempty"`
	Summary   *string `json:"summary,omitempty"`
}

type syncObservationPayload struct {
	SyncID     string  `json:"sync_id"`
	SessionID  string  `json:"session_id"`
	Type       string  `json:"type"`
	Title      string  `json:"title"`
	Content    string  `json:"content"`
	ToolName   *string `json:"tool_name,omitempty"`
	Project    *string `json:"project,omitempty"`
	Scope      string  `json:"scope"`
	TopicKey   *string `json:"topic_key,omitempty"`
	Deleted    bool    `json:"deleted,omitempty"`
	DeletedAt  *string `json:"deleted_at,omitempty"`
	HardDelete bool    `json:"hard_delete,omitempty"`
}

type syncPromptPayload struct {
	SyncID    string  `json:"sync_id"`
	SessionID string  `json:"session_id"`
	Content   string  `json:"content"`
	Project   *string `json:"project,omitempty"`
}

type ExportData struct {
	Version      string        `json:"version"`
	ExportedAt   string        `json:"exported_at"`
	Sessions     []Session     `json:"sessions"`
	Observations []Observation `json:"observations"`
	Prompts      []Prompt      `json:"prompts"`
}

type Backend string

const BackendSQLite Backend = "sqlite"

const BackendPostgreSQL Backend = "postgresql"

type Config struct {
	Backend              Backend
	DataDir              string
	DatabaseURL          string
	MaxObservationLength int
	MaxContextResults    int
	MaxSearchResults     int
	DedupeWindow         time.Duration
}

func (cfg Config) SelectedBackend() Backend {
	if strings.TrimSpace(string(cfg.Backend)) == "" {
		return BackendSQLite
	}
	return cfg.Backend
}

func DefaultConfig() (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf("lore: determine home directory: %w", err)
	}
	return defaultConfigFor(filepath.Join(home, ".lore")), nil
}

func FallbackConfig(dataDir string) Config {
	return defaultConfigFor(dataDir)
}

func defaultConfigFor(dataDir string) Config {
	return Config{
		Backend:              BackendSQLite,
		DataDir:              dataDir,
		DatabaseURL:          "",
		MaxObservationLength: 50000,
		MaxContextResults:    20,
		MaxSearchResults:     20,
		DedupeWindow:         15 * time.Minute,
	}
}
