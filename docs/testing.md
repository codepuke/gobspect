# Testing: Gob Fixture Generation

This document covers generating gob-encoded test artifacts for comprehensive regression testing. For general test structure and assertion conventions, see the testing skill in `.claude/skills/testing/SKILL.md`.

## Fixture Generation Approach

Test fixtures are real gob-encoded blobs produced by Go's `encoding/gob` package. This is the only way to guarantee the fixtures match the actual wire format. Hand-crafted byte literals are fragile and unreadable.

A standalone generator program lives at `testdata/generate.go` and is run with `go generate` to produce `.gob` files in `testdata/`. Each file encodes one or more values of a specific type or pattern.

```go
//go:generate go run testdata/generate.go
```

## Generator Structure

```go
// testdata/generate.go
//go:build ignore

package main

import (
    "bytes"
    "encoding/gob"
    "math/big"
    "os"
    "time"
)

func main() {
    generate("testdata/int.gob", 42)
    generate("testdata/string.gob", "hello world")
    generate("testdata/bool_true.gob", true)
    // ... etc
}

func generate(path string, values ...any) {
    var buf bytes.Buffer
    enc := gob.NewEncoder(&buf)
    for _, v := range values {
        if err := enc.Encode(v); err != nil {
            panic(path + ": " + err.Error())
        }
    }
    if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
        panic(err)
    }
}
```

Every fixture is reproducible. Running `go generate` produces byte-identical files. Commit the `.gob` files — they are the regression baseline. If a Go release changes the gob encoding (unlikely but possible), the diff is visible in version control.

## Type Coverage Matrix

The generator must cover every code path in the decoder. Organize fixtures by category:

### Primitive Types

```go
generate("testdata/bool_true.gob", true)
generate("testdata/bool_false.gob", false)
generate("testdata/int_zero.gob", int(0))
generate("testdata/int_positive.gob", int(42))
generate("testdata/int_negative.gob", int(-42))
generate("testdata/int_large.gob", int64(1<<62))
generate("testdata/uint.gob", uint(42))
generate("testdata/uint_large.gob", uint64(1<<63))
generate("testdata/float64.gob", 3.14159)
generate("testdata/float64_zero.gob", 0.0)
generate("testdata/float64_negative.gob", -1.5)
generate("testdata/complex128.gob", complex(1.0, 2.0))
generate("testdata/string_empty.gob", "")
generate("testdata/string_ascii.gob", "hello")
generate("testdata/string_unicode.gob", "日本語テスト")
generate("testdata/bytes_empty.gob", []byte{})
generate("testdata/bytes_data.gob", []byte{0xDE, 0xAD, 0xBE, 0xEF})
```

### Structs

```go
type Simple struct { A int; B string }
type Nested struct { X Simple; Y float64 }
type Empty struct{}
type ManyFields struct { A, B, C, D, E, F, G, H, I, J int }
type SparseFields struct { A int; B int; C int } // encode with B=0 to test field skipping
type WithUnexported struct { A int; b int; C int } // b is skipped

gob.Register(Simple{})
gob.Register(Nested{})

generate("testdata/struct_simple.gob", Simple{A: 1, B: "two"})
generate("testdata/struct_nested.gob", Nested{X: Simple{A: 1, B: "inner"}, Y: 2.5})
generate("testdata/struct_empty.gob", Empty{})
generate("testdata/struct_many_fields.gob", ManyFields{A: 1, C: 3, E: 5, J: 10})
generate("testdata/struct_sparse.gob", SparseFields{A: 1, B: 0, C: 3})
```

### Collections

```go
generate("testdata/slice_int.gob", []int{1, 2, 3})
generate("testdata/slice_string.gob", []string{"a", "b", "c"})
generate("testdata/slice_empty.gob", []int{})
generate("testdata/slice_nested.gob", [][]int{{1, 2}, {3, 4}})
generate("testdata/map_string_int.gob", map[string]int{"a": 1, "b": 2})
generate("testdata/map_int_string.gob", map[int]string{1: "one", 2: "two"})
generate("testdata/map_empty.gob", map[string]int{})
generate("testdata/array.gob", [3]int{1, 2, 3})
```

### Interfaces

Interface values require `gob.Register` and produce embedded type names in the stream. These are critical to test because they exercise the recursive type definition path.

```go
type Animal interface{}
type Dog struct { Name string; Breed string }
type Cat struct { Name string; Indoor bool }

func init() {
    gob.Register(Dog{})
    gob.Register(Cat{})
}

// Encode via a wrapper struct since gob won't encode bare interfaces
type AnimalHolder struct { Pet Animal }

generate("testdata/interface_dog.gob", AnimalHolder{Pet: Dog{Name: "Rex", Breed: "Shepherd"}})
generate("testdata/interface_cat.gob", AnimalHolder{Pet: Cat{Name: "Whiskers", Indoor: true}})
generate("testdata/interface_nil.gob", AnimalHolder{Pet: nil})
```

### Opaque Types (GobEncoder / BinaryMarshaler / TextMarshaler)

These fixtures exercise the opaque decoder registry. The generator imports the real types.

```go
import (
    "math/big"
    "net/netip"
    "net/url"
    "time"
)

generate("testdata/time.gob", time.Date(2024, 1, 15, 9, 30, 0, 123456789, time.FixedZone("CST", -6*3600)))
generate("testdata/time_utc.gob", time.Unix(0, 0).UTC())
generate("testdata/bigint_positive.gob", big.NewInt(123456789))
generate("testdata/bigint_negative.gob", big.NewInt(-99999))
generate("testdata/bigint_zero.gob", big.NewInt(0))
generate("testdata/bigfloat.gob", big.NewFloat(3.14159265358979323846))
generate("testdata/bigrat.gob", big.NewRat(355, 113))

u, _ := url.Parse("https://example.com/path?q=hello&lang=en")
generate("testdata/url.gob", u)

addr := netip.MustParseAddr("192.168.1.1")
generate("testdata/netip_addr.gob", addr)
```

For third-party opaque types (UUID, shopspring/decimal), use a separate Go module in `testdata/thirdparty/` to avoid polluting the main module's dependency graph:

```
testdata/thirdparty/
├── go.mod          # separate module with uuid, decimal deps
├── go.sum
└── generate.go     # produces .gob files in testdata/
```

### Multi-Value Streams

A single gob stream can contain multiple `Encode` calls. Test that the decoder handles this:

```go
func generateMulti(path string) {
    var buf bytes.Buffer
    enc := gob.NewEncoder(&buf)
    enc.Encode(Simple{A: 1, B: "first"})
    enc.Encode(Simple{A: 2, B: "second"})
    enc.Encode(Simple{A: 3, B: "third"})
    os.WriteFile(path, buf.Bytes(), 0644)
}
```

Also test mixed-type multi-value streams. After the first value establishes type definitions, subsequent values of the same type reuse them:

```go
func generateMixedStream(path string) {
    var buf bytes.Buffer
    enc := gob.NewEncoder(&buf)
    enc.Encode(Simple{A: 1, B: "one"})
    enc.Encode(42)  // different type in same stream
    enc.Encode(Simple{A: 2, B: "two"})
    os.WriteFile(path, buf.Bytes(), 0644)
}
```

### Edge Cases and Malformed Input

These are NOT generated by the standard encoder. They are hand-crafted or mutated byte slices for testing error handling:

```go
// In test code, not in generate.go
func TestDecode_MalformedInput(t *testing.T) {
    tests := []struct {
        name  string
        input []byte
        errMsg string
    }{
        {name: "empty stream",         input: []byte{},              errMsg: "EOF"},
        {name: "truncated length",     input: []byte{0xFF},          errMsg: ""},
        {name: "length exceeds data",  input: []byte{0x05, 0x01},    errMsg: ""},
        {name: "negative type id without body", input: makePartialTypeDef(), errMsg: ""},
        {name: "duplicate type id",    input: makeDuplicateTypeDef(), errMsg: "duplicate"},
    }
    // ...
}
```

Additionally, fuzz-mutate valid fixtures for robustness:

```go
func FuzzDecode(f *testing.F) {
    // Seed with every fixture in testdata/
    entries, _ := os.ReadDir("testdata")
    for _, e := range entries {
        if filepath.Ext(e.Name()) == ".gob" {
            data, _ := os.ReadFile(filepath.Join("testdata", e.Name()))
            f.Add(data)
        }
    }

    f.Fuzz(func(t *testing.T, data []byte) {
        ins := New()
        // Must not panic. Errors are fine.
        ins.Decode(bytes.NewReader(data))
    })
}
```

## Golden File Tests for Formatter Output

The formatter's human-readable output is tested with golden files. Each golden file is the expected output of `Format(value)` for a specific fixture.

```go
func TestFormat_Golden(t *testing.T) {
    entries, err := filepath.Glob("testdata/*.gob")
    require.NoError(t, err)

    for _, gobFile := range entries {
        name := strings.TrimSuffix(filepath.Base(gobFile), ".gob")
        t.Run(name, func(t *testing.T) {
            data, err := os.ReadFile(gobFile)
            require.NoError(t, err)

            ins := New()
            vals, err := ins.Decode(bytes.NewReader(data))
            require.NoError(t, err)

            var out strings.Builder
            for _, v := range vals {
                out.WriteString(Format(v))
                out.WriteString("\n")
            }

            goldenFile := "testdata/" + name + ".golden"
            if *update {
                os.WriteFile(goldenFile, []byte(out.String()), 0644)
                return
            }

            expected, err := os.ReadFile(goldenFile)
            require.NoError(t, err)
            assert.Equal(t, string(expected), out.String())
        })
    }
}
```

Use an `-update` flag to regenerate golden files:

```go
var update = flag.Bool("update", false, "update golden files")
```

Run `go test -update` to refresh all golden files after intentional formatting changes.

## Map Ordering in Tests

Gob encodes maps in an unspecified key order. When asserting on decoded `MapValue` contents, either sort the entries before comparison or use testify's `ElementsMatch` for unordered comparison:

```go
assert.ElementsMatch(t, expectedEntries, mapVal.Entries)
```

Alternatively, the golden file tests can sort map entries lexicographically in the formatter before writing output, making golden files deterministic.
