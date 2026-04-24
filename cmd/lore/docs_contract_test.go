package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCloudFirstDocsContractPrimaryNarrative(t *testing.T) {
	root := repoRootFromCwd(t)

	for _, contract := range cloudFirstDocsContract() {
		content := mustReadFile(t, filepath.Join(root, contract.Path))

		for _, required := range contract.MustContain {
			if !strings.Contains(content, required) {
				t.Fatalf("%s must contain %q", contract.Path, required)
			}
		}

		for _, forbidden := range contract.MustNotContain {
			if strings.Contains(content, forbidden) {
				t.Fatalf("%s must not contain %q", contract.Path, forbidden)
			}
		}
	}
}

func TestCloudFirstDocsContractRetainedPrimitives(t *testing.T) {
	root := repoRootFromCwd(t)
	content := strings.Join([]string{
		mustReadFile(t, filepath.Join(root, "docs", "AGENT-SETUP.md")),
		mustReadFile(t, filepath.Join(root, "docs", "PLUGINS.md")),
		mustReadFile(t, filepath.Join(root, "docs", "ARCHITECTURE.md")),
	}, "\n")

	for _, primitive := range retainedIntegrationPrimitives() {
		if !strings.Contains(content, primitive) {
			t.Fatalf("retained primitive contract must mention %q", primitive)
		}
	}
}

func TestCloudFirstDocsContractRemovedAssetPathsStayGone(t *testing.T) {
	root := repoRootFromCwd(t)

	for _, rel := range docsContractFileSet() {
		content := mustReadFile(t, filepath.Join(root, rel))
		for _, forbidden := range removedSetupAssetReferences() {
			if strings.Contains(content, forbidden) {
				t.Fatalf("%s must not reference removed asset path %q", rel, forbidden)
			}
		}
	}
}
