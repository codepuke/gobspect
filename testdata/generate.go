//go:build ignore

// generate.go produces gob-encoded fixture files used as fuzz seeds and
// regression baselines. Run with:
//
//	go generate ./testdata/...
//
// or directly:
//
//	go run testdata/generate.go
package main

import (
	"bytes"
	"encoding/gob"
	"math/big"
	"os"
	"time"
)

// ————————————————————————————————————————————————————————————————————————————
// Types used in fixtures. Must be exported for gob.

type SimpleStruct struct {
	A int
	B string
}

type NestedStruct struct {
	X SimpleStruct
	Y float64
}

type SparseStruct struct {
	A int
	B int
	C int
}

type IntSlice []int

type StringIntMap map[string]int

type IntArray [3]int

type Animal interface{}
type Dog struct {
	Name  string
	Breed string
}
type Cat struct {
	Name   string
	Indoor bool
}

type AnimalHolder struct {
	Pet Animal
}

func init() {
	gob.Register(Dog{})
	gob.Register(Cat{})
}

// ————————————————————————————————————————————————————————————————————————————

func main() {
	// Primitives
	generate("testdata/int_positive.gob", 42)
	generate("testdata/int_negative.gob", -42)
	generate("testdata/int_zero.gob", 0)
	generate("testdata/string_ascii.gob", "hello world")
	generate("testdata/string_unicode.gob", "日本語テスト")
	generate("testdata/bool_true.gob", true)
	generate("testdata/bool_false.gob", false)
	generate("testdata/float64.gob", 3.14159)
	generate("testdata/bytes_data.gob", []byte{0xDE, 0xAD, 0xBE, 0xEF})

	// Structs
	generate("testdata/struct_simple.gob", SimpleStruct{A: 1, B: "two"})
	generate("testdata/struct_nested.gob", NestedStruct{X: SimpleStruct{A: 1, B: "inner"}, Y: 2.5})
	generate("testdata/struct_sparse.gob", SparseStruct{A: 1, B: 0, C: 3}) // B omitted (zero)

	// Collections
	generate("testdata/slice_int.gob", IntSlice{1, 2, 3, 4, 5})
	generate("testdata/map_string_int.gob", StringIntMap{"foo": 1, "bar": 2})
	generate("testdata/array_int.gob", IntArray{10, 20, 30})

	// Interfaces
	generate("testdata/interface_dog.gob", AnimalHolder{Pet: Dog{Name: "Rex", Breed: "Shepherd"}})
	generate("testdata/interface_nil.gob", AnimalHolder{Pet: nil})

	// Opaque types (GobEncoder / BinaryMarshaler)
	generate("testdata/time_utc.gob", time.Date(2024, 6, 1, 12, 0, 0, 123456789, time.UTC))
	generate("testdata/time_tz.gob", time.Date(2024, 1, 15, 9, 30, 0, 0, time.FixedZone("CST", -6*3600)))
	generate("testdata/bigint_positive.gob", big.NewInt(123456789))
	generate("testdata/bigint_negative.gob", big.NewInt(-99999))
	generate("testdata/bigint_zero.gob", big.NewInt(0))
	generate("testdata/bigfloat.gob", big.NewFloat(3.14159265358979323846))
	generate("testdata/bigrat.gob", big.NewRat(355, 113))

	// Multi-value stream
	generateMulti("testdata/multi_value.gob",
		SimpleStruct{A: 1, B: "first"},
		SimpleStruct{A: 2, B: "second"},
		SimpleStruct{A: 3, B: "third"},
	)
}

func generate(path string, values ...any) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	for _, v := range values {
		if err := enc.Encode(v); err != nil {
			panic(path + ": " + err.Error())
		}
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		panic(err)
	}
}

func generateMulti(path string, values ...any) {
	generate(path, values...)
}
