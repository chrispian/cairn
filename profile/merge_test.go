package profile

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

// pythonSpelling is a manifest value written the way agent-setup's bin/seed.py
// wrote one before the catalog replaced it: json.dumps with sort_keys and its
// default separators, which are ", " and ": ". Go's encoder emits neither
// space.
//
// The seeder is gone and the fixture is not, because what it is for is a
// spelling no Go encoder produces. Any hand-written or non-Go-written document
// serves; this one is kept because it is the spelling this rule was written
// against.
//
// It is the fixture for the one rule this whole file exists to protect. What
// is stored under spec.settings is written into the harness's settings
// document, laid out and otherwise untouched, so an implementation that
// decoded and re-encoded a key only one profile declared would reach the
// operator's file. This fixture catches it by key order — "b" before "a",
// which Go's encoder sorts and laying a document out does not.
const pythonSpelling = `{"b": 1, "a": {"d": 2, "c": 3}}`

// TestResolveSettingsFromOneProfileAreByteIdentical pins that rule: when
// exactly one profile in the chain declares a raw-carried key, the resolved
// value is the stored bytes and not a re-serialization of them.
//
// It falls out of the mechanism — a merge needs two declared values, so one
// declarer never reaches a merger — and it is pinned anyway, because "falls
// out for free" is exactly what stops quietly being true.
func TestResolveSettingsFromOneProfileAreByteIdentical(t *testing.T) {
	t.Parallel()

	stored := json.RawMessage(pythonSpelling)
	l := fakeLoader{
		"root": {ID: "root", Spec: Spec{SpecKeySettings: stored}},
		"leaf": {ID: "leaf", Extends: "root", Spec: spec(t, map[string]string{
			SpecKeySkills: `["review"]`,
		})},
	}

	got := resolveOK(t, l, "leaf")

	if !bytes.Equal(got.Spec[SpecKeySettings], stored) {
		t.Errorf("Spec[settings] = %s, want the stored bytes %s — a key one profile declares is never re-serialized",
			got.Spec[SpecKeySettings], stored)
	}
	raw, declared := got.Spec.Settings()
	if !declared {
		t.Fatal("Settings() reported the key undeclared")
	}
	if !bytes.Equal(raw, stored) {
		t.Errorf("Settings() = %s, want the stored bytes %s", raw, stored)
	}
}

// TestResolveKeyedCollectionFromOneProfileIsCarriedVerbatim is the same rule
// for a collection whose stored shape is an ordered list. A profile that is
// the only one to declare its slots keeps their spelling and their order, so
// the merge changes nothing for a chain that never composes.
func TestResolveKeyedCollectionFromOneProfileIsCarriedVerbatim(t *testing.T) {
	t.Parallel()

	const stored = `[ {"name": "zebra", "source": {"kind": "inline"}},
  {"name": "alpha", "source": {"kind": "inline"}} ]`

	l := fakeLoader{
		"root": {ID: "root", Spec: spec(t, map[string]string{SpecKeySkills: `["review"]`})},
		"leaf": {ID: "leaf", Extends: "root", Spec: Spec{SpecKeySlots: json.RawMessage(stored)}},
	}

	got := resolveOK(t, l, "leaf")

	if !bytes.Equal(got.Spec[SpecKeySlots], json.RawMessage(stored)) {
		t.Errorf("Spec[slots] = %s, want the stored bytes %s", got.Spec[SpecKeySlots], stored)
	}
}

// TestResolveSettingsRedeclaredAfterAMidChainClearAreByteIdentical pins the
// half of that rule a two-profile chain never reaches. The fold carries an
// explicit null forward as the folded value, so a leaf that redeclares a key a
// middle profile cleared arrives at a merger with two "declared" values — and
// must still be carried verbatim, because there is nothing left to fold onto.
//
// It is guarded rather than incidental, and the guard is easy to lose: JSON
// null decodes into a map as an empty map rather than an error, so without it
// this chain composes the leaf's document against nothing, re-encodes it, and
// rewrites every planted .claude/settings.json on whitespace alone — the exact
// failure byte-identity exists to prevent, reached by the one path where the
// operator did nothing wrong.
func TestResolveSettingsRedeclaredAfterAMidChainClearAreByteIdentical(t *testing.T) {
	t.Parallel()

	stored := json.RawMessage(pythonSpelling)
	l := fakeLoader{
		"root": {ID: "root", Spec: spec(t, map[string]string{
			SpecKeySettings: `{"effortLevel":"xhigh"}`,
		})},
		"mid": {ID: "mid", Extends: "root", Spec: spec(t, map[string]string{
			SpecKeySettings: `null`,
		})},
		"leaf": {ID: "leaf", Extends: "mid", Spec: Spec{SpecKeySettings: stored}},
	}

	got := resolveOK(t, l, "leaf")

	if !bytes.Equal(got.Spec[SpecKeySettings], stored) {
		t.Errorf("Spec[settings] = %s, want the stored bytes %s — a redeclaration after a clear has nothing to fold onto and is never re-serialized",
			got.Spec[SpecKeySettings], stored)
	}
	// The clear did clear: the root's key is gone rather than merged back in
	// underneath the leaf's document.
	var merged map[string]any
	if err := json.Unmarshal(got.Spec[SpecKeySettings], &merged); err != nil {
		t.Fatalf("the resolved settings do not decode: %v (%s)", err, got.Spec[SpecKeySettings])
	}
	if _, present := merged["effortLevel"]; present {
		t.Errorf("settings = %v, want the root's key cleared by the middle profile", merged)
	}
}

// TestResolveSettingsMergeAtEveryDepth is the failure this change exists to
// remove: a descendant setting one key under "permissions" must not drop its
// siblings.
func TestResolveSettingsMergeAtEveryDepth(t *testing.T) {
	t.Parallel()

	l := fakeLoader{
		"root": {ID: "root", Spec: spec(t, map[string]string{SpecKeySettings: `{
			"effortLevel": "xhigh",
			"permissions": {"defaultMode": "auto", "additionalDirectories": ["/srv"]},
			"env": {"A": "1", "B": "2"},
			"hooks": {"SessionStart": [{"command": "root.sh"}]}
		}`})},
		"leaf": {ID: "leaf", Extends: "root", Spec: spec(t, map[string]string{SpecKeySettings: `{
			"permissions": {"defaultMode": "plan"},
			"env": {"B": "9"},
			"hooks": {"SessionStart": [{"command": "leaf.sh"}]}
		}`})},
	}

	got := resolveOK(t, l, "leaf")

	var merged map[string]any
	if err := json.Unmarshal(got.Spec[SpecKeySettings], &merged); err != nil {
		t.Fatalf("the merged settings do not decode: %v (%s)", err, got.Spec[SpecKeySettings])
	}

	// The root's untouched top-level key stands.
	if merged["effortLevel"] != "xhigh" {
		t.Errorf("effortLevel = %v, want the root's", merged["effortLevel"])
	}
	// One key under permissions moved and its sibling stayed. This is the
	// whole point.
	perms, _ := merged["permissions"].(map[string]any)
	if perms["defaultMode"] != "plan" {
		t.Errorf("permissions.defaultMode = %v, want the leaf's", perms["defaultMode"])
	}
	if _, ok := perms["additionalDirectories"]; !ok {
		t.Error("permissions.additionalDirectories was dropped; a sibling the leaf never mentioned must stand")
	}
	// The same one level down and in a second object, so it is not one lucky
	// nesting.
	env, _ := merged["env"].(map[string]any)
	if env["A"] != "1" || env["B"] != "9" {
		t.Errorf("env = %v, want {A:1 B:9}", env)
	}
	// An array inside the document is not keyed and replaces whole.
	hooks, _ := merged["hooks"].(map[string]any)
	starts, _ := hooks["SessionStart"].([]any)
	if len(starts) != 1 {
		t.Fatalf("hooks.SessionStart = %v, want the leaf's one entry — an unkeyed list must not union", starts)
	}
	if entry, _ := starts[0].(map[string]any); entry["command"] != "leaf.sh" {
		t.Errorf("hooks.SessionStart[0] = %v, want the leaf's", starts[0])
	}
}

// TestResolveSlotsMergeByName covers the collection the ADR was written for: a
// role profile declares the slot it changes and inherits the rest.
func TestResolveSlotsMergeByName(t *testing.T) {
	t.Parallel()

	l := fakeLoader{
		"base": {ID: "base", Spec: spec(t, map[string]string{SpecKeySlots: `[
			{"name":"standing","source":{"kind":"static_file","static_file":{"path":"/standing.md"}}},
			{"name":"context","source":{"kind":"static_file","static_file":{"path":"/context.md"}}},
			{"name":"role","source":{"kind":"static_file","static_file":{"path":"/roles/base.md"}}}
		]`})},
		"role": {ID: "role", Extends: "base", Spec: spec(t, map[string]string{SpecKeySlots: `[
			{"name":"role","section":"## Role","source":{"kind":"static_file","static_file":{"path":"/roles/architect.md"}}},
			{"name":"repo","section":"## Repository","source":{"kind":"cmd","cmd":{"run":"git status"}}}
		]`})},
	}

	got := resolveOK(t, l, "role")

	slots, err := got.Spec.Slots()
	if err != nil {
		t.Fatalf("Slots() = error %v", err)
	}
	names := make([]string, 0, len(slots))
	for _, s := range slots {
		names = append(names, s.Name)
	}
	// Ordered by key. Nothing reads the order — a template addresses a slot by
	// name — and sorting is what makes two renders identical.
	if want := []string{"context", "repo", "role", "standing"}; !slices.Equal(names, want) {
		t.Fatalf("merged slot names = %v, want %v", names, want)
	}
	for _, s := range slots {
		if s.Name != "role" {
			continue
		}
		// The descendant's member replaced the ancestor's whole, rather than
		// merging field by field: a member is not itself a keyed collection.
		if got := s.Source.StaticFile.Path; got != "/roles/architect.md" {
			t.Errorf("the role slot reads %q, want the descendant's", got)
		}
		if s.Section != "## Role" {
			t.Errorf("the role slot's section = %q, want the descendant's", s.Section)
		}
	}
}

// TestResolveMCPMergesByName covers the second list of objects. spec.mcp is a
// list of agentlaunch.MCPServerSpec, each carrying a "name" field — not an
// object keyed by server name — which makes it structurally identical to
// spec.slots and merged by the same mechanism.
func TestResolveMCPMergesByName(t *testing.T) {
	t.Parallel()

	l := fakeLoader{
		"root": {ID: "root", Spec: spec(t, map[string]string{SpecKeyMCP: `[
			{"name":"fs","command":"mcp-fs","args":["--root","/root"]},
			{"name":"mux","command":"mux"}
		]`})},
		"leaf": {ID: "leaf", Extends: "root", Spec: spec(t, map[string]string{SpecKeyMCP: `[
			{"name":"fs","command":"mcp-fs","args":["--root","/leaf"]}
		]`})},
	}

	got := resolveOK(t, l, "leaf")

	servers, err := got.Spec.MCP()
	if err != nil {
		t.Fatalf("MCP() = error %v", err)
	}
	if len(servers) != 2 {
		t.Fatalf("MCP() returned %d servers, want 2: %+v", len(servers), servers)
	}
	if servers[0].Name != "fs" || servers[1].Name != "mux" {
		t.Fatalf("merged servers = %q, %q; want fs, mux in key order", servers[0].Name, servers[1].Name)
	}
	if !slices.Equal(servers[0].Args, []string{"--root", "/leaf"}) {
		t.Errorf("fs args = %v, want the leaf's", servers[0].Args)
	}
	if servers[1].Command != "mux" {
		t.Errorf("mux command = %q, want the root's untouched", servers[1].Command)
	}
}

// TestResolveMapCollectionsMergeByPath covers the three path-keyed maps
// together, because one mechanism composes all three.
func TestResolveMapCollectionsMergeByPath(t *testing.T) {
	t.Parallel()

	l := fakeLoader{
		"root": {ID: "root", Spec: spec(t, map[string]string{
			SpecKeyTemplates: `{"AGENTS.md":"root agents","CLAUDE.md":"root claude"}`,
			SpecKeyFiles:     `{"notes.md":"root notes","keep.md":"kept"}`,
			SpecKeyTrees:     `{"docs":"/root/docs","shared":"/shared"}`,
		})},
		"leaf": {ID: "leaf", Extends: "root", Spec: spec(t, map[string]string{
			SpecKeyTemplates: `{"CLAUDE.md":"leaf claude","boot.md":"leaf boot"}`,
			SpecKeyFiles:     `{"notes.md":"leaf notes"}`,
			SpecKeyTrees:     `{"docs":"/leaf/docs"}`,
		})},
	}

	got := resolveOK(t, l, "leaf")

	templates, err := got.Spec.Templates()
	if err != nil {
		t.Fatalf("Templates() = error %v", err)
	}
	for path, want := range map[string]string{
		"AGENTS.md": "root agents", "CLAUDE.md": "leaf claude", "boot.md": "leaf boot",
	} {
		if got := templates[path].Literal; got != want {
			t.Errorf("templates[%q] = %q, want %q", path, got, want)
		}
	}
	if len(templates) != 3 {
		t.Errorf("Templates() has %d entries, want 3: %v", len(templates), templates)
	}

	files, err := got.Spec.Files()
	if err != nil {
		t.Fatalf("Files() = error %v", err)
	}
	if files["notes.md"].Literal != "leaf notes" || files["keep.md"].Literal != "kept" {
		t.Errorf("Files() = %v, want the leaf's notes.md and the root's keep.md", files)
	}

	trees, err := got.Spec.Trees()
	if err != nil {
		t.Fatalf("Trees() = error %v", err)
	}
	if trees["docs"] != "/leaf/docs" || trees["shared"] != "/shared" {
		t.Errorf("Trees() = %v, want the leaf's docs and the root's shared", trees)
	}
}

// TestResolveListsOfIDsUnionByID covers the collections whose member is its own
// key.
func TestResolveListsOfIDsUnionByID(t *testing.T) {
	t.Parallel()

	l := fakeLoader{
		"root": {ID: "root", Spec: spec(t, map[string]string{
			SpecKeySkills:    `["commit","adr"]`,
			SpecKeySubagents: `["reviewer"]`,
		})},
		"leaf": {ID: "leaf", Extends: "root", Spec: spec(t, map[string]string{
			SpecKeySkills:    `["adr","qstatus"]`,
			SpecKeySubagents: `["worker","reviewer"]`,
		})},
	}

	got := resolveOK(t, l, "leaf")

	skills, err := got.Spec.Skills()
	if err != nil {
		t.Fatalf("Skills() = error %v", err)
	}
	// A repeated id is one member, and the result is ordered by key.
	if want := []string{"adr", "commit", "qstatus"}; !slices.Equal(skills, want) {
		t.Errorf("Skills() = %v, want %v", skills, want)
	}
	named, err := got.Spec.Subagents()
	if err != nil {
		t.Fatalf("Subagents() = error %v", err)
	}
	if want := []string{"reviewer", "worker"}; !slices.Equal(named, want) {
		t.Errorf("Subagents() = %v, want %v", named, want)
	}
}

// TestResolveInstallSkillsMergeAndSiblingsReplace covers the one nested keyed
// collection. install.skills composes by id; any other member of the install
// namespace is not in the table and replaces whole, so a future install-only
// key behaves predictably without a decision here.
func TestResolveInstallSkillsMergeAndSiblingsReplace(t *testing.T) {
	t.Parallel()

	l := fakeLoader{
		"root": {ID: "root", Spec: spec(t, map[string]string{
			SpecKeyInstall: `{"skills":["commit","push"],"elsewhere":{"kept":1,"moved":2}}`,
		})},
		"leaf": {ID: "leaf", Extends: "root", Spec: spec(t, map[string]string{
			SpecKeyInstall: `{"skills":["adr"],"elsewhere":{"moved":9}}`,
		})},
	}

	got := resolveOK(t, l, "leaf")

	installed, err := got.Spec.InstallSkills()
	if err != nil {
		t.Fatalf("InstallSkills() = error %v", err)
	}
	if want := []string{"adr", "commit", "push"}; !slices.Equal(installed, want) {
		t.Errorf("InstallSkills() = %v, want %v", installed, want)
	}
	var namespace struct {
		Elsewhere map[string]int `json:"elsewhere"`
	}
	if err := json.Unmarshal(got.Spec[SpecKeyInstall], &namespace); err != nil {
		t.Fatalf("the merged install key does not decode: %v", err)
	}
	if _, kept := namespace.Elsewhere["kept"]; kept {
		t.Errorf("install.elsewhere = %v, want the leaf's whole — a member with no rule replaces", namespace.Elsewhere)
	}
	if namespace.Elsewhere["moved"] != 9 {
		t.Errorf("install.elsewhere.moved = %d, want the leaf's 9", namespace.Elsewhere["moved"])
	}
}

// TestResolveAccessDirectoriesUnion covers the access namespace, which
// composes the way the install one does and for a reason of its own: a leaf
// extending a profile to reach one more directory must keep the ones the
// ancestor reached, or extending would silently take access away.
func TestResolveAccessDirectoriesUnion(t *testing.T) {
	t.Parallel()

	l := fakeLoader{
		"root": {ID: "root", Spec: spec(t, map[string]string{
			SpecKeyAccess: `{"directories":["~/dev/one","$WORK/two"],"elsewhere":{"kept":1}}`,
		})},
		"leaf": {ID: "leaf", Extends: "root", Spec: spec(t, map[string]string{
			SpecKeyAccess: `{"directories":["~/dev/one","/srv/three"],"elsewhere":{"replaced":1}}`,
		})},
	}

	got := resolveOK(t, l, "leaf")

	dirs, err := got.Spec.AccessDirectories()
	if err != nil {
		t.Fatalf("AccessDirectories() = error %v", err)
	}
	// Sorted by path, which is what keying a list by its member means here.
	// The duplicate is one member: the two profiles named one directory.
	if want := []string{"$WORK/two", "/srv/three", "~/dev/one"}; !slices.Equal(dirs, want) {
		t.Errorf("AccessDirectories() = %v, want %v", dirs, want)
	}
	var namespace struct {
		Elsewhere map[string]int `json:"elsewhere"`
	}
	if err := json.Unmarshal(got.Spec[SpecKeyAccess], &namespace); err != nil {
		t.Fatalf("the merged access key does not decode: %v", err)
	}
	if _, kept := namespace.Elsewhere["kept"]; kept {
		t.Errorf("access.elsewhere = %v, want the leaf's whole — a member with no rule replaces", namespace.Elsewhere)
	}
}

// TestMergeSettingsLeavesAnEmptyOverlayAlone is the one place a null does not
// clear, and the reason it does not is that the overlay is a renderer's
// contribution rather than a profile's declaration. A renderer with nothing to
// add must hand back the operator's document untouched — bytes included, since
// what comes out of here is written into a file they read — where a profile
// declaring null means "clear what my ancestor said".
func TestMergeSettingsLeavesAnEmptyOverlayAlone(t *testing.T) {
	t.Parallel()

	composed := json.RawMessage(pythonSpelling)
	for _, overlay := range []json.RawMessage{nil, json.RawMessage("null"), json.RawMessage("  ")} {
		got, err := MergeSettings(composed, overlay)
		if err != nil {
			t.Fatalf("MergeSettings(_, %q) = error %v", overlay, err)
		}
		if !bytes.Equal(got, composed) {
			t.Errorf("MergeSettings(_, %q) = %s, want the stored bytes %s", overlay, got, composed)
		}
	}
}

// TestMergeSettingsComposesRatherThanReplaces pins what an overlay may and may
// not do to the document it lands on. It adds its own key at whatever depth it
// declares one, and it leaves every sibling of that key standing — which is
// what stops a renderer contributing one permission from writing over the mode
// the operator declared beside it.
func TestMergeSettingsComposesRatherThanReplaces(t *testing.T) {
	t.Parallel()

	composed := json.RawMessage(`{"apiKeyHelper":"/bin/helper","permissions":{"defaultMode":"auto"}}`)
	overlay := json.RawMessage(`{"permissions":{"additionalDirectories":["/srv/work"]}}`)

	got, err := MergeSettings(composed, overlay)
	if err != nil {
		t.Fatalf("MergeSettings() = error %v", err)
	}
	var document struct {
		APIKeyHelper string `json:"apiKeyHelper"`
		Permissions  struct {
			DefaultMode           string   `json:"defaultMode"`
			AdditionalDirectories []string `json:"additionalDirectories"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(got, &document); err != nil {
		t.Fatalf("the merged document does not decode: %v", err)
	}
	if document.APIKeyHelper != "/bin/helper" {
		t.Errorf("apiKeyHelper = %q, want the operator's %q", document.APIKeyHelper, "/bin/helper")
	}
	if document.Permissions.DefaultMode != "auto" {
		t.Errorf("permissions.defaultMode = %q, want the operator's %q — an overlay keeps its key's siblings",
			document.Permissions.DefaultMode, "auto")
	}
	if want := []string{"/srv/work"}; !slices.Equal(document.Permissions.AdditionalDirectories, want) {
		t.Errorf("permissions.additionalDirectories = %v, want %v", document.Permissions.AdditionalDirectories, want)
	}
}

// TestMergeSettingsReplacesAtTheSameKey is the other half of the rule, and it
// is pinned because it is the reason a caller cannot simply merge and hope.
//
// Composing siblings is what the deep merge does; at one key it replaces, and
// an array is not a keyed collection so two lists do not union. So an overlay
// carrying a key the document already declares silently discards the
// operator's value — which is why bootdir.RenderSettings refuses that case
// before it gets here rather than relying on this function to preserve
// anything. If this ever started unioning, that guard would be the thing to
// revisit, and this test is what would say so.
func TestMergeSettingsReplacesAtTheSameKey(t *testing.T) {
	t.Parallel()

	composed := json.RawMessage(`{"permissions":{"additionalDirectories":["/operator/declared"]}}`)
	overlay := json.RawMessage(`{"permissions":{"additionalDirectories":["/granted"]}}`)

	got, err := MergeSettings(composed, overlay)
	if err != nil {
		t.Fatalf("MergeSettings() = error %v", err)
	}
	var document struct {
		Permissions struct {
			AdditionalDirectories []string `json:"additionalDirectories"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(got, &document); err != nil {
		t.Fatalf("the merged document does not decode: %v", err)
	}
	if want := []string{"/granted"}; !slices.Equal(document.Permissions.AdditionalDirectories, want) {
		t.Errorf("permissions.additionalDirectories = %v, want the overlay's %v — a merge at one key replaces, and the caller is what stops it mattering",
			document.Permissions.AdditionalDirectories, want)
	}
}

// TestResolveNullAtAMemberRemovesIt covers member-level clearing, which has a
// natural spelling for the map-shaped collections and none for the list-shaped
// ones — see the package's own note and docs/plan.md §3.
func TestResolveNullAtAMemberRemovesIt(t *testing.T) {
	t.Parallel()

	l := fakeLoader{
		"root": {ID: "root", Spec: spec(t, map[string]string{
			SpecKeyTemplates: `{"AGENTS.md":"kept","CLAUDE.md":"dropped"}`,
			SpecKeyFiles:     `{"a.md":"kept","b.md":"dropped"}`,
			SpecKeyTrees:     `{"docs":"/docs","gone":"/gone"}`,
			SpecKeySettings:  `{"kept":1,"dropped":2,"nested":{"kept":3,"dropped":4}}`,
		})},
		"leaf": {ID: "leaf", Extends: "root", Spec: spec(t, map[string]string{
			SpecKeyTemplates: `{"CLAUDE.md":null}`,
			SpecKeyFiles:     `{"b.md":null}`,
			SpecKeyTrees:     `{"gone":null}`,
			SpecKeySettings:  `{"dropped":null,"nested":{"dropped":null}}`,
		})},
	}

	got := resolveOK(t, l, "leaf")

	templates, err := got.Spec.Templates()
	if err != nil {
		t.Fatalf("Templates() = error %v", err)
	}
	if len(templates) != 1 || templates["AGENTS.md"].Literal != "kept" {
		t.Errorf("Templates() = %v, want only AGENTS.md", templates)
	}
	files, err := got.Spec.Files()
	if err != nil {
		t.Fatalf("Files() = error %v", err)
	}
	if len(files) != 1 || files["a.md"].Literal != "kept" {
		t.Errorf("Files() = %v, want only a.md", files)
	}
	trees, err := got.Spec.Trees()
	if err != nil {
		t.Fatalf("Trees() = error %v", err)
	}
	if len(trees) != 1 || trees["docs"] != "/docs" {
		t.Errorf("Trees() = %v, want only docs", trees)
	}
	var settings map[string]any
	if err := json.Unmarshal(got.Spec[SpecKeySettings], &settings); err != nil {
		t.Fatalf("the merged settings do not decode: %v", err)
	}
	if _, present := settings["dropped"]; present {
		t.Errorf("settings = %v, want the nulled member gone", settings)
	}
	nested, _ := settings["nested"].(map[string]any)
	if _, present := nested["dropped"]; present {
		t.Errorf("settings.nested = %v, want the nulled member gone", nested)
	}
	if nested["kept"] != float64(3) {
		t.Errorf("settings.nested.kept = %v, want 3 — clearing a sibling must not clear it", nested["kept"])
	}
}

// TestResolveNullAtTheCollectionClearsIt covers clearing a whole key, which is
// the only clearing a list-shaped collection has.
func TestResolveNullAtTheCollectionClearsIt(t *testing.T) {
	t.Parallel()

	l := fakeLoader{
		"root": {ID: "root", Spec: spec(t, map[string]string{
			SpecKeySkills:    `["commit","adr"]`,
			SpecKeySettings:  `{"effortLevel":"xhigh"}`,
			SpecKeyTemplates: `{"AGENTS.md":"root"}`,
		})},
		"leaf": {ID: "leaf", Extends: "root", Spec: spec(t, map[string]string{
			SpecKeySkills:    `null`,
			SpecKeySettings:  `null`,
			SpecKeyTemplates: `null`,
		})},
	}

	got := resolveOK(t, l, "leaf")

	skills, err := got.Spec.Skills()
	if err != nil || skills != nil {
		t.Errorf("Skills() = %v, %v; want nil, nil", skills, err)
	}
	if raw, declared := got.Spec.Settings(); declared {
		t.Errorf("Settings() = %s, declared; want the key cleared", raw)
	}
	templates, err := got.Spec.Templates()
	if err != nil || templates != nil {
		t.Errorf("Templates() = %v, %v; want nil, nil", templates, err)
	}
}

// TestResolveEmptyCollectionAddsNothing covers the behaviour change that comes
// with the merge: an empty list or object used to replace and therefore clear,
// and now says "I add nothing". Clearing is null and only null.
func TestResolveEmptyCollectionAddsNothing(t *testing.T) {
	t.Parallel()

	l := fakeLoader{
		"root": {ID: "root", Spec: spec(t, map[string]string{
			SpecKeySkills:    `["commit"]`,
			SpecKeyTemplates: `{"AGENTS.md":"root"}`,
			SpecKeySettings:  `{"effortLevel":"xhigh"}`,
		})},
		"leaf": {ID: "leaf", Extends: "root", Spec: spec(t, map[string]string{
			SpecKeySkills:    `[]`,
			SpecKeyTemplates: `{}`,
			SpecKeySettings:  `{}`,
		})},
	}

	got := resolveOK(t, l, "leaf")

	skills, err := got.Spec.Skills()
	if err != nil {
		t.Fatalf("Skills() = error %v", err)
	}
	if !slices.Equal(skills, []string{"commit"}) {
		t.Errorf("Skills() = %v, want the root's — an empty list adds nothing rather than clearing", skills)
	}
	templates, err := got.Spec.Templates()
	if err != nil {
		t.Fatalf("Templates() = error %v", err)
	}
	if len(templates) != 1 {
		t.Errorf("Templates() = %v, want the root's", templates)
	}
	var settings map[string]any
	if err := json.Unmarshal(got.Spec[SpecKeySettings], &settings); err != nil {
		t.Fatalf("the merged settings do not decode: %v", err)
	}
	if settings["effortLevel"] != "xhigh" {
		t.Errorf("settings = %v, want the root's", settings)
	}
}

// TestResolveComposesAcrossAThreeProfileChain proves the fold is not a
// two-profile special case: a middle profile's member survives a leaf that
// never mentions it, and the leaf still wins where all three collide.
func TestResolveComposesAcrossAThreeProfileChain(t *testing.T) {
	t.Parallel()

	l := fakeLoader{
		"root": {ID: "root", Spec: spec(t, map[string]string{
			SpecKeySlots:  `[{"name":"standing","source":{"kind":"inline","inline":{"content":"root"}}}]`,
			SpecKeySkills: `["root-skill"]`,
		})},
		"mid": {ID: "mid", Extends: "root", Spec: spec(t, map[string]string{
			SpecKeySlots:  `[{"name":"context","source":{"kind":"inline","inline":{"content":"mid"}}}]`,
			SpecKeySkills: `["mid-skill"]`,
		})},
		"leaf": {ID: "leaf", Extends: "mid", Spec: spec(t, map[string]string{
			SpecKeySlots:  `[{"name":"standing","source":{"kind":"inline","inline":{"content":"leaf"}}}]`,
			SpecKeySkills: `["leaf-skill"]`,
		})},
	}

	got := resolveOK(t, l, "leaf")

	slots, err := got.Spec.Slots()
	if err != nil {
		t.Fatalf("Slots() = error %v", err)
	}
	if len(slots) != 2 {
		t.Fatalf("Slots() returned %d slots, want 2: %+v", len(slots), slots)
	}
	if slots[0].Name != "context" || slots[0].Source.Inline.Content != "mid" {
		t.Errorf("slot 0 = %+v, want the mid profile's context slot", slots[0])
	}
	if slots[1].Name != "standing" || slots[1].Source.Inline.Content != "leaf" {
		t.Errorf("slot 1 = %+v, want the leaf's standing slot", slots[1])
	}
	skills, err := got.Spec.Skills()
	if err != nil {
		t.Fatalf("Skills() = error %v", err)
	}
	if want := []string{"leaf-skill", "mid-skill", "root-skill"}; !slices.Equal(skills, want) {
		t.Errorf("Skills() = %v, want %v", skills, want)
	}
}

// TestResolveMergeIsDeterministic pins that a composed key is the same bytes
// twice, whatever order the loader's maps are walked in.
func TestResolveMergeIsDeterministic(t *testing.T) {
	t.Parallel()

	newLoader := func() fakeLoader {
		return fakeLoader{
			"root": {ID: "root", Spec: spec(t, map[string]string{
				SpecKeySlots:     `[{"name":"zebra","source":{"kind":"inline"}},{"name":"alpha","source":{"kind":"inline"}}]`,
				SpecKeySkills:    `["zulu","alpha"]`,
				SpecKeyTemplates: `{"z.md":"z","a.md":"a"}`,
				SpecKeySettings:  `{"z":1,"a":{"z":2,"a":3}}`,
			})},
			"leaf": {ID: "leaf", Extends: "root", Spec: spec(t, map[string]string{
				SpecKeySlots:     `[{"name":"mike","source":{"kind":"inline"}}]`,
				SpecKeySkills:    `["mike"]`,
				SpecKeyTemplates: `{"m.md":"m"}`,
				SpecKeySettings:  `{"m":4}`,
			})},
		}
	}

	first := resolveOK(t, newLoader(), "leaf")
	for range 8 {
		next := resolveOK(t, newLoader(), "leaf")
		for _, key := range specKeys(first.Spec) {
			if !bytes.Equal(first.Spec[key], next.Spec[key]) {
				t.Fatalf("spec.%s resolved to %s and then to %s", key, first.Spec[key], next.Spec[key])
			}
		}
	}

	// Ordered by key, in both list shapes and in the object.
	if s := string(first.Spec[SpecKeySkills]); s != `["alpha","mike","zulu"]` {
		t.Errorf("Spec[skills] = %s, want the ids ordered by key", s)
	}
	slots, err := first.Spec.Slots()
	if err != nil {
		t.Fatalf("Slots() = error %v", err)
	}
	names := make([]string, 0, len(slots))
	for _, s := range slots {
		names = append(names, s.Name)
	}
	if want := []string{"alpha", "mike", "zebra"}; !slices.Equal(names, want) {
		t.Errorf("merged slot names = %v, want %v", names, want)
	}
}

// TestResolveMergeRefusesAShapeItCannotCompose covers the failures a merge can
// have that a replace could not, and pins that each one names the key and the
// profile that brought the second declaration.
//
// The refusal is reachable only when two profiles declare the key. A shape one
// profile got wrong still cascades untouched and is reported by the accessor
// that reads it, which knows what the key was for.
func TestResolveMergeRefusesAShapeItCannotCompose(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		key        string
		root, leaf string
		wantIn     string
	}{
		{
			name: "a keyed list declared as an object",
			key:  SpecKeySlots,
			root: `[{"name":"role","source":{"kind":"inline"}}]`,
			leaf: `{"name":"role"}`,
			// The library's own decode would say the same thing later; saying
			// it here names both profiles.
			wantIn: "not a list",
		},
		{
			name:   "a keyed object declared as a list",
			key:    SpecKeyTemplates,
			root:   `{"AGENTS.md":"root"}`,
			leaf:   `["AGENTS.md"]`,
			wantIn: "not an object",
		},
		{
			name:   "a list member carrying no name to compose by",
			key:    SpecKeySlots,
			root:   `[{"name":"role","source":{"kind":"inline"}}]`,
			leaf:   `[{"source":{"kind":"inline"}}]`,
			wantIn: `declares no "name"`,
		},
		{
			name:   "a list member that is not an object at all",
			key:    SpecKeyMCP,
			root:   `[{"name":"fs","command":"mcp-fs"}]`,
			leaf:   `["fs"]`,
			wantIn: "is not an object",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			l := fakeLoader{
				"root": {ID: "root", Spec: spec(t, map[string]string{tc.key: tc.root})},
				"leaf": {ID: "leaf", Extends: "root", Spec: spec(t, map[string]string{tc.key: tc.leaf})},
			}

			got, err := Resolve(t.Context(), l, "leaf")

			if got != nil {
				t.Errorf("Resolve returned %+v, want nil alongside the error", got)
			}
			if !errors.Is(err, ErrSpecMerge) {
				t.Fatalf("Resolve error = %v, want one wrapping ErrSpecMerge", err)
			}
			for _, want := range []string{"spec." + tc.key, `"leaf"`, tc.wantIn} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Resolve error = %q, want it to mention %q", err, want)
				}
			}
		})
	}
}

// TestResolveMergeLeavesASingleProfilesShapeToItsAccessor is the other side of
// that line: one profile declaring a malformed value is not the cascade's
// business, so it resolves and fails where it is read.
func TestResolveMergeLeavesASingleProfilesShapeToItsAccessor(t *testing.T) {
	t.Parallel()

	l := fakeLoader{
		"root": {ID: "root", Spec: spec(t, map[string]string{SpecKeySlots: `{"name":"role"}`})},
		"leaf": {ID: "leaf", Extends: "root"},
	}

	got := resolveOK(t, l, "leaf")

	if _, err := got.Spec.Slots(); err == nil {
		t.Fatal("Slots() accepted an object, want the accessor's error")
	} else if !strings.Contains(err.Error(), SpecKeySlots) {
		t.Errorf("Slots() error = %q, want it to name the key", err)
	}
}

// TestResolveMergeDoesNotEscapeHTML pins that composing a key does not
// re-spell what the operator wrote. Go's default encoder rewrites <, > and &
// inside a raw message, and a settings document or a slot command that came
// back <-escaped would be a silent edit to content this package has no
// standing to touch.
func TestResolveMergeDoesNotEscapeHTML(t *testing.T) {
	t.Parallel()

	l := fakeLoader{
		"root": {ID: "root", Spec: spec(t, map[string]string{
			SpecKeySettings: `{"root":"a & b"}`,
		})},
		"leaf": {ID: "leaf", Extends: "root", Spec: spec(t, map[string]string{
			SpecKeySettings: `{"leaf":"x < y > z"}`,
		})},
	}

	got := resolveOK(t, l, "leaf")

	merged := string(got.Spec[SpecKeySettings])
	if strings.Contains(merged, `\u00`) {
		t.Errorf("Spec[settings] = %s, want the operator's characters unescaped", merged)
	}
	if !strings.Contains(merged, "a & b") || !strings.Contains(merged, "x < y > z") {
		t.Errorf("Spec[settings] = %s, want both values carried through", merged)
	}
}

// TestResolveMergeCarriesAMembersUnknownFields covers the promise one level
// down: a merge keys a member and never reads the rest of it, so a field this
// package has never heard of survives inside one.
func TestResolveMergeCarriesAMembersUnknownFields(t *testing.T) {
	t.Parallel()

	l := fakeLoader{
		"root": {ID: "root", Spec: spec(t, map[string]string{
			SpecKeySlots: `[{"name":"kept","source":{"kind":"inline"},"aFieldNothingKnows":{"deep":[1,2]}}]`,
		})},
		"leaf": {ID: "leaf", Extends: "root", Spec: spec(t, map[string]string{
			SpecKeySlots: `[{"name":"added","source":{"kind":"inline"}}]`,
		})},
	}

	got := resolveOK(t, l, "leaf")

	if s := string(got.Spec[SpecKeySlots]); !strings.Contains(s, `"aFieldNothingKnows":{"deep":[1,2]}`) {
		t.Errorf("Spec[slots] = %s, want the untouched member's unknown field carried through", s)
	}
}

// TestResolveMergeRefusesADuplicateKeyWithinOneProfile pins where duplicate
// detection lives once a collection composes. slots.Sections and renderMCP
// refuse two members of one name, but they read the merged value — and keying
// the members would compose the duplicate away before they ever saw it, so
// detection would stop exactly when the chain got long enough to need it.
//
// The refusal is per profile's value, not across the two: one profile's member
// replacing another profile's member of the same name is the mechanism working.
func TestResolveMergeRefusesADuplicateKeyWithinOneProfile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		key        string
		root, leaf string
	}{
		{
			name: "two slots of one name in the ancestor",
			key:  SpecKeySlots,
			root: `[{"name":"a","source":{"kind":"inline"}},{"name":"a","source":{"kind":"inline"}}]`,
			leaf: `[{"name":"b","source":{"kind":"inline"}}]`,
		},
		{
			name: "two slots of one name in the descendant",
			key:  SpecKeySlots,
			root: `[{"name":"b","source":{"kind":"inline"}}]`,
			leaf: `[{"name":"a","source":{"kind":"inline"}},{"name":"a","source":{"kind":"inline"}}]`,
		},
		{
			name: "two mcp servers of one name",
			key:  SpecKeyMCP,
			root: `[{"name":"a","command":"one"},{"name":"a","command":"two"}]`,
			leaf: `[{"name":"b","command":"three"}]`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			l := fakeLoader{
				"root": {ID: "root", Spec: spec(t, map[string]string{tc.key: tc.root})},
				"leaf": {ID: "leaf", Extends: "root", Spec: spec(t, map[string]string{tc.key: tc.leaf})},
			}

			_, err := Resolve(t.Context(), l, "leaf")

			if !errors.Is(err, ErrSpecMerge) {
				t.Fatalf("Resolve error = %v, want one wrapping ErrSpecMerge", err)
			}
			// The key and the duplicated name both, so the operator is sent to
			// the line they wrote rather than to the profile.
			for _, want := range []string{"spec." + tc.key, `"a"`} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Resolve error = %q, want it to mention %q", err, want)
				}
			}
		})
	}
}

// TestResolveMergeLeavesOneProfilesDuplicateToItsAccessor is the asymmetry
// that leaves behind, pinned so it is a known shape rather than a surprise. A
// key exactly one profile declares is carried unread — that is what keeps it
// byte-identical — so its duplicate survives the cascade and is refused by the
// accessor instead, with that accessor's own error.
func TestResolveMergeLeavesOneProfilesDuplicateToItsAccessor(t *testing.T) {
	t.Parallel()

	const duplicated = `[{"name":"a","source":{"kind":"inline"}},{"name":"a","source":{"kind":"inline"}}]`

	l := fakeLoader{
		"root": {ID: "root", Spec: spec(t, map[string]string{SpecKeySlots: duplicated})},
		"leaf": {ID: "leaf", Extends: "root"},
	}

	got := resolveOK(t, l, "leaf")

	// Resolve does not refuse it, and does not rewrite it either.
	if !bytes.Equal(got.Spec[SpecKeySlots], json.RawMessage(duplicated)) {
		t.Errorf("Spec[slots] = %s, want the stored bytes carried unread", got.Spec[SpecKeySlots])
	}
	// Spec.Slots() decodes a list and is happy with two members; it is
	// slots.Sections that names the collision, and it still gets the chance.
	parsed, err := got.Spec.Slots()
	if err != nil {
		t.Fatalf("Slots() = error %v, want the duplicate left for the renderer", err)
	}
	if len(parsed) != 2 {
		t.Errorf("Slots() returned %d slots, want both members carried through: %+v", len(parsed), parsed)
	}
}
