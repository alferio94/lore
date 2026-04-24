package tui

import (
	"testing"

	"github.com/alferio94/lore/internal/store"
	"github.com/alferio94/lore/internal/version"
)

type testFixture struct {
	store        *store.Store
	sessionID    string
	obsID        int64
	secondObs    int64
	otherSession string
}

func newTestFixture(t *testing.T) testFixture {
	t.Helper()

	cfg, err := store.DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig: %v", err)
	}
	cfg.DataDir = t.TempDir()

	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if err := s.CreateSession("session-1", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("create session-1: %v", err)
	}
	if err := s.CreateSession("session-2", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("create session-2: %v", err)
	}

	obsID, err := s.AddObservation(store.AddObservationParams{
		SessionID: "session-1",
		Type:      "bugfix",
		Title:     "Needle observation",
		Content:   "needle content for deterministic search",
		Project:   "engram",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add first observation: %v", err)
	}

	secondObs, err := s.AddObservation(store.AddObservationParams{
		SessionID: "session-1",
		Type:      "decision",
		Title:     "Second observation",
		Content:   "timeline sibling",
		Project:   "engram",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add second observation: %v", err)
	}

	return testFixture{store: s, sessionID: "session-1", obsID: obsID, secondObs: secondObs, otherSession: "session-2"}
}

func TestNewInitializesModelDefaults(t *testing.T) {
	m := New(nil, "")

	if m.Screen != ScreenDashboard {
		t.Fatalf("screen = %v, want %v", m.Screen, ScreenDashboard)
	}
	if m.SearchInput.Placeholder != "Search memories..." {
		t.Fatalf("placeholder = %q", m.SearchInput.Placeholder)
	}
	if m.SearchInput.CharLimit != 256 {
		t.Fatalf("char limit = %d", m.SearchInput.CharLimit)
	}
	if m.SearchInput.Width != 60 {
		t.Fatalf("width = %d", m.SearchInput.Width)
	}
}

func TestInitReturnsCommand(t *testing.T) {
	m := New(newTestFixture(t).store, "")
	if cmd := m.Init(); cmd == nil {
		t.Fatal("init should return a startup command")
	}
}

func TestDataLoadingCommands(t *testing.T) {
	fx := newTestFixture(t)

	t.Run("checkForUpdate", func(t *testing.T) {
		msg := checkForUpdate("dev")()
		loaded, ok := msg.(updateCheckMsg)
		if !ok {
			t.Fatalf("message type = %T", msg)
		}
		if loaded.result.Status != version.StatusCheckFailed {
			t.Fatalf("status = %q, want %q", loaded.result.Status, version.StatusCheckFailed)
		}
	})

	t.Run("loadStats", func(t *testing.T) {
		msg := loadStats(fx.store)()
		loaded, ok := msg.(statsLoadedMsg)
		if !ok {
			t.Fatalf("message type = %T", msg)
		}
		if loaded.err != nil {
			t.Fatalf("unexpected error: %v", loaded.err)
		}
		if loaded.stats == nil || loaded.stats.TotalSessions < 2 {
			t.Fatalf("unexpected stats: %+v", loaded.stats)
		}
	})

	t.Run("searchMemories", func(t *testing.T) {
		msg := searchMemories(fx.store, "needle")()
		loaded, ok := msg.(searchResultsMsg)
		if !ok {
			t.Fatalf("message type = %T", msg)
		}
		if loaded.err != nil {
			t.Fatalf("unexpected error: %v", loaded.err)
		}
		if loaded.query != "needle" {
			t.Fatalf("query = %q", loaded.query)
		}
		if len(loaded.results) == 0 {
			t.Fatal("expected at least one search result")
		}
	})

	t.Run("loadRecentObservations", func(t *testing.T) {
		msg := loadRecentObservations(fx.store)()
		loaded, ok := msg.(recentObservationsMsg)
		if !ok {
			t.Fatalf("message type = %T", msg)
		}
		if loaded.err != nil {
			t.Fatalf("unexpected error: %v", loaded.err)
		}
		if len(loaded.observations) < 2 {
			t.Fatalf("observations = %d, want >= 2", len(loaded.observations))
		}
	})

	t.Run("loadObservationDetail", func(t *testing.T) {
		msg := loadObservationDetail(fx.store, fx.obsID)()
		loaded, ok := msg.(observationDetailMsg)
		if !ok {
			t.Fatalf("message type = %T", msg)
		}
		if loaded.err != nil {
			t.Fatalf("unexpected error: %v", loaded.err)
		}
		if loaded.observation == nil || loaded.observation.ID != fx.obsID {
			t.Fatalf("unexpected observation: %+v", loaded.observation)
		}
	})

	t.Run("loadTimeline", func(t *testing.T) {
		msg := loadTimeline(fx.store, fx.secondObs)()
		loaded, ok := msg.(timelineMsg)
		if !ok {
			t.Fatalf("message type = %T", msg)
		}
		if loaded.err != nil {
			t.Fatalf("unexpected error: %v", loaded.err)
		}
		if loaded.timeline == nil || loaded.timeline.Focus.ID != fx.secondObs {
			t.Fatalf("unexpected timeline focus: %+v", loaded.timeline)
		}
	})

	t.Run("loadRecentSessions", func(t *testing.T) {
		msg := loadRecentSessions(fx.store)()
		loaded, ok := msg.(recentSessionsMsg)
		if !ok {
			t.Fatalf("message type = %T", msg)
		}
		if loaded.err != nil {
			t.Fatalf("unexpected error: %v", loaded.err)
		}
		if len(loaded.sessions) < 2 {
			t.Fatalf("sessions = %d, want >= 2", len(loaded.sessions))
		}
	})

	t.Run("loadSessionObservations", func(t *testing.T) {
		msg := loadSessionObservations(fx.store, fx.sessionID)()
		loaded, ok := msg.(sessionObservationsMsg)
		if !ok {
			t.Fatalf("message type = %T", msg)
		}
		if loaded.err != nil {
			t.Fatalf("unexpected error: %v", loaded.err)
		}
		if len(loaded.observations) < 2 {
			t.Fatalf("observations = %d, want >= 2", len(loaded.observations))
		}
	})
}
