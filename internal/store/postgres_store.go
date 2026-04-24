package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	postgresbackend "github.com/alferio94/lore/internal/store/backend/postgres"
)

type PostgresStore struct {
	db  *sql.DB
	cfg Config
}

var _ Contract = (*PostgresStore)(nil)

func newPostgresStore(cfg Config) (*PostgresStore, error) {
	db, err := postgresbackend.OpenDatabase(cfg.DatabaseURL, sql.Open)
	if err != nil {
		return nil, err
	}
	return &PostgresStore{db: db, cfg: cfg}, nil
}

func (s *PostgresStore) Close() error { return s.db.Close() }

func (s *PostgresStore) Ping(ctx context.Context) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT 1`).Scan(&n); err != nil {
		return err
	}
	return nil
}

func (s *PostgresStore) MaxObservationLength() int { return s.cfg.MaxObservationLength }

func (s *PostgresStore) CreateSession(id, project, directory string) error {
	project, _ = NormalizeProject(project)
	return s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`
			INSERT INTO sessions (id, project, directory)
			VALUES ($1, $2, $3)
			ON CONFLICT (id) DO UPDATE SET
				project = CASE WHEN sessions.project = '' THEN EXCLUDED.project ELSE sessions.project END,
				directory = CASE WHEN sessions.directory = '' THEN EXCLUDED.directory ELSE sessions.directory END
		`, id, project, directory); err != nil {
			return err
		}
		return s.enqueueSyncMutationTx(tx, SyncEntitySession, id, SyncOpUpsert, syncSessionPayload{ID: id, Project: project, Directory: directory})
	})
}

func (s *PostgresStore) EndSession(id, summary string) error {
	return s.withTx(func(tx *sql.Tx) error {
		row := tx.QueryRow(`
			UPDATE sessions
			SET ended_at = to_char(timezone('UTC', now()), 'YYYY-MM-DD HH24:MI:SS'), summary = $1
			WHERE id = $2
			RETURNING project, directory, ended_at, summary
		`, nullableString(summary), id)

		var project, directory, endedAt string
		var storedSummary *string
		if err := row.Scan(&project, &directory, &endedAt, &storedSummary); err != nil {
			if err == sql.ErrNoRows {
				return nil
			}
			return err
		}

		return s.enqueueSyncMutationTx(tx, SyncEntitySession, id, SyncOpUpsert, syncSessionPayload{
			ID:        id,
			Project:   project,
			Directory: directory,
			EndedAt:   &endedAt,
			Summary:   storedSummary,
		})
	})
}

func (s *PostgresStore) GetSession(id string) (*Session, error) {
	row := s.db.QueryRow(`SELECT id, project, directory, started_at, ended_at, summary FROM sessions WHERE id = $1`, id)
	var sess Session
	if err := row.Scan(&sess.ID, &sess.Project, &sess.Directory, &sess.StartedAt, &sess.EndedAt, &sess.Summary); err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *PostgresStore) RecentSessions(project string, limit int) ([]SessionSummary, error) {
	project, _ = NormalizeProject(project)
	if limit <= 0 {
		limit = 5
	}
	return s.sessionSummaries(project, limit)
}

func (s *PostgresStore) AllSessions(project string, limit int) ([]SessionSummary, error) {
	project, _ = NormalizeProject(project)
	if limit <= 0 {
		limit = 50
	}
	return s.sessionSummaries(project, limit)
}

func (s *PostgresStore) sessionSummaries(project string, limit int) ([]SessionSummary, error) {
	query := `
		SELECT s.id, s.project, s.started_at, s.ended_at, s.summary, COUNT(o.id) AS observation_count
		FROM sessions s
		LEFT JOIN observations o ON o.session_id = s.id AND o.deleted_at IS NULL
		WHERE ($1 = '' OR s.project = $1)
		GROUP BY s.id, s.project, s.started_at, s.ended_at, s.summary
		ORDER BY MAX(COALESCE(o.created_at, s.started_at)) DESC
		LIMIT $2
	`
	rows, err := s.db.Query(query, project, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []SessionSummary{}
	for rows.Next() {
		var ss SessionSummary
		if err := rows.Scan(&ss.ID, &ss.Project, &ss.StartedAt, &ss.EndedAt, &ss.Summary, &ss.ObservationCount); err != nil {
			return nil, err
		}
		results = append(results, ss)
	}
	return results, rows.Err()
}

func (s *PostgresStore) AllObservations(project, scope string, limit int) ([]Observation, error) {
	if limit <= 0 {
		limit = s.cfg.MaxContextResults
	}
	return s.queryObservations(`
		SELECT id, COALESCE(sync_id, '') AS sync_id, session_id, type, title, content, tool_name, project,
		       scope, topic_key, revision_count, duplicate_count, last_seen_at, created_at, updated_at, deleted_at
		FROM observations
		WHERE deleted_at IS NULL
		  AND ($1 = '' OR project = $1)
		  AND ($2 = '' OR scope = $2)
		ORDER BY created_at DESC
		LIMIT $3
	`, normalizeProjectOnly(project), normalizeScopeMaybe(scope), limit)
}

func (s *PostgresStore) SessionObservations(sessionID string, limit int) ([]Observation, error) {
	if limit <= 0 {
		limit = 200
	}
	return s.queryObservations(`
		SELECT id, COALESCE(sync_id, '') AS sync_id, session_id, type, title, content, tool_name, project,
		       scope, topic_key, revision_count, duplicate_count, last_seen_at, created_at, updated_at, deleted_at
		FROM observations
		WHERE session_id = $1 AND deleted_at IS NULL
		ORDER BY created_at ASC
		LIMIT $2
	`, sessionID, limit)
}

func (s *PostgresStore) AddObservation(p AddObservationParams) (int64, error) {
	p.Project, _ = NormalizeProject(p.Project)
	title := stripPrivateTags(p.Title)
	content := stripPrivateTags(p.Content)
	if len(content) > s.cfg.MaxObservationLength {
		content = content[:s.cfg.MaxObservationLength] + "... [truncated]"
	}
	scope := normalizeScope(p.Scope)
	normHash := hashNormalized(content)
	topicKey := normalizeTopicKey(p.TopicKey)
	window := int(s.cfg.DedupeWindow.Seconds())
	if window <= 0 {
		window = int((15 * time.Minute).Seconds())
	}

	var observationID int64
	err := s.withTx(func(tx *sql.Tx) error {
		if topicKey != "" {
			var existingID int64
			err := tx.QueryRow(`
				SELECT id
				FROM observations
				WHERE topic_key = $1
				  AND COALESCE(project, '') = COALESCE($2, '')
				  AND scope = $3
				  AND deleted_at IS NULL
				ORDER BY updated_at DESC, created_at DESC
				LIMIT 1
			`, topicKey, nullableString(p.Project), scope).Scan(&existingID)
			if err == nil {
				if _, err := tx.Exec(`
					UPDATE observations
					SET type = $1,
					    title = $2,
					    content = $3,
					    tool_name = $4,
					    topic_key = $5,
					    normalized_hash = $6,
					    revision_count = revision_count + 1,
					    last_seen_at = to_char(timezone('UTC', now()), 'YYYY-MM-DD HH24:MI:SS'),
					    updated_at = to_char(timezone('UTC', now()), 'YYYY-MM-DD HH24:MI:SS')
					WHERE id = $7
				`, p.Type, title, content, nullableString(p.ToolName), nullableString(topicKey), normHash, existingID); err != nil {
					return err
				}
				obs, err := s.getObservationTx(tx, existingID)
				if err != nil {
					return err
				}
				observationID = existingID
				return s.enqueueSyncMutationTx(tx, SyncEntityObservation, obs.SyncID, SyncOpUpsert, observationPayloadFromObservation(obs))
			}
			if err != sql.ErrNoRows {
				return err
			}
		}

		var existingID int64
		err := tx.QueryRow(`
			SELECT id
			FROM observations
			WHERE normalized_hash = $1
			  AND COALESCE(project, '') = COALESCE($2, '')
			  AND scope = $3
			  AND type = $4
			  AND title = $5
			  AND deleted_at IS NULL
			  AND created_at >= to_char(timezone('UTC', now()) - ($6 * interval '1 second'), 'YYYY-MM-DD HH24:MI:SS')
			ORDER BY created_at DESC
			LIMIT 1
		`, normHash, nullableString(p.Project), scope, p.Type, title, window).Scan(&existingID)
		if err == nil {
			if _, err := tx.Exec(`
				UPDATE observations
				SET duplicate_count = duplicate_count + 1,
				    last_seen_at = to_char(timezone('UTC', now()), 'YYYY-MM-DD HH24:MI:SS'),
				    updated_at = to_char(timezone('UTC', now()), 'YYYY-MM-DD HH24:MI:SS')
				WHERE id = $1
			`, existingID); err != nil {
				return err
			}
			obs, err := s.getObservationTx(tx, existingID)
			if err != nil {
				return err
			}
			observationID = existingID
			return s.enqueueSyncMutationTx(tx, SyncEntityObservation, obs.SyncID, SyncOpUpsert, observationPayloadFromObservation(obs))
		}
		if err != sql.ErrNoRows {
			return err
		}

		syncID := newSyncID("obs")
		if err := tx.QueryRow(`
			INSERT INTO observations (sync_id, session_id, type, title, content, tool_name, project, scope, topic_key, normalized_hash, revision_count, duplicate_count, last_seen_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 1, 1,
			        to_char(timezone('UTC', now()), 'YYYY-MM-DD HH24:MI:SS'),
			        to_char(timezone('UTC', now()), 'YYYY-MM-DD HH24:MI:SS'))
			RETURNING id
		`, syncID, p.SessionID, p.Type, title, content, nullableString(p.ToolName), nullableString(p.Project), scope, nullableString(topicKey), normHash).Scan(&observationID); err != nil {
			return err
		}
		obs, err := s.getObservationTx(tx, observationID)
		if err != nil {
			return err
		}
		return s.enqueueSyncMutationTx(tx, SyncEntityObservation, obs.SyncID, SyncOpUpsert, observationPayloadFromObservation(obs))
	})
	if err != nil {
		return 0, err
	}
	return observationID, nil
}

func (s *PostgresStore) PassiveCapture(p PassiveCaptureParams) (*PassiveCaptureResult, error) {
	p.Project, _ = NormalizeProject(p.Project)
	result := &PassiveCaptureResult{}
	learnings := ExtractLearnings(p.Content)
	result.Extracted = len(learnings)
	if len(learnings) == 0 {
		return result, nil
	}

	for _, learning := range learnings {
		var existingID int64
		err := s.db.QueryRow(`
			SELECT id FROM observations
			WHERE normalized_hash = $1
			  AND COALESCE(project, '') = COALESCE($2, '')
			  AND deleted_at IS NULL
			LIMIT 1
		`, hashNormalized(learning), nullableString(p.Project)).Scan(&existingID)
		if err == nil {
			result.Duplicates++
			continue
		}
		if err != nil && err != sql.ErrNoRows {
			return result, err
		}

		title := learning
		if len(title) > 60 {
			title = title[:60] + "..."
		}
		if _, err := s.AddObservation(AddObservationParams{
			SessionID: p.SessionID,
			Type:      "passive",
			Title:     title,
			Content:   learning,
			Project:   p.Project,
			Scope:     "project",
			ToolName:  p.Source,
		}); err != nil {
			return result, fmt.Errorf("passive capture save: %w", err)
		}
		result.Saved++
	}

	return result, nil
}

func (s *PostgresStore) RecentObservations(project, scope string, limit int) ([]Observation, error) {
	if limit <= 0 {
		limit = s.cfg.MaxContextResults
	}
	return s.queryObservations(`
		SELECT id, COALESCE(sync_id, '') AS sync_id, session_id, type, title, content, tool_name, project,
		       scope, topic_key, revision_count, duplicate_count, last_seen_at, created_at, updated_at, deleted_at
		FROM observations
		WHERE deleted_at IS NULL
		  AND ($1 = '' OR project = $1)
		  AND ($2 = '' OR scope = $2)
		ORDER BY created_at DESC
		LIMIT $3
	`, normalizeProjectOnly(project), normalizeScopeMaybe(scope), limit)
}

func (s *PostgresStore) Search(query string, opts SearchOptions) ([]SearchResult, error) {
	return nil, s.unsupported("search")
}

func (s *PostgresStore) SearchWithMetadata(query string, opts SearchOptions) (*SearchWithMetadataResult, error) {
	return nil, s.unsupported("search with metadata")
}

func (s *PostgresStore) GetObservation(id int64) (*Observation, error) {
	row := s.db.QueryRow(`
		SELECT id, COALESCE(sync_id, '') AS sync_id, session_id, type, title, content, tool_name, project,
		       scope, topic_key, revision_count, duplicate_count, last_seen_at, created_at, updated_at, deleted_at
		FROM observations
		WHERE id = $1 AND deleted_at IS NULL
	`, id)
	var o Observation
	if err := scanObservation(row, &o); err != nil {
		return nil, err
	}
	return &o, nil
}

func (s *PostgresStore) UpdateObservation(id int64, p UpdateObservationParams) (*Observation, error) {
	var updated *Observation
	err := s.withTx(func(tx *sql.Tx) error {
		obs, err := s.getObservationTx(tx, id)
		if err != nil {
			return err
		}

		typ := obs.Type
		title := obs.Title
		content := obs.Content
		project := derefString(obs.Project)
		scope := obs.Scope
		topicKey := derefString(obs.TopicKey)

		if p.Type != nil {
			typ = *p.Type
		}
		if p.Title != nil {
			title = stripPrivateTags(*p.Title)
		}
		if p.Content != nil {
			content = stripPrivateTags(*p.Content)
			if len(content) > s.cfg.MaxObservationLength {
				content = content[:s.cfg.MaxObservationLength] + "... [truncated]"
			}
		}
		if p.Project != nil {
			project, _ = NormalizeProject(*p.Project)
		}
		if p.Scope != nil {
			scope = normalizeScope(*p.Scope)
		}
		if p.TopicKey != nil {
			topicKey = normalizeTopicKey(*p.TopicKey)
		}

		if _, err := tx.Exec(`
			UPDATE observations
			SET type = $1,
			    title = $2,
			    content = $3,
			    project = $4,
			    scope = $5,
			    topic_key = $6,
			    normalized_hash = $7,
			    revision_count = revision_count + 1,
			    updated_at = to_char(timezone('UTC', now()), 'YYYY-MM-DD HH24:MI:SS')
			WHERE id = $8 AND deleted_at IS NULL
		`, typ, title, content, nullableString(project), scope, nullableString(topicKey), hashNormalized(content), id); err != nil {
			return err
		}

		updated, err = s.getObservationTx(tx, id)
		if err != nil {
			return err
		}
		return s.enqueueSyncMutationTx(tx, SyncEntityObservation, updated.SyncID, SyncOpUpsert, observationPayloadFromObservation(updated))
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *PostgresStore) DeleteObservation(id int64, hardDelete bool) error {
	return s.withTx(func(tx *sql.Tx) error {
		obs, err := s.getObservationTx(tx, id)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}

		deletedAt := Now()
		if hardDelete {
			if _, err := tx.Exec(`DELETE FROM observations WHERE id = $1`, id); err != nil {
				return err
			}
		} else {
			if err := tx.QueryRow(`
				UPDATE observations
				SET deleted_at = to_char(timezone('UTC', now()), 'YYYY-MM-DD HH24:MI:SS'),
				    updated_at = to_char(timezone('UTC', now()), 'YYYY-MM-DD HH24:MI:SS')
				WHERE id = $1 AND deleted_at IS NULL
				RETURNING deleted_at
			`, id).Scan(&deletedAt); err != nil {
				return err
			}
		}

		return s.enqueueSyncMutationTx(tx, SyncEntityObservation, obs.SyncID, SyncOpDelete, syncObservationPayload{
			SyncID:     obs.SyncID,
			Project:    obs.Project,
			Deleted:    true,
			DeletedAt:  &deletedAt,
			HardDelete: hardDelete,
		})
	})
}

func (s *PostgresStore) Timeline(observationID int64, before, after int) (*TimelineResult, error) {
	if before <= 0 {
		before = 5
	}
	if after <= 0 {
		after = 5
	}

	focus, err := s.GetObservation(observationID)
	if err != nil {
		return nil, fmt.Errorf("timeline: observation #%d not found: %w", observationID, err)
	}

	session, err := s.GetSession(focus.SessionID)
	if err != nil {
		session = nil
	}

	beforeEntries, err := s.queryTimelineEntries(`
		SELECT id, session_id, type, title, content, tool_name, project,
		       scope, topic_key, revision_count, duplicate_count, last_seen_at, created_at, updated_at, deleted_at
		FROM observations
		WHERE session_id = $1 AND id < $2 AND deleted_at IS NULL
		ORDER BY id DESC
		LIMIT $3
	`, focus.SessionID, observationID, before)
	if err != nil {
		return nil, fmt.Errorf("timeline: before query: %w", err)
	}
	for i, j := 0, len(beforeEntries)-1; i < j; i, j = i+1, j-1 {
		beforeEntries[i], beforeEntries[j] = beforeEntries[j], beforeEntries[i]
	}

	afterEntries, err := s.queryTimelineEntries(`
		SELECT id, session_id, type, title, content, tool_name, project,
		       scope, topic_key, revision_count, duplicate_count, last_seen_at, created_at, updated_at, deleted_at
		FROM observations
		WHERE session_id = $1 AND id > $2 AND deleted_at IS NULL
		ORDER BY id ASC
		LIMIT $3
	`, focus.SessionID, observationID, after)
	if err != nil {
		return nil, fmt.Errorf("timeline: after query: %w", err)
	}

	var totalInRange int
	if err := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM observations
		WHERE session_id = $1 AND deleted_at IS NULL
	`, focus.SessionID).Scan(&totalInRange); err != nil {
		return nil, fmt.Errorf("timeline: count query: %w", err)
	}

	return &TimelineResult{
		Focus:        *focus,
		Before:       beforeEntries,
		After:        afterEntries,
		SessionInfo:  session,
		TotalInRange: totalInRange,
	}, nil
}

func (s *PostgresStore) AddPrompt(AddPromptParams) (int64, error) { return 0, s.unsupported("prompts") }
func (s *PostgresStore) RecentPrompts(string, int) ([]Prompt, error) {
	return nil, s.unsupported("prompts")
}
func (s *PostgresStore) SearchPrompts(string, string, int) ([]Prompt, error) {
	return nil, s.unsupported("prompt search")
}
func (s *PostgresStore) FormatContext(string, string) (string, error) {
	return "", s.unsupported("formatted context")
}
func (s *PostgresStore) Stats() (*Stats, error)       { return nil, s.unsupported("stats") }
func (s *PostgresStore) Export() (*ExportData, error) { return nil, s.unsupported("export") }
func (s *PostgresStore) Import(*ExportData) (*ImportResult, error) {
	return nil, s.unsupported("import")
}
func (s *PostgresStore) GetSyncedChunks() (map[string]bool, error) {
	return nil, s.unsupported("synced chunks")
}
func (s *PostgresStore) RecordSyncedChunk(string) error { return s.unsupported("synced chunks") }
func (s *PostgresStore) MigrateProject(string, string) (*MigrateResult, error) {
	return nil, s.unsupported("project migration")
}
func (s *PostgresStore) ListProjectNames() ([]string, error) {
	return nil, s.unsupported("project listing")
}
func (s *PostgresStore) ListProjectsWithStats() ([]ProjectStats, error) {
	return nil, s.unsupported("project stats")
}
func (s *PostgresStore) CountObservationsForProject(string) (int, error) {
	return 0, s.unsupported("project counts")
}
func (s *PostgresStore) PruneProject(string) (*PruneResult, error) {
	return nil, s.unsupported("project pruning")
}
func (s *PostgresStore) MergeProjects([]string, string) (*MergeResult, error) {
	return nil, s.unsupported("project merge")
}
func (s *PostgresStore) ListSkills(ListSkillsParams) ([]Skill, error) {
	return nil, s.unsupported("skills catalog")
}
func (s *PostgresStore) GetSkill(string) (*Skill, error) { return nil, s.unsupported("skills catalog") }
func (s *PostgresStore) CreateSkill(CreateSkillParams) (*Skill, error) {
	return nil, s.unsupported("skills catalog")
}
func (s *PostgresStore) UpdateSkill(string, UpdateSkillParams) (*Skill, error) {
	return nil, s.unsupported("skills catalog")
}
func (s *PostgresStore) DeleteSkill(string, string) error { return s.unsupported("skills catalog") }
func (s *PostgresStore) ListStacks() ([]Stack, error)     { return nil, s.unsupported("stack catalog") }
func (s *PostgresStore) CreateStack(string, string) (*Stack, error) {
	return nil, s.unsupported("stack catalog")
}
func (s *PostgresStore) DeleteStack(int64) error { return s.unsupported("stack catalog") }
func (s *PostgresStore) ListCategories() ([]Category, error) {
	return nil, s.unsupported("category catalog")
}
func (s *PostgresStore) CreateCategory(string, string) (*Category, error) {
	return nil, s.unsupported("category catalog")
}
func (s *PostgresStore) DeleteCategory(int64) error { return s.unsupported("category catalog") }
func (s *PostgresStore) AdminStats() (AdminStats, error) {
	return AdminStats{}, s.unsupported("admin stats")
}
func (s *PostgresStore) UpsertUser(string, string, string, string) (*User, error) {
	return nil, s.unsupported("users")
}
func (s *PostgresStore) ListUsers() ([]User, error) { return nil, s.unsupported("users") }
func (s *PostgresStore) UpdateUserRole(int64, string) (*User, error) {
	return nil, s.unsupported("users")
}

func (s *PostgresStore) unsupported(feature string) error {
	return ErrUnsupportedBackendFeature{Backend: BackendPostgreSQL, Feature: feature}
}

func (s *PostgresStore) withTx(fn func(tx *sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PostgresStore) ensureSyncStateTx(tx *sql.Tx, targetKey string) error {
	_, err := tx.Exec(`
		INSERT INTO sync_state (target_key, lifecycle, updated_at)
		VALUES ($1, $2, to_char(timezone('UTC', now()), 'YYYY-MM-DD HH24:MI:SS'))
		ON CONFLICT (target_key) DO NOTHING
	`, targetKey, SyncLifecycleIdle)
	return err
}

func (s *PostgresStore) enqueueSyncMutationTx(tx *sql.Tx, entity, entityKey, op string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	project := extractProjectFromPayload(payload)
	if err := s.ensureSyncStateTx(tx, DefaultSyncTargetKey); err != nil {
		return err
	}
	var seq int64
	if err := tx.QueryRow(`
		INSERT INTO sync_mutations (target_key, entity, entity_key, op, payload, source, project)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING seq
	`, DefaultSyncTargetKey, entity, entityKey, op, string(encoded), SyncSourceLocal, project).Scan(&seq); err != nil {
		return err
	}
	_, err = tx.Exec(`
		UPDATE sync_state
		SET lifecycle = $1, last_enqueued_seq = $2, updated_at = to_char(timezone('UTC', now()), 'YYYY-MM-DD HH24:MI:SS')
		WHERE target_key = $3
	`, SyncLifecyclePending, seq, DefaultSyncTargetKey)
	return err
}

func (s *PostgresStore) getObservationTx(tx *sql.Tx, id int64) (*Observation, error) {
	row := tx.QueryRow(`
		SELECT id, COALESCE(sync_id, '') AS sync_id, session_id, type, title, content, tool_name, project,
		       scope, topic_key, revision_count, duplicate_count, last_seen_at, created_at, updated_at, deleted_at
		FROM observations
		WHERE id = $1 AND deleted_at IS NULL
	`, id)
	var o Observation
	if err := scanObservation(row, &o); err != nil {
		return nil, err
	}
	return &o, nil
}

func (s *PostgresStore) queryObservations(query string, args ...any) ([]Observation, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []Observation{}
	for rows.Next() {
		var o Observation
		if err := scanObservation(rows, &o); err != nil {
			return nil, err
		}
		results = append(results, o)
	}
	return results, rows.Err()
}

func (s *PostgresStore) queryTimelineEntries(query string, args ...any) ([]TimelineEntry, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []TimelineEntry{}
	for rows.Next() {
		var e TimelineEntry
		if err := rows.Scan(
			&e.ID, &e.SessionID, &e.Type, &e.Title, &e.Content,
			&e.ToolName, &e.Project, &e.Scope, &e.TopicKey, &e.RevisionCount, &e.DuplicateCount, &e.LastSeenAt,
			&e.CreatedAt, &e.UpdatedAt, &e.DeletedAt,
		); err != nil {
			return nil, err
		}
		results = append(results, e)
	}
	return results, rows.Err()
}

func scanObservation(scanner interface{ Scan(dest ...any) error }, o *Observation) error {
	return scanner.Scan(
		&o.ID, &o.SyncID, &o.SessionID, &o.Type, &o.Title, &o.Content,
		&o.ToolName, &o.Project, &o.Scope, &o.TopicKey, &o.RevisionCount, &o.DuplicateCount, &o.LastSeenAt,
		&o.CreatedAt, &o.UpdatedAt, &o.DeletedAt,
	)
}

func normalizeProjectOnly(project string) string {
	project, _ = NormalizeProject(project)
	return project
}

func normalizeScopeMaybe(scope string) string {
	if strings.TrimSpace(scope) == "" {
		return ""
	}
	return normalizeScope(scope)
}
