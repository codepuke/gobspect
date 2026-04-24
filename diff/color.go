package diff

import "github.com/codepuke/gobspect"

// ColorScheme styles a text-rendered Delta. A zero value renders plain text
// (each Style's Apply is the identity), so the same rendering code path is
// used whether or not the caller wants color.
type ColorScheme struct {
	Added   gobspect.Style // lines introduced in the "after" side ("+ …")
	Removed gobspect.Style // lines dropped from the "before" side ("- …")
	Header  gobspect.Style // composite-delta headers ("~ TypeName {" and the closing "}")
	Index   gobspect.Style // stream position markers ("[N]") in FormatStream
}

// ANSIColorScheme is a terminal-ready preset that matches the conventions
// used by git, diff, and delta: additions green, removals red, headers bold
// cyan, position markers dim.
var ANSIColorScheme = ColorScheme{
	Added:   gobspect.Style{Prefix: "\x1b[32m", Suffix: "\x1b[0m"},
	Removed: gobspect.Style{Prefix: "\x1b[31m", Suffix: "\x1b[0m"},
	Header:  gobspect.Style{Prefix: "\x1b[1;36m", Suffix: "\x1b[0m"},
	Index:   gobspect.Style{Prefix: "\x1b[2m", Suffix: "\x1b[0m"},
}

// NoColorScheme is a zero-valued ColorScheme for explicit opt-out. Passing
// it is equivalent to not passing any FormatOption at all.
var NoColorScheme = ColorScheme{}
