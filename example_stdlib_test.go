package gobspect_test

// Runnable examples of plain encoding/gob usage. These host the shared
// cross-language snippet topics synced to codepuke.com; the snippet regions
// define the vocabulary mirrored by ports in other languages, so keep them
// idiomatic, minimal, and language-neutral in concept. The canonical data is
// shared across ports: the running struct is Point{X: 3, Y: 4}, and interface
// examples register the concrete type under the wire name "main.Point".
//
// The Point type used by several examples is declared in decode_test.go.

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"log"
	"time"
)

func Example_encodeStruct() {
	// snippet:start encode-struct
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(Point{X: 3, Y: 4}); err != nil {
		log.Fatal(err)
	}
	// snippet:end
	fmt.Println("encoded:", buf.Len() > 0)

	// Output: encoded: true
}

func Example_decodeStruct() {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(Point{X: 3, Y: 4}); err != nil {
		log.Fatal(err)
	}

	// snippet:start decode-struct
	var p Point
	dec := gob.NewDecoder(&buf)
	if err := dec.Decode(&p); err != nil {
		log.Fatal(err)
	}
	fmt.Println("X =", p.X, "Y =", p.Y)
	// snippet:end

	// Output: X = 3 Y = 4
}

func Example_encodeScalars() {
	// snippet:start encode-scalars
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)

	// Every scalar kind encodes the same way: hand the value to Encode.
	if err := enc.Encode(42); err != nil {
		log.Fatal(err)
	}
	if err := enc.Encode("hello"); err != nil {
		log.Fatal(err)
	}
	if err := enc.Encode(true); err != nil {
		log.Fatal(err)
	}

	dec := gob.NewDecoder(&buf)
	var (
		n  int
		s  string
		ok bool
	)
	if err := dec.Decode(&n); err != nil {
		log.Fatal(err)
	}
	if err := dec.Decode(&s); err != nil {
		log.Fatal(err)
	}
	if err := dec.Decode(&ok); err != nil {
		log.Fatal(err)
	}
	fmt.Println(n, s, ok)
	// snippet:end

	// Output: 42 hello true
}

func Example_nestedStruct() {
	// snippet:start nested-struct
	type Line struct {
		From, To Point
	}

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(Line{From: Point{X: 1, Y: 2}, To: Point{X: 3, Y: 4}}); err != nil {
		log.Fatal(err)
	}

	var l Line
	if err := gob.NewDecoder(&buf).Decode(&l); err != nil {
		log.Fatal(err)
	}
	fmt.Println("To.X =", l.To.X)
	// snippet:end

	// Output: To.X = 3
}

func Example_encodeSlice() {
	// snippet:start encode-slice
	nums := []int{1, 2, 3}

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(nums); err != nil {
		log.Fatal(err)
	}
	// snippet:end

	var decoded []int
	if err := gob.NewDecoder(&buf).Decode(&decoded); err != nil {
		log.Fatal(err)
	}
	fmt.Println(decoded)
	// Output: [1 2 3]
}

func Example_encodeMap() {
	// snippet:start encode-map
	counts := map[string]int{"one": 1, "two": 2}

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(counts); err != nil {
		log.Fatal(err)
	}
	// snippet:end

	var decoded map[string]int
	if err := gob.NewDecoder(&buf).Decode(&decoded); err != nil {
		log.Fatal(err)
	}
	fmt.Println("one:", decoded["one"])
	fmt.Println("two:", decoded["two"])
	// Output:
	// one: 1
	// two: 2
}

func Example_interfaceValues() {
	// The package-level Point is registered under its default name by
	// value_test.go, and gob allows only one name per type, so this example
	// declares its own Point to register under the canonical cross-port
	// wire name "main.Point".
	type Point struct {
		X, Y int
	}

	// snippet:start interface-values
	type Box struct {
		Value any
	}

	// Register the concrete type under an explicit name so both sides of
	// the wire agree on it.
	gob.RegisterName("main.Point", Point{})

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(Box{Value: Point{X: 3, Y: 4}}); err != nil {
		log.Fatal(err)
	}

	var b Box
	if err := gob.NewDecoder(&buf).Decode(&b); err != nil {
		log.Fatal(err)
	}
	p := b.Value.(Point)
	fmt.Println(p.X, p.Y)
	// snippet:end

	// Output: 3 4
}

func Example_streamMultipleValues() {
	// snippet:start stream-multiple-values
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)

	// The encoder sends Point's type definition once, ahead of the first
	// value; later values reuse it.
	if err := enc.Encode(Point{X: 3, Y: 4}); err != nil {
		log.Fatal(err)
	}
	if err := enc.Encode(Point{X: 5, Y: 6}); err != nil {
		log.Fatal(err)
	}

	dec := gob.NewDecoder(&buf)
	var first, second Point
	if err := dec.Decode(&first); err != nil {
		log.Fatal(err)
	}
	if err := dec.Decode(&second); err != nil {
		log.Fatal(err)
	}
	fmt.Println(first.X, first.Y)
	fmt.Println(second.X, second.Y)
	// snippet:end

	// Output:
	// 3 4
	// 5 6
}

func Example_endOfStream() {
	// snippet:start end-of-stream
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	for _, p := range []Point{{3, 4}, {5, 6}} {
		if err := enc.Encode(p); err != nil {
			log.Fatal(err)
		}
	}

	// Decode until the stream runs out; io.EOF is the normal end signal.
	dec := gob.NewDecoder(&buf)
	for {
		var p Point
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			log.Fatal(err)
		}
		fmt.Println(p.X, p.Y)
	}
	// snippet:end

	// Output:
	// 3 4
	// 5 6
}

func Example_zeroFieldsOmitted() {
	// snippet:start zero-fields-omitted
	var full, partial bytes.Buffer
	if err := gob.NewEncoder(&full).Encode(Point{X: 3, Y: 4}); err != nil {
		log.Fatal(err)
	}
	if err := gob.NewEncoder(&partial).Encode(Point{X: 3, Y: 0}); err != nil {
		log.Fatal(err)
	}

	// The zero-valued Y is absent from the wire, so the partial encoding
	// is shorter.
	fmt.Println("partial is shorter:", partial.Len() < full.Len())

	// The decoder restores omitted fields to their zero values.
	var p Point
	if err := gob.NewDecoder(&partial).Decode(&p); err != nil {
		log.Fatal(err)
	}
	fmt.Println("X =", p.X, "Y =", p.Y)
	// snippet:end

	// Output:
	// partial is shorter: true
	// X = 3 Y = 0
}

func Example_timeValues() {
	// snippet:start time-values
	launch := time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC)

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(launch); err != nil {
		log.Fatal(err)
	}

	var decoded time.Time
	if err := gob.NewDecoder(&buf).Decode(&decoded); err != nil {
		log.Fatal(err)
	}
	fmt.Println(decoded.Format(time.RFC3339))
	// snippet:end

	// Output: 2009-11-10T23:00:00Z
}
