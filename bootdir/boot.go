package bootdir

import (
	"fmt"
	"strings"
)

// renderBoot returns the file the assembled slot content is written to.
//
// The content is [Instance].Boot verbatim, including whether it ends in a
// newline: the slots were assembled before rendering began, and the bytes that
// came back are the bytes an agent reads.
//
// An instance carrying no assembled content renders no file at all. An empty
// boot file is a file that says nothing, and a boot directory holding one
// would suggest a slot resolved to nothing rather than that none was declared.
func renderBoot(inst *Instance) ([]File, error) {
	if inst == nil || inst.Profile == nil {
		return nil, ErrNoProfile
	}
	if strings.TrimSpace(inst.Boot) == "" {
		return nil, nil
	}
	if !inst.Layout.Boot.Declared() {
		return nil, fmt.Errorf(
			"%w: %d bytes of slot content were assembled, but this layout declares no path for the boot file",
			ErrProviderLayout, len(inst.Boot))
	}
	return []File{{
		Path:    inst.Layout.Boot.RelPath,
		Content: []byte(inst.Boot),
		Mode:    inst.Layout.Boot.Mode,
	}}, nil
}
