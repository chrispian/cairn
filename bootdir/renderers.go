package bootdir

import "github.com/chrispian/cairn/profile"

// Renderers returns the artifact renderers a boot directory is rendered from,
// in render order.
//
// The order is the order files appear in a rendering, and it is also the order
// a failure is reported in, so the templates come first: a profile with a
// broken marker should fail on the marker, not on a skill.
//
// Each Artifact is the name of one manifest key or one line of the output
// contract, which is what a diagnostic quotes. It is a label and never a path.
// Two of the artifacts take their paths from the provider's BootDirSpec; the
// rest take them from the manifest, and the templates, skills, prompts,
// subagents, trees and files renderers each emit many files.
func Renderers() []Renderer {
	return []Renderer{
		{Artifact: profile.SpecKeyTemplates, Render: renderTemplates},
		{Artifact: ".mcp.json", Render: renderMCP},
		{Artifact: ".claude/settings.json", Render: RenderSettings},
		{Artifact: SkillsDirName, Render: RenderSkills},
		{Artifact: PromptsDirName, Render: renderPrompts},
		{Artifact: SubagentsDirName, Render: renderSubagents},
		{Artifact: profile.SpecKeyTrees, Render: renderTrees},
		{Artifact: profile.SpecKeyFiles, Render: renderFiles},
	}
}
