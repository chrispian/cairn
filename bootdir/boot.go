package bootdir

import (
	"fmt"
	"strings"
)

// renderBoot returns the file the assembled slot content is written to.
//
// The content is [Instance].Boot verbatim, with one newline appended if it
// lacks one. The slots were assembled before rendering began and the bytes
// that came back are the bytes an agent reads; the newline is this package's,
// because a file is what is being written and a text file ends in a newline.
// The assembler leaves it off whenever the last slot resolved empty, so
// without this the boot file's final byte would depend on which slot happened
// to come last.
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
	content := inst.Boot
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return []File{{
		Path:    inst.Layout.Boot.RelPath,
		Content: []byte(content),
		Mode:    inst.Layout.Boot.Mode,
	}}, nil
}
