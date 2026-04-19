package main

import (
	"bytes"
	"encoding/gob"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testPerson struct{ Name string }

func gobEncodeValues(t *testing.T, vals ...any) io.Reader {
	t.Helper()
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	for _, v := range vals {
		if err := enc.Encode(v); err != nil {
			t.Fatal(err)
		}
	}
	return &buf
}

// TestRun_SortAscending verifies -sort Name produces ascending output.
func TestRun_SortAscending(t *testing.T) {
	r := gobEncodeValues(t,
		testPerson{Name: "Charlie"},
		testPerson{Name: "Alice"},
		testPerson{Name: "Bob"},
	)

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-sort", "Name"}, r, &stdout, &stderr)

	require.Equal(t, 0, exitCode, "stderr: %s", stderr.String())
	out := stdout.String()
	posAlice := strings.Index(out, "Alice")
	posBob := strings.Index(out, "Bob")
	posCharlie := strings.Index(out, "Charlie")
	assert.Less(t, posAlice, posBob, "Alice should appear before Bob")
	assert.Less(t, posBob, posCharlie, "Bob should appear before Charlie")
}

// TestRun_SortDescending verifies -sort Name -sort-desc produces descending output.
func TestRun_SortDescending(t *testing.T) {
	r := gobEncodeValues(t,
		testPerson{Name: "Charlie"},
		testPerson{Name: "Alice"},
		testPerson{Name: "Bob"},
	)

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-sort", "Name", "-sort-desc"}, r, &stdout, &stderr)

	require.Equal(t, 0, exitCode, "stderr: %s", stderr.String())
	out := stdout.String()
	posAlice := strings.Index(out, "Alice")
	posBob := strings.Index(out, "Bob")
	posCharlie := strings.Index(out, "Charlie")
	assert.Less(t, posCharlie, posBob, "Charlie should appear before Bob")
	assert.Less(t, posBob, posAlice, "Bob should appear before Alice")
}

// TestRun_SortFold verifies -sort-fold produces case-insensitive ordering.
func TestRun_SortFold(t *testing.T) {
	r := gobEncodeValues(t,
		testPerson{Name: "CHARLIE"},
		testPerson{Name: "alice"},
		testPerson{Name: "Bob"},
	)

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-sort", "Name", "-sort-fold"}, r, &stdout, &stderr)

	require.Equal(t, 0, exitCode, "stderr: %s", stderr.String())
	out := stdout.String()
	posAlice := strings.Index(out, "alice")
	posBob := strings.Index(out, "Bob")
	posCharlie := strings.Index(out, "CHARLIE")
	assert.Less(t, posAlice, posBob, "alice should appear before Bob (case-insensitive)")
	assert.Less(t, posBob, posCharlie, "Bob should appear before CHARLIE (case-insensitive)")
}

// TestRun_SortModifiersWithoutSort verifies that -sort-desc without -sort emits a
// warning but exits 0.
func TestRun_SortModifiersWithoutSort(t *testing.T) {
	r := gobEncodeValues(t, testPerson{Name: "Alice"})

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-sort-desc"}, r, &stdout, &stderr)

	assert.Equal(t, 0, exitCode, "should be a warning, not an error")
	assert.Contains(t, stderr.String(), "-sort-* flags have no effect without -sort")
}

// TestRun_SortWithLimitOffset verifies -sort combined with -limit and -offset
// applies offset/limit to the sorted result set.
func TestRun_SortWithLimitOffset(t *testing.T) {
	r := gobEncodeValues(t,
		testPerson{Name: "Eve"},
		testPerson{Name: "Bob"},
		testPerson{Name: "Dave"},
		testPerson{Name: "Alice"},
		testPerson{Name: "Charlie"},
	)

	// Sorted: Alice, Bob, Charlie, Dave, Eve
	// offset=1, limit=2 → Bob, Charlie
	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-sort", "Name", "-limit", "2", "-offset", "1"}, r, &stdout, &stderr)

	require.Equal(t, 0, exitCode, "stderr: %s", stderr.String())
	out := stdout.String()
	assert.Contains(t, out, "Bob")
	assert.Contains(t, out, "Charlie")
	assert.NotContains(t, out, "Alice")
	assert.NotContains(t, out, "Dave")
	assert.NotContains(t, out, "Eve")
}

// TestRun_SortDropMissing verifies that -sort-drop-missing excludes records that
// lack all sort key fields.
func TestRun_SortDropMissing(t *testing.T) {
	// testRecord has both Name and Zip; encode some with Zip, some without
	// by using structs that only have a Name field for the "missing" case.
	type recordWithZip struct {
		Name string
		Zip  string
	}
	type recordNoZip struct {
		Name string
	}

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	require.NoError(t, enc.Encode(recordWithZip{Name: "Alice", Zip: "10001"}))
	require.NoError(t, enc.Encode(recordNoZip{Name: "Bob"}))
	require.NoError(t, enc.Encode(recordWithZip{Name: "Charlie", Zip: "94105"}))

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-sort", "Zip", "-sort-drop-missing"}, &buf, &stdout, &stderr)

	require.Equal(t, 0, exitCode, "stderr: %s", stderr.String())
	out := stdout.String()
	assert.Contains(t, out, "Alice")
	assert.Contains(t, out, "Charlie")
	assert.NotContains(t, out, "Bob", "Bob has no Zip field and should be excluded")
}
