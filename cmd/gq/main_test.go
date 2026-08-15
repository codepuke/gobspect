package main

import (
	"bytes"
	"compress/gzip"
	"encoding/gob"
	"io"
	"math"
	"os"
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

// TestRun_SortModifiersWithoutSort verifies that -sort-desc without -sort is
// rejected: it would otherwise be silently ignored.
func TestRun_SortModifiersWithoutSort(t *testing.T) {
	r := gobEncodeValues(t, testPerson{Name: "Alice"})

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-sort-desc"}, r, &stdout, &stderr)

	assert.Equal(t, 2, exitCode)
	assert.Contains(t, stderr.String(), "-sort-desc has no effect without -sort")
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

// TestRun_SortPerKeyDirection verifies the -sort Name,Score:desc syntax.
func TestRun_SortPerKeyDirection(t *testing.T) {
	type rec struct {
		Name  string
		Score int
	}
	r := gobEncodeValues(t,
		rec{Name: "Alice", Score: 10},
		rec{Name: "Bob", Score: 50},
		rec{Name: "Alice", Score: 90},
		rec{Name: "Bob", Score: 20},
	)

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-sort", "Name,Score:desc"}, r, &stdout, &stderr)
	require.Equal(t, 0, exitCode, "stderr: %s", stderr.String())
	out := stdout.String()

	// Expected order: Alice-90, Alice-10, Bob-50, Bob-20
	a90 := strings.Index(out, "Score: 90")
	a10 := strings.Index(out, "Score: 10")
	b50 := strings.Index(out, "Score: 50")
	b20 := strings.Index(out, "Score: 20")
	require.Positive(t, a90)
	require.Positive(t, a10)
	require.Positive(t, b50)
	require.Positive(t, b20)
	assert.Less(t, a90, a10, "Alice 90 should come before Alice 10 (Score desc within Name asc)")
	assert.Less(t, a10, b50, "All Alices should come before Bobs")
	assert.Less(t, b50, b20, "Bob 50 should come before Bob 20")
}

// TestRun_PerPartitionSort verifies that -hetero partition sorts within each
// bucket and keeps buckets in arrival order.
func TestRun_PerPartitionSort(t *testing.T) {
	type a struct{ Name string }
	type b struct{ Title string }

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	require.NoError(t, enc.Encode(a{Name: "Zeta"}))
	require.NoError(t, enc.Encode(a{Name: "Alpha"}))
	require.NoError(t, enc.Encode(b{Title: "Yak"}))
	require.NoError(t, enc.Encode(b{Title: "Badger"}))
	require.NoError(t, enc.Encode(a{Name: "Mike"}))

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{
		"-format", "csv",
		"-hetero", "partition",
		"-sort", "Name,Title",
	}, &buf, &stdout, &stderr)
	require.Equal(t, 0, exitCode, "stderr: %s", stderr.String())

	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	// First partition: Name header followed by Alpha, Mike, Zeta.
	// Blank line, then Title header + Badger, Yak.
	// Lines: [0] Name [1] Alpha [2] Mike [3] Zeta [4] "" [5] Title [6] Badger [7] Yak
	require.GreaterOrEqual(t, len(lines), 8, "unexpected output:\n%s", stdout.String())
	assert.Equal(t, "Name", lines[0])
	assert.Equal(t, "Alpha", lines[1])
	assert.Equal(t, "Mike", lines[2])
	assert.Equal(t, "Zeta", lines[3])
	assert.Equal(t, "", lines[4])
	assert.Equal(t, "Title", lines[5])
	assert.Equal(t, "Badger", lines[6])
	assert.Equal(t, "Yak", lines[7])
}

// TestRun_JSONLFormat verifies that -format jsonl emits one compact JSON
// object per line, without a wrapping array.
func TestRun_JSONLFormat(t *testing.T) {
	r := gobEncodeValues(t,
		testPerson{Name: "Alice"},
		testPerson{Name: "Bob"},
	)

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-format", "jsonl"}, r, &stdout, &stderr)
	require.Equal(t, 0, exitCode, "stderr: %s", stderr.String())
	out := stdout.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Len(t, lines, 2, "expected two lines for two values, got:\n%s", out)
	for _, line := range lines {
		// Each line should be valid JSON and contain the struct discriminator.
		assert.True(t, strings.HasPrefix(line, "{"), "line must start with {: %q", line)
		assert.Contains(t, line, `"kind":"struct"`, "jsonl is compact; no spaces")
	}
}

// TestRun_SchemaFormatJSON verifies -schema -schema-format json emits
// machine-readable schema output rather than Go syntax.
func TestRun_SchemaFormatJSON(t *testing.T) {
	r := gobEncodeValues(t, testPerson{Name: "Alice"})

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-schema", "-schema-format", "json"}, r, &stdout, &stderr)
	require.Equal(t, 0, exitCode, "stderr: %s", stderr.String())
	out := stdout.String()

	// JSON output is an array of type declarations.
	assert.True(t, strings.HasPrefix(strings.TrimSpace(out), "["), "expected JSON array, got:\n%s", out)
	assert.Contains(t, out, `"name"`)
	assert.Contains(t, out, `"kind"`)
	assert.Contains(t, out, `"fields"`)
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

// TestRun_Stats verifies that -stats reports message counts, per-type
// breakdown, and field presence rates.
func TestRun_Stats(t *testing.T) {
	r := gobEncodeValues(t,
		testPerson{Name: "Alice"},
		testPerson{Name: "Bob"},
		testPerson{Name: "Charlie"},
	)

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-stats"}, r, &stdout, &stderr)
	require.Equal(t, 0, exitCode, "stderr: %s", stderr.String())
	out := stdout.String()

	assert.Contains(t, out, "messages:")
	assert.Contains(t, out, "values: 3")
	assert.Contains(t, out, "by type:")
	// testPerson has a single Name field; presence should report 3/3.
	assert.Contains(t, out, "Name")
}

// TestRun_StatsJSON verifies that -stats -format json emits machine-readable
// stats output with expected top-level keys.
func TestRun_StatsJSON(t *testing.T) {
	r := gobEncodeValues(t, testPerson{Name: "Alice"}, testPerson{Name: "Bob"})

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-stats", "-format", "json"}, r, &stdout, &stderr)
	require.Equal(t, 0, exitCode, "stderr: %s", stderr.String())
	out := stdout.String()
	assert.Contains(t, out, `"TotalMessages"`)
	assert.Contains(t, out, `"ByType"`)
	assert.Contains(t, out, `"FieldPresence"`)
}

// TestRun_Diff verifies that -diff reports changes between two gob files and
// exits with code 1 when differences exist.
func TestRun_Diff(t *testing.T) {
	// Left file.
	leftBuf := encodeStream(t, testPerson{Name: "Alice"}, testPerson{Name: "Bob"})
	// Right file (differs on position 1).
	rightPath := writeTempGob(t, testPerson{Name: "Alice"}, testPerson{Name: "Bobby"})

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-diff", rightPath}, leftBuf, &stdout, &stderr)
	assert.Equal(t, 1, exitCode, "diff with changes should exit 1; stderr: %s", stderr.String())
	out := stdout.String()
	assert.Contains(t, out, "[1]")
	assert.Contains(t, out, "Bob")
	assert.Contains(t, out, "Bobby")
}

// TestRun_DiffNoChanges verifies that -diff returns code 0 when the two
// streams are identical, and prints the "no differences" marker.
func TestRun_DiffNoChanges(t *testing.T) {
	leftBuf := encodeStream(t, testPerson{Name: "Alice"})
	rightPath := writeTempGob(t, testPerson{Name: "Alice"})

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-diff", rightPath}, leftBuf, &stdout, &stderr)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout.String(), "no differences")
}

// TestRun_Count verifies that -count reports the number of query matches.
func TestRun_Count(t *testing.T) {
	r := gobEncodeValues(t,
		testPerson{Name: "Alice"},
		testPerson{Name: "Bob"},
		testPerson{Name: "Charlie"},
	)
	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-count"}, r, &stdout, &stderr)
	require.Equal(t, 0, exitCode, "stderr: %s", stderr.String())
	assert.Equal(t, "3\n", stdout.String())
}

// TestRun_SumMinMaxAvg verifies the numeric aggregators.
func TestRun_SumMinMaxAvg(t *testing.T) {
	type rec struct{ Score int }
	raw := encodeStream(t, rec{Score: 10}, rec{Score: 20}, rec{Score: 30})

	cases := []struct {
		flag, value string
		want        string
	}{
		{"sum", "Score", "60\n"},
		{"min", "Score", "10\n"},
		{"max", "Score", "30\n"},
		{"avg", "Score", "20\n"},
	}
	for _, tc := range cases {
		t.Run(tc.flag, func(t *testing.T) {
			input := bytes.NewReader(raw.Bytes())
			var stdout, stderr bytes.Buffer
			args := []string{"-" + tc.flag, tc.value}
			exitCode := run(args, input, &stdout, &stderr)
			require.Equal(t, 0, exitCode, "stderr: %s", stderr.String())
			assert.Equal(t, tc.want, stdout.String())
		})
	}
}

// encodeStream mirrors the helper used elsewhere but kept local since main_test
// already imports encoding/gob.
func encodeStream(t *testing.T, vals ...any) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	for _, v := range vals {
		require.NoError(t, enc.Encode(v))
	}
	return &buf
}

// writeTempGob encodes vals to a temp .gob file and returns its path; the
// file is cleaned up automatically via t.TempDir.
func writeTempGob(t *testing.T, vals ...any) string {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/data.gob"
	buf := encodeStream(t, vals...)
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))
	return path
}

// TestRun_DiffColor verifies that -diff output honors -color by wrapping
// additions green and removals red.
func TestRun_DiffColor(t *testing.T) {
	leftBuf := encodeStream(t, testPerson{Name: "Alice"})
	rightPath := writeTempGob(t, testPerson{Name: "Alicia"})

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-diff", rightPath, "-color"}, leftBuf, &stdout, &stderr)
	assert.Equal(t, 1, exitCode, "changes present; stderr: %s", stderr.String())
	out := stdout.String()
	assert.Contains(t, out, "\x1b[31m", "expected red (removed) ANSI wrapping")
	assert.Contains(t, out, "\x1b[32m", "expected green (added) ANSI wrapping")
	assert.Contains(t, out, "Alice")
	assert.Contains(t, out, "Alicia")
}

// TestRun_DiffNoColor confirms that -no-color suppresses ANSI output even on
// a terminal.
func TestRun_DiffNoColor(t *testing.T) {
	leftBuf := encodeStream(t, testPerson{Name: "Alice"})
	rightPath := writeTempGob(t, testPerson{Name: "Alicia"})

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-diff", rightPath, "-no-color"}, leftBuf, &stdout, &stderr)
	assert.Equal(t, 1, exitCode, "stderr: %s", stderr.String())
	assert.NotContains(t, stdout.String(), "\x1b[", "-no-color must suppress ANSI escapes")
}

// TestRun_HelpExitsZero verifies that explicit -h prints usage to stdout and
// exits 0 (help is not a usage error).
func TestRun_HelpExitsZero(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-h"}, strings.NewReader(""), &stdout, &stderr)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout.String(), "usage: gq [flags] [query]")
	assert.Empty(t, stderr.String())
}

// TestRun_BadFlagExitsTwo verifies that an unknown flag prints usage to
// stderr and exits 2.
func TestRun_BadFlagExitsTwo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-definitely-not-a-flag"}, strings.NewReader(""), &stdout, &stderr)

	assert.Equal(t, 2, exitCode)
	assert.Contains(t, stderr.String(), "usage: gq [flags] [query]")
	assert.Empty(t, stdout.String())
}

// TestRun_GzipFileWithoutSuffix verifies that gzip detection for -f files is
// content-based: a gzipped file works regardless of its extension.
func TestRun_GzipFileWithoutSuffix(t *testing.T) {
	var raw bytes.Buffer
	require.NoError(t, gob.NewEncoder(&raw).Encode(testPerson{Name: "Zipped"}))

	f, err := os.CreateTemp(t.TempDir(), "data.gob") // no .gz suffix
	require.NoError(t, err)
	gz := gzip.NewWriter(f)
	_, err = gz.Write(raw.Bytes())
	require.NoError(t, err)
	require.NoError(t, gz.Close())
	require.NoError(t, f.Close())

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-f", f.Name()}, strings.NewReader(""), &stdout, &stderr)

	require.Equal(t, 0, exitCode, "stderr: %s", stderr.String())
	assert.Contains(t, stdout.String(), "Zipped")
}

type testScore struct{ ID int64 }

// TestRun_SumLargeIntsExact verifies integer aggregation stays exact above
// 2^53, where float64 accumulation would silently round.
func TestRun_SumLargeIntsExact(t *testing.T) {
	// 2^60 + 1 and 2: float64 accumulation would lose the +1.
	r := gobEncodeValues(t,
		testScore{ID: 1<<60 + 1},
		testScore{ID: 2},
	)

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-sum", "ID"}, r, &stdout, &stderr)

	require.Equal(t, 0, exitCode, "stderr: %s", stderr.String())
	assert.Equal(t, "1152921504606846979\n", stdout.String(), "sum must be exact, not float64-rounded")
}

// TestRun_SumDegradesToFloat verifies mixed int/float aggregation produces a
// float result.
func TestRun_SumDegradesToFloat(t *testing.T) {
	type mixed struct{ V float64 }
	r := gobEncodeValues(t, mixed{V: 1.5}, mixed{V: 2})

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-sum", "V"}, r, &stdout, &stderr)

	require.Equal(t, 0, exitCode, "stderr: %s", stderr.String())
	assert.Equal(t, "3.5\n", stdout.String())
}

// TestRun_NonFiniteJSON verifies NaN floats serialize as strings by default
// and as null under -nonfinite null, instead of failing the document.
func TestRun_NonFiniteJSON(t *testing.T) {
	type withNaN struct{ F float64 }
	v := withNaN{F: math.NaN()}

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-format", "json"}, gobEncodeValues(t, v), &stdout, &stderr)
	require.Equal(t, 0, exitCode, "stderr: %s", stderr.String())
	assert.Contains(t, stdout.String(), `"NaN"`)

	stdout.Reset()
	stderr.Reset()
	exitCode = run([]string{"-format", "json", "-nonfinite", "null"}, gobEncodeValues(t, v), &stdout, &stderr)
	require.Equal(t, 0, exitCode, "stderr: %s", stderr.String())
	assert.Contains(t, stdout.String(), `"v": null`)
}
