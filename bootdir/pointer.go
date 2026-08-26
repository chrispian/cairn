package bootdir

// renderPointer returns the harness's own instruction file, holding nothing
// but an include of [AgentsFileName].
//
// The content is one line so that there is one place agent instructions live.
// A harness that reads its own filename finds the pointer and follows it; a
// reader who opens either file learns immediately which one carries the
// contract, and no edit to one of them can leave the two disagreeing.
//
// A layout whose harness reads [AgentsFileName] directly declares no pointer
// path, and renders no file.
func renderPointer(inst *Instance) ([]File, error) {
	if inst == nil || inst.Profile == nil {
		return nil, ErrNoProfile
	}
	if !inst.Layout.Pointer.Declared() {
		return nil, nil
	}
	return []File{{
		Path:    inst.Layout.Pointer.RelPath,
		Content: []byte(PointerFileContent),
		Mode:    inst.Layout.Pointer.Mode,
	}}, nil
}
