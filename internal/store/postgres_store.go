package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	postgresbackend "github.com/alferio94/lore/internal/store/backend/postgres"
	"github.com/lib/pq"
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
	outcome, err := s.SearchWithMetadata(query, opts)
	if err != nil {
		return nil, err
	}
	return outcome.Results, nil
}

func (s *PostgresStore) SearchWithMetadata(query string, opts SearchOptions) (*SearchWithMetadataResult, error) {
	return executeSearchWithMetadata(query, opts, s.searchExact, s.selectFallbackProjects)
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
	rows, err := s.db.Query(`
		SELECT DISTINCT project
		FROM observations
		WHERE project IS NOT NULL AND project != '' AND deleted_at IS NULL
		ORDER BY project
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		results = append(results, name)
	}
	return results, rows.Err()
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

func (s *PostgresStore) loadSkillRelationships(db queryer, skillID int64) ([]StackRef, []CategoryRef, error) {
	stackRows, err := db.Query(`
		SELECT st.id, st.name, st.display_name
		FROM skill_stacks ss
		JOIN stacks st ON st.id = ss.stack_id
		WHERE ss.skill_id = $1
		ORDER BY st.name ASC
	`, skillID)
	if err != nil {
		return nil, nil, err
	}
	defer stackRows.Close()

	stacks := make([]StackRef, 0)
	for stackRows.Next() {
		var ref StackRef
		if err := stackRows.Scan(&ref.ID, &ref.Name, &ref.DisplayName); err != nil {
			return nil, nil, err
		}
		stacks = append(stacks, ref)
	}
	if err := stackRows.Err(); err != nil {
		return nil, nil, err
	}

	categoryRows, err := db.Query(`
		SELECT c.id, c.name, c.display_name
		FROM skill_categories sc
		JOIN categories c ON c.id = sc.category_id
		WHERE sc.skill_id = $1
		ORDER BY c.name ASC
	`, skillID)
	if err != nil {
		return nil, nil, err
	}
	defer categoryRows.Close()

	categories := make([]CategoryRef, 0)
	for categoryRows.Next() {
		var ref CategoryRef
		if err := categoryRows.Scan(&ref.ID, &ref.Name, &ref.DisplayName); err != nil {
			return nil, nil, err
		}
		categories = append(categories, ref)
	}
	if err := categoryRows.Err(); err != nil {
		return nil, nil, err
	}

	return stacks, categories, nil
}

func postgresSkillSearchVector(alias string) string {
	qualify := func(column string) string {
		if alias == "" {
			return column
		}
		return alias + "." + column
	}

	return strings.Join([]string{
		fmt.Sprintf("setweight(to_tsvector('simple', COALESCE(%s, '')), 'A')", qualify("name")),
		fmt.Sprintf("setweight(to_tsvector('simple', COALESCE(%s, '')), 'A')", qualify("display_name")),
		fmt.Sprintf("setweight(to_tsvector('simple', COALESCE(%s, '')), 'B')", qualify("triggers")),
		fmt.Sprintf("setweight(to_tsvector('simple', COALESCE(%s, '')), 'C')", qualify("content")),
	}, " || ")
}

func postgresSkillSearchQuery(query string) string {
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case unicode.IsLetter(r), unicode.IsNumber(r):
			return r
		case strings.ContainsRune("-_./#+", r):
			return r
		default:
			return ' '
		}
	}, query)

	tokens := make([]string, 0)
	for _, token := range strings.Fields(cleaned) {
		tokens = append(tokens, `"`+strings.Trim(token, `"'`)+`"`)
	}
	return strings.Join(tokens, " ")
}

func normalizePostgresCatalogError(err error) error {
	if err == nil {
		return nil
	}

	var pqErr *pq.Error
	if errors.As(err, &pqErr) && string(pqErr.Code) == "23505" {
		if constraint := strings.TrimSpace(pqErr.Constraint); constraint != "" {
			return fmt.Errorf("UNIQUE constraint failed: %s", constraint)
		}
		return fmt.Errorf("UNIQUE constraint failed")
	}

	upper := strings.ToUpper(err.Error())
	if strings.Contains(upper, "DUPLICATE KEY VALUE") || strings.Contains(upper, "UNIQUE") {
		return fmt.Errorf("UNIQUE constraint failed: %w", err)
	}

	return err
}

func scanSkill(scanner interface{ Scan(dest ...any) error }, skill *Skill) error {
	return scanner.Scan(
		&skill.ID,
		&skill.Name,
		&skill.DisplayName,
		&skill.Triggers,
		&skill.Content,
		&skill.CompactRules,
		&skill.Version,
		&skill.IsActive,
		&skill.ChangedBy,
		&skill.CreatedAt,
		&skill.UpdatedAt,
	)
}

func (s *PostgresStore) getSkillByID(db queryer, id int64, metadataOnly bool) (*Skill, error) {
	contentExpr := "content"
	rulesExpr := "compact_rules"
	if metadataOnly {
		contentExpr = `'' AS content`
		rulesExpr = `'' AS compact_rules`
	}

	rows, err := db.Query(fmt.Sprintf(`
		SELECT id, name, display_name, triggers, %s, %s, version, is_active, changed_by, created_at, updated_at
		FROM skills
		WHERE id = $1
	`, contentExpr, rulesExpr), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, sql.ErrNoRows
	}

	var skill Skill
	if err := scanSkill(rows, &skill); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	stacks, categories, err := s.loadSkillRelationships(db, skill.ID)
	if err != nil {
		return nil, err
	}
	skill.Stacks = stacks
	skill.Categories = categories
	return &skill, nil
}

func (s *PostgresStore) getSkillByName(db queryer, name string, metadataOnly bool) (*Skill, error) {
	rows, err := db.Query(`SELECT id FROM skills WHERE name = $1`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, sql.ErrNoRows
	}
	var id int64
	if err := rows.Scan(&id); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return s.getSkillByID(db, id, metadataOnly)
}

func insertSkillRelationshipsTx(tx *sql.Tx, skillID int64, stackIDs, categoryIDs []int64) error {
	for _, stackID := range stackIDs {
		if _, err := tx.Exec(`INSERT INTO skill_stacks (skill_id, stack_id) VALUES ($1, $2)`, skillID, stackID); err != nil {
			return err
		}
	}
	for _, categoryID := range categoryIDs {
		if _, err := tx.Exec(`INSERT INTO skill_categories (skill_id, category_id) VALUES ($1, $2)`, skillID, categoryID); err != nil {
			return err
		}
	}
	return nil
}

func writeSkillVersionTx(tx *sql.Tx, skillID int64, version int, content, compactRules, changedBy string) error {
	_, err := tx.Exec(`
		INSERT INTO skill_versions (skill_id, version, content, compact_rules, changed_by)
		VALUES ($1, $2, $3, $4, $5)
	`, skillID, version, content, compactRules, changedBy)
	return err
}

func (s *PostgresStore) ListSkills(params ListSkillsParams) ([]Skill, error) {
	args := make([]any, 0, 3)
	baseQuery := `
		SELECT sk.id, sk.name, sk.display_name, sk.triggers, '' AS content, '' AS compact_rules,
		       sk.version, sk.is_active, sk.changed_by, sk.created_at, sk.updated_at
		FROM skills sk
	`
	where := []string{"sk.is_active = TRUE"}
	orderBy := "ORDER BY sk.name ASC"

	if params.Query != "" {
		searchQuery := postgresSkillSearchQuery(params.Query)
		if searchQuery == "" {
			return []Skill{}, nil
		}
		vector := postgresSkillSearchVector("sk")
		args = append(args, searchQuery)
		baseQuery = fmt.Sprintf(`
			SELECT sk.id, sk.name, sk.display_name, sk.triggers, '' AS content, '' AS compact_rules,
			       sk.version, sk.is_active, sk.changed_by, sk.created_at, sk.updated_at,
			       ts_rank_cd(%s, websearch_to_tsquery('simple', $1)) AS rank
			FROM skills sk
		`, vector)
		where = append(where, fmt.Sprintf(`%s @@ websearch_to_tsquery('simple', $1)`, vector))
		orderBy = "ORDER BY rank DESC, sk.name ASC"
	}

	if params.StackID != nil {
		args = append(args, *params.StackID)
		where = append(where, fmt.Sprintf("sk.id IN (SELECT skill_id FROM skill_stacks WHERE stack_id = $%d)", len(args)))
	}
	if params.CategoryID != nil {
		args = append(args, *params.CategoryID)
		where = append(where, fmt.Sprintf("sk.id IN (SELECT skill_id FROM skill_categories WHERE category_id = $%d)", len(args)))
	}

	rows, err := s.db.Query(baseQuery+" WHERE "+strings.Join(where, " AND ")+" "+orderBy, args...)
	if err != nil {
		return nil, normalizePostgresCatalogError(err)
	}
	defer rows.Close()

	results := make([]Skill, 0)
	for rows.Next() {
		var skill Skill
		if params.Query != "" {
			var rank float64
			if err := rows.Scan(&skill.ID, &skill.Name, &skill.DisplayName, &skill.Triggers, &skill.Content, &skill.CompactRules, &skill.Version, &skill.IsActive, &skill.ChangedBy, &skill.CreatedAt, &skill.UpdatedAt, &rank); err != nil {
				return nil, err
			}
		} else {
			if err := scanSkill(rows, &skill); err != nil {
				return nil, err
			}
		}
		stacks, categories, err := s.loadSkillRelationships(s.db, skill.ID)
		if err != nil {
			return nil, err
		}
		skill.Stacks = stacks
		skill.Categories = categories
		results = append(results, skill)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (s *PostgresStore) GetSkill(name string) (*Skill, error) {
	return s.getSkillByName(s.db, name, false)
}

func (s *PostgresStore) CreateSkill(params CreateSkillParams) (*Skill, error) {
	changedBy := params.ChangedBy
	if changedBy == "" {
		changedBy = "system"
	}

	var created *Skill
	err := s.withTx(func(tx *sql.Tx) error {
		var skillID int64
		if err := tx.QueryRow(`
			INSERT INTO skills (name, display_name, triggers, content, compact_rules, version, is_active, changed_by)
			VALUES ($1, $2, $3, $4, $5, 1, TRUE, $6)
			RETURNING id
		`, params.Name, params.DisplayName, params.Triggers, params.Content, params.CompactRules, changedBy).Scan(&skillID); err != nil {
			return normalizePostgresCatalogError(err)
		}

		if err := insertSkillRelationshipsTx(tx, skillID, params.StackIDs, params.CategoryIDs); err != nil {
			return normalizePostgresCatalogError(err)
		}
		if err := writeSkillVersionTx(tx, skillID, 1, params.Content, params.CompactRules, changedBy); err != nil {
			return normalizePostgresCatalogError(err)
		}

		skill, err := s.getSkillByID(tx, skillID, false)
		if err != nil {
			return err
		}
		created = skill
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func (s *PostgresStore) UpdateSkill(name string, params UpdateSkillParams) (*Skill, error) {
	changedBy := params.ChangedBy
	if changedBy == "" {
		changedBy = "system"
	}

	var updated *Skill
	err := s.withTx(func(tx *sql.Tx) error {
		var skillID int64
		var currentVersion int
		if err := tx.QueryRow(`SELECT id, version FROM skills WHERE name = $1 FOR UPDATE`, name).Scan(&skillID, &currentVersion); err != nil {
			return err
		}

		newVersion := currentVersion + 1
		setClauses := []string{"version = $1", "changed_by = $2", "updated_at = to_char(timezone('UTC', now()), 'YYYY-MM-DD HH24:MI:SS')"}
		args := []any{newVersion, changedBy}
		placeholder := 3

		if params.DisplayName != nil {
			setClauses = append(setClauses, fmt.Sprintf("display_name = $%d", placeholder))
			args = append(args, *params.DisplayName)
			placeholder++
		}
		if params.Triggers != nil {
			setClauses = append(setClauses, fmt.Sprintf("triggers = $%d", placeholder))
			args = append(args, *params.Triggers)
			placeholder++
		}
		if params.Content != nil {
			setClauses = append(setClauses, fmt.Sprintf("content = $%d", placeholder))
			args = append(args, *params.Content)
			placeholder++
		}
		if params.CompactRules != nil {
			setClauses = append(setClauses, fmt.Sprintf("compact_rules = $%d", placeholder))
			args = append(args, *params.CompactRules)
			placeholder++
		}
		args = append(args, skillID)

		if _, err := tx.Exec("UPDATE skills SET "+strings.Join(setClauses, ", ")+fmt.Sprintf(" WHERE id = $%d", placeholder), args...); err != nil {
			return normalizePostgresCatalogError(err)
		}

		if params.StackIDs != nil {
			if _, err := tx.Exec(`DELETE FROM skill_stacks WHERE skill_id = $1`, skillID); err != nil {
				return err
			}
			if err := insertSkillRelationshipsTx(tx, skillID, *params.StackIDs, nil); err != nil {
				return normalizePostgresCatalogError(err)
			}
		}
		if params.CategoryIDs != nil {
			if _, err := tx.Exec(`DELETE FROM skill_categories WHERE skill_id = $1`, skillID); err != nil {
				return err
			}
			if err := insertSkillRelationshipsTx(tx, skillID, nil, *params.CategoryIDs); err != nil {
				return normalizePostgresCatalogError(err)
			}
		}

		var content string
		var compactRules string
		if err := tx.QueryRow(`SELECT content, compact_rules FROM skills WHERE id = $1`, skillID).Scan(&content, &compactRules); err != nil {
			return err
		}
		if err := writeSkillVersionTx(tx, skillID, newVersion, content, compactRules, changedBy); err != nil {
			return normalizePostgresCatalogError(err)
		}

		skill, err := s.getSkillByID(tx, skillID, false)
		if err != nil {
			return err
		}
		updated = skill
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *PostgresStore) DeleteSkill(name, changedBy string) error {
	if changedBy == "" {
		changedBy = "system"
	}
	result, err := s.db.Exec(`
		UPDATE skills
		SET is_active = FALSE, changed_by = $1, updated_at = to_char(timezone('UTC', now()), 'YYYY-MM-DD HH24:MI:SS')
		WHERE name = $2
	`, changedBy, name)
	if err != nil {
		return normalizePostgresCatalogError(err)
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

func (s *PostgresStore) ListStacks() ([]Stack, error) {
	rows, err := s.db.Query(`SELECT id, name, display_name FROM stacks ORDER BY name ASC`)
	if err != nil {
		return nil, normalizePostgresCatalogError(err)
	}
	defer rows.Close()

	results := make([]Stack, 0)
	for rows.Next() {
		var stack Stack
		if err := rows.Scan(&stack.ID, &stack.Name, &stack.DisplayName); err != nil {
			return nil, err
		}
		results = append(results, stack)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (s *PostgresStore) CreateStack(name, displayName string) (*Stack, error) {
	var id int64
	if err := s.db.QueryRow(`INSERT INTO stacks (name, display_name) VALUES ($1, $2) RETURNING id`, name, displayName).Scan(&id); err != nil {
		if normalized := normalizePostgresCatalogError(err); normalized != err {
			return nil, normalized
		}
		result, execErr := s.db.Exec(`INSERT INTO stacks (name, display_name) VALUES ($1, $2)`, name, displayName)
		if execErr != nil {
			return nil, normalizePostgresCatalogError(execErr)
		}
		id, err = result.LastInsertId()
		if err != nil {
			return nil, err
		}
	}
	return &Stack{ID: id, Name: name, DisplayName: displayName}, nil
}

func (s *PostgresStore) DeleteStack(id int64) error {
	result, err := s.db.Exec(`DELETE FROM stacks WHERE id = $1`, id)
	if err != nil {
		return normalizePostgresCatalogError(err)
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

func (s *PostgresStore) ListCategories() ([]Category, error) {
	rows, err := s.db.Query(`SELECT id, name, display_name FROM categories ORDER BY name ASC`)
	if err != nil {
		return nil, normalizePostgresCatalogError(err)
	}
	defer rows.Close()

	results := make([]Category, 0)
	for rows.Next() {
		var category Category
		if err := rows.Scan(&category.ID, &category.Name, &category.DisplayName); err != nil {
			return nil, err
		}
		results = append(results, category)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (s *PostgresStore) CreateCategory(name, displayName string) (*Category, error) {
	var id int64
	if err := s.db.QueryRow(`INSERT INTO categories (name, display_name) VALUES ($1, $2) RETURNING id`, name, displayName).Scan(&id); err != nil {
		if normalized := normalizePostgresCatalogError(err); normalized != err {
			return nil, normalized
		}
		result, execErr := s.db.Exec(`INSERT INTO categories (name, display_name) VALUES ($1, $2)`, name, displayName)
		if execErr != nil {
			return nil, normalizePostgresCatalogError(execErr)
		}
		id, err = result.LastInsertId()
		if err != nil {
			return nil, err
		}
	}
	return &Category{ID: id, Name: name, DisplayName: displayName}, nil
}

func (s *PostgresStore) DeleteCategory(id int64) error {
	result, err := s.db.Exec(`DELETE FROM categories WHERE id = $1`, id)
	if err != nil {
		return normalizePostgresCatalogError(err)
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

func (s *PostgresStore) AdminStats() (AdminStats, error) {
	var stats AdminStats
	var firstErr error

	if err := s.db.QueryRow(
		`SELECT COUNT(DISTINCT project) FROM observations WHERE project IS NOT NULL AND deleted_at IS NULL`,
	).Scan(&stats.ActiveProjects); err != nil && firstErr == nil {
		firstErr = err
	}

	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM skills WHERE is_active = TRUE`,
	).Scan(&stats.ActiveSkills); err != nil && firstErr == nil {
		firstErr = err
	}

	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM observations WHERE deleted_at IS NULL AND created_at >= to_char(timezone('UTC', now()) - interval '7 days', 'YYYY-MM-DD HH24:MI:SS')`,
	).Scan(&stats.ObservationsThisWeek); err != nil && firstErr == nil {
		firstErr = err
	}

	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM sessions WHERE started_at >= to_char(timezone('UTC', now()) - interval '7 days', 'YYYY-MM-DD HH24:MI:SS')`,
	).Scan(&stats.SessionsThisWeek); err != nil && firstErr == nil {
		firstErr = err
	}

	return stats, firstErr
}

func (s *PostgresStore) UpsertUser(email, name, avatarURL, provider string) (*User, error) {
	var user *User
	err := s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE`); err != nil {
			return err
		}

		var count int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
			return err
		}

		role := UserRoleDeveloper
		if count == 0 {
			role = UserRoleAdmin
		}

		var upserted User
		if err := tx.QueryRow(`
			INSERT INTO users (email, name, role, status, avatar_url, provider, password_hash)
			VALUES ($1, $2, $3, $4, $5, $6, '')
			ON CONFLICT(email) DO UPDATE SET
				name = EXCLUDED.name,
				avatar_url = EXCLUDED.avatar_url,
				provider = EXCLUDED.provider,
				updated_at = to_char(timezone('UTC', now()), 'YYYY-MM-DD HH24:MI:SS')
			RETURNING id, email, name, role, status, avatar_url, provider, created_at, updated_at
		`, email, name, role, UserStatusActive, avatarURL, provider).Scan(
			&upserted.ID,
			&upserted.Email,
			&upserted.Name,
			&upserted.Role,
			&upserted.Status,
			&upserted.AvatarURL,
			&upserted.Provider,
			&upserted.CreatedAt,
			&upserted.UpdatedAt,
		); err != nil {
			return normalizePostgresCatalogError(err)
		}
		user = &upserted
		return nil
	})
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (s *PostgresStore) CreatePendingUser(email, name, passwordHash string) (*User, error) {
	var user *User
	err := s.withTx(func(tx *sql.Tx) error {
		var created User
		if err := tx.QueryRow(`
			INSERT INTO users (email, name, role, status, password_hash, avatar_url, provider)
			VALUES ($1, $2, $3, $4, $5, '', 'password')
			RETURNING id, email, name, role, status, avatar_url, provider, created_at, updated_at
		`, email, name, UserRoleNA, UserStatusPending, passwordHash).Scan(
			&created.ID,
			&created.Email,
			&created.Name,
			&created.Role,
			&created.Status,
			&created.AvatarURL,
			&created.Provider,
			&created.CreatedAt,
			&created.UpdatedAt,
		); err != nil {
			return normalizePostgresCatalogError(err)
		}
		user = &created
		return nil
	})
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (s *PostgresStore) GetUserByEmail(email string) (*User, error) {
	row := s.db.QueryRow(`
		SELECT id, email, name, role, status, avatar_url, provider, created_at, updated_at
		FROM users WHERE email = $1
	`, email)
	var user User
	if err := scanUser(row, &user); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, normalizePostgresCatalogError(err)
	}
	return &user, nil
}

func (s *PostgresStore) GetUserAuthByEmail(email string) (*UserAuth, error) {
	row := s.db.QueryRow(`
		SELECT id, email, name, role, status, avatar_url, provider, password_hash, created_at, updated_at
		FROM users WHERE email = $1
	`, email)
	var user UserAuth
	if err := scanUserAuth(row, &user); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, normalizePostgresCatalogError(err)
	}
	return &user, nil
}

func (s *PostgresStore) GetUserByID(id int64) (*User, error) {
	row := s.db.QueryRow(`
		SELECT id, email, name, role, status, avatar_url, provider, created_at, updated_at
		FROM users WHERE id = $1
	`, id)
	var user User
	if err := scanUser(row, &user); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, normalizePostgresCatalogError(err)
	}
	return &user, nil
}

func (s *PostgresStore) ListUsers() ([]User, error) {
	rows, err := s.db.Query(`
		SELECT id, email, name, role, status, avatar_url, provider, created_at, updated_at
		FROM users
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, normalizePostgresCatalogError(err)
	}
	defer rows.Close()

	results := make([]User, 0)
	for rows.Next() {
		var user User
		if err := scanUser(rows, &user); err != nil {
			return nil, err
		}
		results = append(results, user)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (s *PostgresStore) UpdateUserRole(id int64, role string) (*User, error) {
	current, err := s.GetUserByID(id)
	if err != nil {
		return nil, err
	}
	return s.UpdateUserStatusRole(id, current.Status, role)
}

func (s *PostgresStore) UpdateUserStatusRole(id int64, status, role string) (*User, error) {
	var user *User
	err := s.withTx(func(tx *sql.Tx) error {
		var updated User
		err := tx.QueryRow(`
			UPDATE users
			SET role = $1, status = $2, updated_at = to_char(timezone('UTC', now()), 'YYYY-MM-DD HH24:MI:SS')
			WHERE id = $3
			RETURNING id, email, name, role, status, avatar_url, provider, created_at, updated_at
		`, role, status, id).Scan(
			&updated.ID,
			&updated.Email,
			&updated.Name,
			&updated.Role,
			&updated.Status,
			&updated.AvatarURL,
			&updated.Provider,
			&updated.CreatedAt,
			&updated.UpdatedAt,
		)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return normalizePostgresCatalogError(err)
		}
		user = &updated
		return nil
	})
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (s *PostgresStore) BootstrapAdmin(email, name, passwordHash string) (*User, error) {
	email = strings.TrimSpace(email)
	if email == "" || strings.TrimSpace(passwordHash) == "" {
		return nil, nil
	}

	var user *User
	err := s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE`); err != nil {
			return err
		}

		var existing User
		err := tx.QueryRow(`
			SELECT id, email, name, role, status, avatar_url, provider, created_at, updated_at
			FROM users WHERE role = $1 ORDER BY id ASC LIMIT 1
		`, UserRoleAdmin).Scan(
			&existing.ID,
			&existing.Email,
			&existing.Name,
			&existing.Role,
			&existing.Status,
			&existing.AvatarURL,
			&existing.Provider,
			&existing.CreatedAt,
			&existing.UpdatedAt,
		)
		if err == nil {
			user = &existing
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return normalizePostgresCatalogError(err)
		}

		var bootstrapped User
		if err := tx.QueryRow(`
			INSERT INTO users (email, name, role, status, password_hash, avatar_url, provider)
			VALUES ($1, $2, $3, $4, $5, '', 'bootstrap')
			ON CONFLICT(email) DO UPDATE SET
				name = CASE WHEN users.name = '' THEN EXCLUDED.name ELSE users.name END,
				role = $3,
				status = $4,
				password_hash = CASE WHEN users.password_hash = '' THEN EXCLUDED.password_hash ELSE users.password_hash END,
				updated_at = to_char(timezone('UTC', now()), 'YYYY-MM-DD HH24:MI:SS')
			RETURNING id, email, name, role, status, avatar_url, provider, created_at, updated_at
		`, email, name, UserRoleAdmin, UserStatusActive, passwordHash).Scan(
			&bootstrapped.ID,
			&bootstrapped.Email,
			&bootstrapped.Name,
			&bootstrapped.Role,
			&bootstrapped.Status,
			&bootstrapped.AvatarURL,
			&bootstrapped.Provider,
			&bootstrapped.CreatedAt,
			&bootstrapped.UpdatedAt,
		); err != nil {
			return normalizePostgresCatalogError(err)
		}
		user = &bootstrapped
		return nil
	})
	if err != nil {
		return nil, err
	}
	return user, nil
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

func (s *PostgresStore) searchExact(query string, opts SearchOptions) ([]SearchResult, error) {
	opts.Project = normalizeProjectOnly(opts.Project)
	limit := clampSearchLimit(s.cfg.MaxSearchResults, opts.Limit)

	var directResults []SearchResult
	if strings.Contains(query, "/") {
		rows, err := s.db.Query(`
			SELECT id, COALESCE(sync_id, '') AS sync_id, session_id, type, title, content, tool_name, project,
			       scope, topic_key, revision_count, duplicate_count, last_seen_at, created_at, updated_at, deleted_at
			FROM observations
			WHERE topic_key = $1
			  AND deleted_at IS NULL
			  AND ($2 = '' OR type = $2)
			  AND ($3 = '' OR project = $3)
			  AND ($4 = '' OR scope = $4)
			ORDER BY updated_at DESC
			LIMIT $5
		`, query, opts.Type, opts.Project, normalizeScopeMaybe(opts.Scope), limit)
		if err != nil {
			return nil, fmt.Errorf("search: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var sr SearchResult
			if err := rows.Scan(
				&sr.ID, &sr.SyncID, &sr.SessionID, &sr.Type, &sr.Title, &sr.Content,
				&sr.ToolName, &sr.Project, &sr.Scope, &sr.TopicKey, &sr.RevisionCount, &sr.DuplicateCount,
				&sr.LastSeenAt, &sr.CreatedAt, &sr.UpdatedAt, &sr.DeletedAt,
			); err != nil {
				return nil, err
			}
			sr.Rank = -1000
			directResults = append(directResults, sr)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	trimmedQuery := strings.TrimSpace(query)
	if trimmedQuery == "" {
		if len(directResults) > limit {
			return directResults[:limit], nil
		}
		return directResults, nil
	}

	searchVector := postgresObservationSearchVector("o")
	rows, err := s.db.Query(fmt.Sprintf(`
		SELECT o.id, COALESCE(o.sync_id, '') AS sync_id, o.session_id, o.type, o.title, o.content, o.tool_name, o.project,
		       o.scope, o.topic_key, o.revision_count, o.duplicate_count, o.last_seen_at, o.created_at, o.updated_at, o.deleted_at,
		       ts_rank_cd(%s, websearch_to_tsquery('simple', $1)) AS rank
		FROM observations o
		WHERE o.deleted_at IS NULL
		  AND %s @@ websearch_to_tsquery('simple', $1)
		  AND ($2 = '' OR o.type = $2)
		  AND ($3 = '' OR o.project = $3)
		  AND ($4 = '' OR o.scope = $4)
		ORDER BY rank DESC, o.updated_at DESC
		LIMIT $5
	`, searchVector, searchVector), trimmedQuery, opts.Type, opts.Project, normalizeScopeMaybe(opts.Scope), limit)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()

	seen := make(map[int64]bool, len(directResults))
	results := append([]SearchResult(nil), directResults...)
	for _, result := range directResults {
		seen[result.ID] = true
	}

	for rows.Next() {
		var sr SearchResult
		if err := rows.Scan(
			&sr.ID, &sr.SyncID, &sr.SessionID, &sr.Type, &sr.Title, &sr.Content,
			&sr.ToolName, &sr.Project, &sr.Scope, &sr.TopicKey, &sr.RevisionCount, &sr.DuplicateCount,
			&sr.LastSeenAt, &sr.CreatedAt, &sr.UpdatedAt, &sr.DeletedAt,
			&sr.Rank,
		); err != nil {
			return nil, err
		}
		if seen[sr.ID] {
			continue
		}
		seen[sr.ID] = true
		results = append(results, sr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (s *PostgresStore) selectFallbackProjects(project string) ([]string, error) {
	return executeSelectFallbackProjects(project, s.ListProjectNames)
}

func postgresObservationSearchVector(alias string) string {
	qualify := func(column string) string {
		if alias == "" {
			return column
		}
		return alias + "." + column
	}

	return strings.Join([]string{
		fmt.Sprintf("setweight(to_tsvector('simple', COALESCE(%s, '')), 'A')", qualify("title")),
		fmt.Sprintf("setweight(to_tsvector('simple', COALESCE(%s, '')), 'A')", qualify("topic_key")),
		fmt.Sprintf("setweight(to_tsvector('simple', COALESCE(%s, '')), 'B')", qualify("type")),
		fmt.Sprintf("setweight(to_tsvector('simple', COALESCE(%s, '')), 'B')", qualify("tool_name")),
		fmt.Sprintf("setweight(to_tsvector('simple', COALESCE(%s, '')), 'B')", qualify("project")),
		fmt.Sprintf("setweight(to_tsvector('simple', COALESCE(%s, '')), 'C')", qualify("content")),
	}, " || ")
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

func scanUser(scanner interface{ Scan(dest ...any) error }, user *User) error {
	return scanner.Scan(
		&user.ID,
		&user.Email,
		&user.Name,
		&user.Role,
		&user.Status,
		&user.AvatarURL,
		&user.Provider,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
}

func scanUserAuth(scanner interface{ Scan(dest ...any) error }, user *UserAuth) error {
	return scanner.Scan(
		&user.ID,
		&user.Email,
		&user.Name,
		&user.Role,
		&user.Status,
		&user.AvatarURL,
		&user.Provider,
		&user.PasswordHash,
		&user.CreatedAt,
		&user.UpdatedAt,
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
