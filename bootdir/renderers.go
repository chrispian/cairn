package bootdir

// Renderers returns the artifact renderers a boot directory is rendered from,
// in render order.
//
// The order is the order files appear in a rendering, and it is also the order
// a failure is reported in, so the instruction file comes first: a profile
// with a broken manifest should fail on the manifest, not on a skill.
func Renderers() []Renderer {
	return []Renderer{}
}
