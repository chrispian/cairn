package bootdir

// Renderers returns the artifact renderers a boot directory is rendered from,
// in render order.
//
// The order is the order files appear in a rendering, and it is also the order
// a failure is reported in, so the instruction file comes first: a profile
// with a broken manifest should fail on the manifest, not on a skill.
//
// Each Artifact is the name of one line of the output contract, which is what
// a diagnostic quotes. It is a label and never a path: the path an artifact is
// written to comes from the instance's [Layout], four of whose artifacts
// belong to the provider rather than to cairn, and the skills and files
// renderers each emit a tree.
func Renderers() []Renderer {
	return []Renderer{
		{Artifact: AgentsFileName, Render: renderAgents},
		{Artifact: "CLAUDE.md", Render: renderPointer},
		{Artifact: "boot.md", Render: renderBoot},
		{Artifact: ".mcp.json", Render: renderMCP},
		{Artifact: ".claude/settings.json", Render: renderSettings},
		{Artifact: SkillsDirName, Render: renderSkills},
		{Artifact: "files", Render: renderFiles},
	}
}
