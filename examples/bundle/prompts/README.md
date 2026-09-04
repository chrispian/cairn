# prompts

One flat `.md` file per prompt, named after the prompt. A profile declares the
ones its boot directory carries under `spec.prompts`, and `--prompt <a,b,c>`
adds more for one launch. Each lands at
`.claude/commands/boot/<name>.md` in the boot directory, so the operator invokes
it as `/boot:<name>`.

**A prompt is a template.** The same `<!-- cairn:slot ... -->` and
`<!-- cairn:value ... -->` markers a `templates:` entry carries are substituted
here, from the same slots and the same instance values. There is no second
syntax and no conditionals: a prompt that must differ is two prompts, or a slot.

**Cairn plants the file and stops.** Nothing fires a prompt at launch — the
operator types the command. That is why a prompt is worth a file rather than a
string on a command line.

A name is written without its extension: `spec.prompts: [handoff]` reads
`handoff.md` here and plants `handoff.md` there. The directory is named by
`spec.prompts_dir`, which cascades closest-wins exactly as `skills_dir` does —
see `../profiles/base.md`.
