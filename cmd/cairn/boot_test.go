package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrispian/cairn/profile"
	"github.com/chrispian/cairn/scope"
	"github.com/chrispian/cairn/store"
)

// TestBootEndToEnd is the MVP gate: cairn boot writes a directory that can be
// opened and read. Every assertion below reads what was actually written,
// because a contract asserted against the renderer that produced it is not
// asserted at all.
func TestBootEndToEnd(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	dbPath := filepath.Join(home, "cairn.db")
	bootRoot := filepath.Join(home, "runtime", "boot")
	skillsDir := filepath.Join(home, "skills")
	scopeDir := filepath.Join(home, "repo")

	mustMkdir(t, scopeDir)
	writeSkill(t, skillsDir)
	seed(t, ctx, dbPath, skillsDir, scopeDir)

	var stdout, stderr bytes.Buffer
	err := run(ctx, []string{
		"boot", "engineer",
		"--db", dbPath,
		"--boot-root", bootRoot,
		"--session", "s1",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("boot: %v\nstderr: %s", err, stderr.String())
	}

	dir := strings.TrimSpace(stdout.String())
	want := filepath.Join(bootRoot, "engineer", "s1")
	if dir != want {
		t.Fatalf("boot printed %q, want %q", dir, want)
	}

	// The output contract, read off disk.
	for _, rel := range []string{
		"AGENTS.md",
		"CLAUDE.md",
		"boot.md",
		".mcp.json",
		".claude/settings.json",
		".claude/skills/code-review/SKILL.md",
		".claude/skills/code-review/references/checklist.md",
		".claude/skills/code-review/run.sh",
		"notes/scratch.md",
	} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("the boot directory is missing %s: %v", rel, err)
		}
	}

	if got := read(t, dir, "CLAUDE.md"); got != "@AGENTS.md\n" {
		t.Errorf("CLAUDE.md = %q, want the one-line pointer", got)
	}

	agents := read(t, dir, "AGENTS.md")
	for _, want := range []string{"# Engineer", "base persona", "engineer persona", scopeDir} {
		if !strings.Contains(agents, want) {
			t.Errorf("AGENTS.md does not carry %q:\n%s", want, agents)
		}
	}

	if got := read(t, dir, "boot.md"); !strings.Contains(got, "the standing note") {
		t.Errorf("boot.md does not carry the assembled slot:\n%s", got)
	}

	var mcp struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(read(t, dir, ".mcp.json")), &mcp); err != nil {
		t.Fatalf(".mcp.json is not JSON: %v", err)
	}
	if got, ok := mcp.MCPServers["vanta"]; !ok || got.Command != "vanta-mcp" {
		t.Errorf(".mcp.json did not carry the declared server: %+v", mcp)
	}

	// Settings are written verbatim: the operator's bytes, not Cairn's opinion
	// of them.
	if got := read(t, dir, ".claude/settings.json"); !strings.Contains(got, `"whateverTheOperatorWrote"`) {
		t.Errorf("settings.json was not written verbatim:\n%s", got)
	}

	// A skill's executable bit is load-bearing: a script that arrives without
	// it is a skill that fails halfway through instead of at boot.
	info, err := os.Stat(filepath.Join(dir, ".claude", "skills", "code-review", "run.sh"))
	if err != nil {
		t.Fatalf("stat the planted script: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("the planted run.sh lost its executable bit: mode %v", info.Mode().Perm())
	}
}

// TestBootRefusals covers the three ways a boot is refused: an abstract
// profile, a target that names nothing, and a boot directory that would land
// inside the scope.
func TestBootRefusals(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	dbPath := filepath.Join(home, "cairn.db")
	skillsDir := filepath.Join(home, "skills")
	scopeDir := filepath.Join(home, "repo")
	mustMkdir(t, scopeDir)
	writeSkill(t, skillsDir)
	seed(t, ctx, dbPath, skillsDir, scopeDir)

	t.Run("an abstract profile", func(t *testing.T) {
		var out, errOut bytes.Buffer
		err := run(ctx, []string{"boot", "base", "--db", dbPath, "--boot-root", filepath.Join(home, "b1")}, &out, &errOut)
		if err == nil || !strings.Contains(err.Error(), "abstract") {
			t.Errorf("booting an abstract profile = %v, want a refusal naming it as abstract", err)
		}
	})

	t.Run("a target that names nothing", func(t *testing.T) {
		var out, errOut bytes.Buffer
		err := run(ctx, []string{"boot", "nobody", "--db", dbPath, "--boot-root", filepath.Join(home, "b2")}, &out, &errOut)
		if err == nil || !strings.Contains(err.Error(), "nobody") {
			t.Errorf("booting an unknown target = %v, want a refusal naming it", err)
		}
	})

	t.Run("a boot directory inside the scope", func(t *testing.T) {
		var out, errOut bytes.Buffer
		err := run(ctx, []string{
			"boot", "engineer",
			"--db", dbPath,
			"--boot-root", filepath.Join(scopeDir, "runtime", "boot"),
			"--session", "s1",
		}, &out, &errOut)
		if !errors.Is(err, scope.ErrInsideScope) {
			t.Errorf("planting inside the scope = %v, want ErrInsideScope", err)
		}
	})
}

// TestInstallIsNotImplemented pins the honest failure. cairn install is
// human-executed, permanently, and until it is written it must say so rather
// than doing something approximate.
func TestInstallIsNotImplemented(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := run(context.Background(), []string{"install"}, &out, &errOut); err == nil {
		t.Error("install reported success without being implemented")
	}
	if err := run(context.Background(), []string{"install", "--check"}, &out, &errOut); err == nil {
		t.Error("install --check reported success without being implemented")
	}
}

// seed writes the two profiles and the binding the tests above boot.
func seed(t *testing.T, ctx context.Context, dbPath, skillsDir, scopeDir string) {
	t.Helper()
	st, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	defer func() { _ = st.Close() }()

	base := profile.Profile{
		ID:       "base",
		Abstract: true,
		Name:     "Base",
		Provider: profile.ProviderClaude,
		Model:    "opus",
		Body:     "base persona",
		Spec: profile.Spec{
			"settings": json.RawMessage(`{"env":{"CAIRN":"whateverTheOperatorWrote"}}`),
			// An unknown key: carried through the cascade and ignored by every
			// renderer, never an error.
			"somethingCairnHasNeverHeardOf": json.RawMessage(`{"nested":[1,2,3]}`),
		},
	}
	engineer := profile.Profile{
		ID:      "engineer",
		Extends: "base",
		Name:    "Engineer",
		Body:    "engineer persona",
		Spec: profile.Spec{
			"slots":      json.RawMessage(`[{"name":"note","source":{"kind":"inline","inline":{"content":"the standing note"}}}]`),
			"mcp":        json.RawMessage(`[{"name":"vanta","command":"vanta-mcp","args":["serve"]}]`),
			"skills":     json.RawMessage(`["code-review"]`),
			"skills_dir": json.RawMessage(`"` + skillsDir + `"`),
			"files":      json.RawMessage(`{"notes/scratch.md":"scratch\n"}`),
		},
	}
	for _, p := range []profile.Profile{base, engineer} {
		if err := st.PutProfile(ctx, p); err != nil {
			t.Fatalf("put profile %q: %v", p.ID, err)
		}
	}
	if err := st.PutBinding(ctx, store.Binding{Name: "engineer", ProfileID: "engineer", Scope: scopeDir}); err != nil {
		t.Fatalf("put binding: %v", err)
	}
}

// writeSkill lays down a multi-file skill: an entry file, a reference under a
// subdirectory, and an executable script. Flattening it is the defect the
// directory-tree copier exists to prevent.
func writeSkill(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "code-review")
	mustMkdir(t, filepath.Join(dir, "references"))
	writeFile(t, filepath.Join(dir, "SKILL.md"), "# code review\n", 0o644)
	writeFile(t, filepath.Join(dir, "references", "checklist.md"), "- read it\n", 0o644)
	writeFile(t, filepath.Join(dir, "run.sh"), "#!/bin/sh\necho hi\n", 0o755)
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
}

func writeFile(t *testing.T, path, content string, mode fs.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
}

func read(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}
