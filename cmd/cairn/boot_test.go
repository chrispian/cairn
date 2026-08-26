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

// TestInstall covers the second command end to end, against a fixture root.
//
// Never against a real home directory: `cairn install` rewrites the live
// ~/.claude that every agent working on cairn is running under, which is why
// --root exists and why every test here passes one.
func TestInstall(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	dbPath := filepath.Join(home, "cairn.db")
	skillsDir := filepath.Join(home, "skills")
	scopeDir := filepath.Join(home, "repo")
	mustMkdir(t, scopeDir)
	writeSkill(t, skillsDir)
	seed(t, ctx, dbPath, skillsDir, scopeDir)

	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	// "base" is abstract. install renders it and boot refuses it — that
	// divergence is the reason the cascade carries the flag rather than acting
	// on it.
	if err := run(ctx, []string{"install", "base", "--db", dbPath, "--root", root}, &stdout, &stderr); err != nil {
		t.Fatalf("install: %v\nstderr: %s", err, stderr.String())
	}

	for _, rel := range []string{
		".claude/AGENTS.md",
		".claude/CLAUDE.md",
		".claude/settings.json",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Errorf("the installed layer is missing %s: %v", rel, err)
		}
	}
	// Not the boot directory's contract: no boot file, no MCP config, and none
	// of spec.files.
	for _, rel := range []string{"boot.md", ".claude/boot.md", ".mcp.json", ".claude/.mcp.json", "notes/scratch.md"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("the installed layer carries %s, which belongs to a boot directory", rel)
		}
	}

	agents := read(t, root, ".claude/AGENTS.md")
	if !strings.HasPrefix(agents, "<!-- Generated by `cairn install` from profile \"base\". -->") {
		t.Errorf(".claude/AGENTS.md does not open with the provenance line:\n%s", agents)
	}
	if got := read(t, root, ".claude/CLAUDE.md"); got != "@AGENTS.md\n" {
		t.Errorf(".claude/CLAUDE.md = %q, want the pointer with no marker", got)
	}

	t.Run("a clean check reports no drift", func(t *testing.T) {
		var out, errOut bytes.Buffer
		if err := run(ctx, []string{"install", "base", "--db", dbPath, "--root", root, "--check"}, &out, &errOut); err != nil {
			t.Fatalf("install --check on a freshly installed root = %v\n%s", err, out.String())
		}
	})

	t.Run("an edited file is drift, and check writes nothing", func(t *testing.T) {
		edited := filepath.Join(root, ".claude", "AGENTS.md")
		before := read(t, root, ".claude/AGENTS.md")
		writeFile(t, edited, before+"\nthe operator edited this in place\n", 0o644)

		var out, errOut bytes.Buffer
		err := run(ctx, []string{"install", "base", "--db", dbPath, "--root", root, "--check"}, &out, &errOut)
		var code exitCode
		if !errors.As(err, &code) || int(code) == 0 {
			t.Fatalf("install --check over an edited file = %v, want a non-zero exit status", err)
		}
		if !strings.Contains(out.String(), ".claude/AGENTS.md") {
			t.Errorf("the report does not name the edited file:\n%s", out.String())
		}
		// --check reports and repairs nothing.
		if got := read(t, root, ".claude/AGENTS.md"); !strings.Contains(got, "the operator edited this in place") {
			t.Error("--check rewrote the file it was reporting on")
		}
	})

	t.Run("the harness's own files are not drift", func(t *testing.T) {
		// A fresh root, so the edit from the previous subtest is not in play.
		clean := t.TempDir()
		var out, errOut bytes.Buffer
		if err := run(ctx, []string{"install", "base", "--db", dbPath, "--root", clean}, &out, &errOut); err != nil {
			t.Fatalf("install: %v", err)
		}
		for _, rel := range []string{".claude/settings.local.json", ".claude/.credentials.json"} {
			writeFile(t, filepath.Join(clean, filepath.FromSlash(rel)), "{}\n", 0o644)
		}
		out.Reset()
		if err := run(ctx, []string{"install", "base", "--db", dbPath, "--root", clean, "--check"}, &out, &errOut); err != nil {
			t.Fatalf("the harness's own state files were reported as drift: %v\n%s", err, out.String())
		}
	})

	t.Run("install takes exactly one target", func(t *testing.T) {
		var out, errOut bytes.Buffer
		if err := run(ctx, []string{"install", "--root", root, "--db", dbPath}, &out, &errOut); err == nil {
			t.Error("install with no target reported success")
		}
		if err := run(ctx, []string{"install", "base", "engineer", "--db", dbPath, "--root", root}, &out, &errOut); err == nil {
			t.Error("install with two targets reported success")
		}
	})
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
