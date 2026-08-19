package gq

// ToNumericForTest exposes toNumeric for white-box tests that need to feed
// hand-built Value trees (e.g. nested interface wrappers) into the numeric
// coercion directly.
var ToNumericForTest = toNumeric

// FormatFloatForTest exposes formatFloat so its int64 fast-path bounds can be
// pinned directly.
var FormatFloatForTest = formatFloat
