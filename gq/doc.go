// Package gq implements the gq query engine as a library: the shared logic
// behind the gq command-line tool, exported so that other frontends (such as
// MCP servers) can offer the same behavior without reimplementing it.
//
// The package provides three pieces:
//
//   - [Pipeline] drives a decoded [gobspect.Stream] through the standard
//     result-iteration steps — query matching, top-level value indexing,
//     sorting, offset, and limit — delivering each result to a [Sink] or to
//     one of the turnkey drivers [Pipeline.RunRender] and [Pipeline.RunTabular].
//   - [Render] writes a single [gobspect.Value] in one of the gq output
//     formats (pretty, json, jsonl) with the raw, compact, and color switches.
//   - [Aggregate] reduces query matches numerically (count, sum, min, max,
//     avg) with exact int64 arithmetic that degrades to float64 only when
//     necessary.
//
// The semantics are the gq command's semantics: consult the gq README for the
// meaning of index, offset, limit, sort specs, and the output formats. When a
// behavior question arises, the answer is "whatever gq does" — the command is
// built on this package.
package gq
