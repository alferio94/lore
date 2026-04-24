package store

import (
	"strings"
	"testing"
)

func TestPostgresStoreUnsupportedMethodsReturnExplicitError(t *testing.T) {
	s := &PostgresStore{cfg: Config{Backend: BackendPostgreSQL}}

	checks := []struct {
		name string
		err  error
	}{
		{name: "search", err: func() error { _, err := s.Search("auth", SearchOptions{}); return err }()},
		{name: "prompts", err: func() error { _, err := s.AddPrompt(AddPromptParams{}); return err }()},
		{name: "skills catalog", err: func() error { _, err := s.ListSkills(ListSkillsParams{}); return err }()},
	}

	for _, tc := range checks {
		if tc.err == nil {
			t.Fatalf("%s: expected unsupported error", tc.name)
		}
		unsupported, ok := tc.err.(ErrUnsupportedBackendFeature)
		if !ok {
			t.Fatalf("%s: error type = %T, want ErrUnsupportedBackendFeature", tc.name, tc.err)
		}
		if unsupported.Backend != BackendPostgreSQL {
			t.Fatalf("%s: backend = %q, want %q", tc.name, unsupported.Backend, BackendPostgreSQL)
		}
		if !strings.Contains(tc.err.Error(), tc.name) {
			t.Fatalf("%s: error text %q missing feature name", tc.name, tc.err.Error())
		}
	}
}
