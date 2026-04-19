// Package query provides path-based navigation of decoded gobspect Value trees.
//
// Navigate nested structs, arrays, slices, and maps without writing type switches.
// Paths are dot-separated segment expressions:
//
//	v, ok   := query.Get(root, "Orders.0.Customer.Name")
//	names   := query.All(root, "Orders.*.Customer.Name")
//	keys, _ := query.Keys(root, "Orders.0")
//
//	// Recursive descent: find Price anywhere in a deep struct
//	prices  := query.All(root, "..Price")
//
//	// Wildcard descent with a contains filter
//	active  := query.All(root, "..[Tags~devops][Status=active]")
//
// # Path syntax
//
// Segments are separated by dots. Supported segment forms:
//
//   - Named field (e.g. "Name") — navigates a struct field or a map[string]T entry
//   - Non-negative integer (e.g. "0", "42") — indexes into a slice or array
//   - Negative integer (e.g. "-1") — counts from the end; -1 is the last element
//   - Wildcard "*" — expands to all elements of a slice, array, or map
//   - Existence filter "[Field!]" — keeps elements that have Field present
//   - Glob filter "[Field=pattern]" — keeps elements where Field is a string matching pattern
//   - Contains filter "[Field~pattern]" — keeps elements where Field is a slice/array/map containing a string matching pattern
//   - Recursive descent "..Name" — finds all nodes named Name at any depth
//   - Wildcard recursive descent "..[Filter]" — traverses all depths, keeping nodes matching Filter
//   - Field projection "A,B" — returns an anonymous struct with only the selected fields
//
// An empty path resolves to the root value (identity).
// InterfaceValue nodes are unwrapped transparently at every step.
// Out-of-bounds indices return (nil, false) rather than panicking.
//
// # One-off functions
//
// [Get], [All], [AllSeq], [Keys], and [MustGet] accept a path expression as a
// plain string and panic if the expression is syntactically invalid. This is
// intentional: these functions are designed for call sites where the path is a
// compile-time constant and a bad expression is a programming error.
//
//	v, ok   := query.Get(root, "Orders.0.Customer.Name")
//	names   := query.All(root, "Orders.*.Customer.Name")
//	keys, _ := query.Keys(root, "")
//	name    := query.MustGet(root, "Orders.0.Customer.Name") // panics if not found
//
// # Pre-compiled paths
//
// Use [Parse] with [GetPath], [AllPath], [AllPathSeq], and [KeysPath] when
// evaluating the same path expression against many roots, or when you need to
// handle syntax errors without panicking:
//
//	p, err := query.Parse("Orders.*.Customer[Name=?*].Address")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	for _, root := range roots {
//	    addrs := query.AllPath(root, p)
//	    // ...
//	}
//
// # Filter patterns
//
// Glob patterns follow path.Match semantics. "*" matches zero or more characters;
// "?" matches exactly one character. Use "[Field=?*]" to require a non-empty string.
//
// The "[Field~pattern]" filter checks collection membership: it keeps elements where
// Field is a slice, array, or map that contains at least one string entry matching
// pattern. Only StringValue elements and keys are checked; non-string entries are
// silently skipped. Use "[Field~?*]" to require at least one non-empty entry.
//
// See the package README for the full path syntax reference and examples.
package query
