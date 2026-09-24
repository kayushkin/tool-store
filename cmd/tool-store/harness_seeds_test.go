package main

import (
	"path/filepath"
	"reflect"
	"testing"

	toolstore "github.com/kayushkin/tool-store"
)

func openSeedTestStore(t *testing.T) *toolstore.Store {
	t.Helper()
	store, err := toolstore.Open(filepath.Join(t.TempDir(), "ts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestSeedHarnessToolsCreatesEveryToolEnabled(t *testing.T) {
	store := openSeedTestStore(t)
	if err := seedHarnessTools(store); err != nil {
		t.Fatal(err)
	}
	for harness, seeds := range map[string][]harnessToolSeed{harnessClaudeCode: claudeCodeTools, harnessCodex: codexTools} {
		rows, err := store.ListTools(toolstore.ListFilter{Kind: toolstore.KindHarness, Harness: harness})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != len(seeds) {
			t.Fatalf("%s: %d rows for %d seeds", harness, len(rows), len(seeds))
		}
		for _, row := range rows {
			if !row.Enabled || row.Description == "" || row.Name != harness+"."+row.HarnessToolName {
				t.Fatalf("seeded row wrong: %+v", row)
			}
		}
	}
	if len(claudeCodeTools) != 28 || len(codexTools) != 12 {
		t.Fatalf("seed set changed size: claude_code %d, codex %d", len(claudeCodeTools), len(codexTools))
	}
}

func TestSeedHarnessToolsTagsExactlyTheShellRunners(t *testing.T) {
	store := openSeedTestStore(t)
	if err := seedHarnessTools(store); err != nil {
		t.Fatal(err)
	}
	rows, err := store.ListTools(toolstore.ListFilter{Kind: toolstore.KindHarness, Tag: tagRunsCommands})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, row := range rows {
		names = append(names, row.Name)
	}
	want := []string{"claude_code.Bash", "claude_code.Monitor", "codex.shell_tool", "codex.unified_exec"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("runs-commands tools = %v, want %v", names, want)
	}
}

func TestSeedHarnessToolsKeepsEnabledAndDescriptionAndRefreshesTags(t *testing.T) {
	store := openSeedTestStore(t)
	stale := &toolstore.Tool{
		Name: "claude_code.Bash", Kind: toolstore.KindHarness, Harness: harnessClaudeCode, HarnessToolName: "Bash",
		Description: "operator's words", Tags: []string{"old"}, Enabled: false,
	}
	if _, err := store.UpsertTool(stale); err != nil {
		t.Fatal(err)
	}
	if err := seedHarnessTools(store); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetToolByName("claude_code.Bash")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != stale.ID || got.Enabled || got.Description != "operator's words" {
		t.Fatalf("seeder overwrote what it must keep: %+v", got)
	}
	if !reflect.DeepEqual(got.Tags, []string{tagEffects, tagRunsCommands}) {
		t.Fatalf("tags not refreshed: %v", got.Tags)
	}
}

func TestSeedHarnessToolsRefusesANameAnotherKindHolds(t *testing.T) {
	store := openSeedTestStore(t)
	if _, err := store.UpsertTool(&toolstore.Tool{Name: "codex.web_search", Kind: toolstore.KindLocal, Local: &toolstore.LocalSpec{Symbol: "x"}}); err != nil {
		t.Fatal(err)
	}
	if err := seedHarnessTools(store); err == nil {
		t.Fatal("seeder took over a kind=local row")
	}
}
