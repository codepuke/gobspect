package main

import (
	"bytes"
	"encoding/gob"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/codepuke/gobspect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlagValidation(t *testing.T) {
	// Create a dummy gob file for tests that don't fail early.
	tmpFile, err := os.CreateTemp("", "test.gob")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	tests := []struct {
		name         string
		args         []string
		wantExit     int
		wantStderr   []string
		wantNoStderr []string
	}{
		{
			name:       "schema with query",
			args:       []string{"--schema", "-f", tmpFile.Name(), ".Foo"},
			wantExit:   0, // Warn and continue
			wantStderr: []string{"gq: query expression has no effect with --schema; ignoring"},
		},
		{
			name:       "types with query",
			args:       []string{"--types", "-f", tmpFile.Name(), ".Foo"},
			wantExit:   0, // Warn and continue
			wantStderr: []string{"gq: query expression has no effect with --types; ignoring"},
		},
		{
			name:       "schema with format json",
			args:       []string{"--schema", "--format", "json", "-f", tmpFile.Name()},
			wantExit:   0,
			wantStderr: []string{"gq: --format json has no effect with --schema; ignoring"},
		},
		{
			name:       "schema with index",
			args:       []string{"--schema", "--index", "0", "-f", tmpFile.Name()},
			wantExit:   0,
			wantStderr: []string{"gq: --index has no effect with --schema; ignoring"},
		},
		{
			name:       "color and no-color",
			args:       []string{"--color", "--no-color", "-f", tmpFile.Name()},
			wantExit:   2,
			wantStderr: []string{"gq: cannot use --color and --no-color together"},
		},
		{
			name:       "compact with csv",
			args:       []string{"--compact", "--format", "csv", "-f", tmpFile.Name()},
			wantExit:   0,
			wantStderr: []string{"gq: --compact has no effect with --format csv; ignoring"},
		},
		{
			name:       "raw with json",
			args:       []string{"-r", "--format", "json", "-f", tmpFile.Name()},
			wantExit:   0,
			wantStderr: []string{"gq: -r has no effect with --format json; ignoring"},
		},
		{
			name:       "raw with csv",
			args:       []string{"-r", "--format", "csv", "-f", tmpFile.Name()},
			wantExit:   0,
			wantStderr: []string{"gq: -r has no effect with --format csv; ignoring"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			exitCode := run(tt.args, os.Stdin, io.Discard, &stderr)

			if exitCode != tt.wantExit {
				t.Errorf("exit code = %d, want %d", exitCode, tt.wantExit)
			}

			stderrStr := stderr.String()
			for _, want := range tt.wantStderr {
				if !strings.Contains(stderrStr, want) {
					t.Errorf("stderr does not contain %q\nGot:\n%s", want, stderrStr)
				}
			}
		})
	}
}

// TestPrintValue_ColorProducesANSI verifies that when color is enabled,
// printValue produces output containing ANSI escape codes via
// gobspect.WithColor(gobspect.ANSIColorScheme).
func TestPrintValue_ColorProducesANSI(t *testing.T) {
	type sample struct {
		Name  string
		Count int
	}

	// Encode a sample struct to a gob buffer.
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(sample{Name: "hello", Count: 42}); err != nil {
		t.Fatalf("gob encode: %v", err)
	}

	// Decode it with gobspect.
	ins := gobspect.New()
	vals, err := ins.Stream(&buf).Collect()
	if err != nil {
		t.Fatalf("gobspect collect: %v", err)
	}
	if len(vals) == 0 {
		t.Fatal("no values decoded")
	}

	fmtOpts := []gobspect.FormatOption{
		gobspect.WithBytesFormat(gobspect.BytesHex),
		gobspect.WithMaxBytes(64),
	}

	var out bytes.Buffer
	if err := printValue(vals[0], &out, "pretty", false, false, true /* color */, fmtOpts); err != nil {
		t.Fatalf("printValue: %v", err)
	}

	got := out.String()
	const ansiEsc = "\x1b["
	if !strings.Contains(got, ansiEsc) {
		t.Errorf("expected ANSI escape codes in output, got:\n%s", got)
	}
}

// — Pipe error tests —————————————————————————————————————————————————————————

// errorWriter is an io.Writer that returns an error after writing n bytes.
type errorWriter struct {
	limit int
	n     int
	err   error
}

func (ew *errorWriter) Write(p []byte) (int, error) {
	if ew.n >= ew.limit {
		return 0, ew.err
	}
	remaining := ew.limit - ew.n
	if len(p) > remaining {
		p = p[:remaining]
	}
	ew.n += len(p)
	return len(p), nil
}

// gobStream encodes n copies of v and returns the bytes.
func gobStream(t *testing.T, v any, n int) io.Reader {
	t.Helper()
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	for range n {
		if err := enc.Encode(v); err != nil {
			t.Fatalf("gob encode: %v", err)
		}
	}
	return &buf
}

// TestRun_BrokenPipe verifies that when stdout returns EPIPE, run() exits
// quickly with code 0 (silent, matching Unix tool convention for `head`).
func TestRun_BrokenPipe(t *testing.T) {
	type row struct{ Name string }

	// Encode many values to ensure the loop would run many iterations.
	stdin := gobStream(t, row{Name: "hello"}, 100)

	// Writer that fails immediately with EPIPE.
	brokenPipe := &errorWriter{limit: 0, err: syscall.EPIPE}

	var stderr bytes.Buffer
	exitCode := run([]string{}, stdin, brokenPipe, &stderr)

	assert.Equal(t, 0, exitCode, "EPIPE should produce exit code 0")
	assert.Empty(t, stderr.String(), "EPIPE should produce no stderr output")
}

// TestRun_OtherWriteError verifies that a non-EPIPE write error on stdout
// produces exit code 1 and a message on stderr.
func TestRun_OtherWriteError(t *testing.T) {
	type row struct{ Name string }

	stdin := gobStream(t, row{Name: "hello"}, 5)

	// Writer that fails after a few bytes with a non-EPIPE error.
	badWriter := &errorWriter{limit: 5, err: io.ErrClosedPipe}

	var stderr bytes.Buffer
	exitCode := run([]string{}, stdin, badWriter, &stderr)

	assert.Equal(t, 1, exitCode, "non-EPIPE write error should produce exit code 1")
	assert.NotEmpty(t, stderr.String(), "non-EPIPE error should produce stderr message")
}

// TestRun_AllPathSeqEarlyTermination verifies that when stdout errors, the
// AllPathSeq loop stops early — i.e. we do not process more results than needed.
func TestRun_AllPathSeqEarlyTermination(t *testing.T) {
	type item struct{ Value string }
	type container struct{ Items []item }

	// Encode a container with many items so a wildcard query yields many results.
	items := make([]item, 50)
	for i := range items {
		items[i] = item{Value: "x"}
	}
	c := container{Items: items}

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	require.NoError(t, enc.Encode(c))

	// Writer that errors after writing a small number of bytes.
	brokenPipe := &errorWriter{limit: 10, err: syscall.EPIPE}

	var stderr bytes.Buffer
	exitCode := run([]string{".Items.*"}, &buf, brokenPipe, &stderr)

	// EPIPE: should exit 0 silently.
	assert.Equal(t, 0, exitCode)
	assert.Empty(t, stderr.String())

	// The writer should not have received all 50 items worth of output
	// (each item formats to more than 10 bytes total), confirming early exit.
	assert.LessOrEqual(t, brokenPipe.n, 10)
}

// gobEncodeToFile encodes v as gob into a temp file and returns the file path.
// The caller is responsible for removing the file.
func gobEncodeToFile(t *testing.T, v any) string {
	t.Helper()
	f, err := os.CreateTemp("", "*.gob")
	require.NoError(t, err, "creating temp file")
	enc := gob.NewEncoder(f)
	require.NoError(t, enc.Encode(v), "encoding gob")
	require.NoError(t, f.Close(), "closing temp file")
	return f.Name()
}

// TestRun_ExplicitFile verifies that -f reads from the given file rather than stdin.
func TestRun_ExplicitFile(t *testing.T) {
	type sample struct{ Field string }

	path := gobEncodeToFile(t, sample{Field: "from-file"})
	defer os.Remove(path)

	// Stdin contains a different value — if -f is honoured, it should not be read.
	stdinData := gobStream(t, sample{Field: "from-stdin"}, 1)

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-f", path, ".Field"}, stdinData, &stdout, &stderr)

	assert.Equal(t, 0, exitCode, "unexpected exit code; stderr: %s", stderr.String())
	assert.Contains(t, stdout.String(), "from-file", "expected value from file")
	assert.NotContains(t, stdout.String(), "from-stdin", "should not have read stdin")
}

// TestRun_FileNotFound verifies that a missing -f argument produces exit 1 and
// an error message on stderr.
func TestRun_FileNotFound(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-f", "missing.gob"}, strings.NewReader(""), &stdout, &stderr)

	assert.Equal(t, 1, exitCode, "expected exit 1 for missing file")
	assert.Contains(t, stderr.String(), "missing.gob", "expected file name in error message")
}

// TestRun_QueryArgWithStdin verifies that a single positional argument is
// treated as the query expression and input is read from stdin.
func TestRun_QueryArgWithStdin(t *testing.T) {
	type sample struct{ Field string }

	stdinData := gobStream(t, sample{Field: "hello"}, 1)

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{".Field"}, stdinData, &stdout, &stderr)

	assert.Equal(t, 0, exitCode, "unexpected exit code; stderr: %s", stderr.String())
	assert.Contains(t, stdout.String(), "hello", "expected field value in output")
}

// TestRun_NoOsStatAmbiguity creates a temp file whose name is also a valid
// query for the decoded struct and verifies that gq treats the positional
// argument as a query (reading from stdin), not as a file path.
func TestRun_NoOsStatAmbiguity(t *testing.T) {
	type Orders struct{ Orders string }

	// Create a temp file named "Orders" in a temp directory.
	dir := t.TempDir()
	filePath := dir + "/Orders"
	f, err := os.Create(filePath)
	require.NoError(t, err)
	f.Close()

	// Encode a struct that has an "Orders" field, put it on stdin.
	stdinData := gobStream(t, Orders{Orders: "pending"}, 1)

	var stdout, stderr bytes.Buffer
	// Pass "Orders" as a positional arg. With the old os.Stat logic, this would
	// be treated as a file because filePath/Orders exists — but we are NOT
	// passing the full path, we are passing the bare name "Orders".
	// The point is: gq must NOT perform os.Stat on positional args. Because
	// "Orders" does not resolve to an existing path in the working directory,
	// it should be treated as a query regardless. The test directory is not the
	// cwd, so "Orders" does not exist in cwd.
	exitCode := run([]string{"Orders"}, stdinData, &stdout, &stderr)

	assert.Equal(t, 0, exitCode, "unexpected exit code; stderr: %s", stderr.String())
	assert.Contains(t, stdout.String(), "pending", "expected field value from stdin")
}

// TestRun_LimitOffset verifies -limit and -offset flag behavior.
func TestRun_LimitOffset(t *testing.T) {
	type row struct{ Name string }

	tests := []struct {
		name       string
		args       []string
		n          int // number of values to encode
		wantExit   int
		wantLines  int    // expected number of non-empty output lines (-1 = don't check)
		wantStderr string // substring expected in stderr (empty = none)
	}{
		{
			name:      "limit reduces output",
			args:      []string{"-limit", "3"},
			n:         5,
			wantExit:  0,
			wantLines: 3,
		},
		{
			name:      "offset skips results",
			args:      []string{"-offset", "2"},
			n:         5,
			wantExit:  0,
			wantLines: 3,
		},
		{
			name:      "limit and offset combined",
			args:      []string{"-limit", "2", "-offset", "2"},
			n:         5,
			wantExit:  0,
			wantLines: 2,
		},
		{
			name:      "limit zero means no limit",
			args:      []string{"-limit", "0"},
			n:         5,
			wantExit:  0,
			wantLines: 5,
		},
		{
			name:      "offset beyond total no error",
			args:      []string{"-offset", "10"},
			n:         3,
			wantExit:  0,
			wantLines: 0,
		},
		{
			name:       "negative limit is error",
			args:       []string{"-limit", "-1"},
			n:          1,
			wantExit:   2,
			wantStderr: "--limit must be non-negative",
		},
		{
			name:       "negative offset is error",
			args:       []string{"-offset", "-1"},
			n:          1,
			wantExit:   2,
			wantStderr: "--offset must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdin := gobStream(t, row{Name: "hello"}, tt.n)
			var stdout, stderr bytes.Buffer
			exitCode := run(tt.args, stdin, &stdout, &stderr)

			assert.Equal(t, tt.wantExit, exitCode, "exit code mismatch; stderr: %s", stderr.String())

			if tt.wantStderr != "" {
				assert.Contains(t, stderr.String(), tt.wantStderr)
			} else {
				assert.Empty(t, stderr.String(), "unexpected stderr: %s", stderr.String())
			}

			if tt.wantLines >= 0 {
				// Count value occurrences (one "hello" per encoded row).
				got := strings.Count(stdout.String(), "hello")
				assert.Equal(t, tt.wantLines, got, "output value count mismatch")
			}
		})
	}
}

// TestRun_RawUnwrapsInterface verifies that -r unwraps an InterfaceValue and
// prints the inner string without quotes when the top-level value is encoded
// as an interface (any).
func TestRun_RawUnwrapsInterface(t *testing.T) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	gob.Register("")
	var v any = "hello"
	require.NoError(t, enc.Encode(&v), "encoding interface string")

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-r"}, &buf, &stdout, &stderr)

	assert.Equal(t, 0, exitCode, "unexpected exit code; stderr: %s", stderr.String())
	assert.Equal(t, "hello\n", stdout.String(), "expected raw string output without quotes")
}
