package gobspect_test

// Runnable examples of plain encoding/gob usage. These host the shared
// cross-language snippet topics synced to codepuke.com; the snippet regions
// define the vocabulary mirrored by ports in other languages, so keep them
// idiomatic, minimal, and language-neutral in concept.
//
// The Point type used by several examples is declared in decode_test.go.

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"io"
	"log"
	"time"
)

// Animal is an interface used by Example_encodeInterface.
type Animal interface{ Sound() string }

// Dog is a concrete Animal registered with gob so its type name travels
// on the wire.
type Dog struct{ Name string }

// Sound implements Animal.
func (Dog) Sound() string { return "woof" }

// Pet holds an Animal through an interface-typed field.
type Pet struct{ Companion Animal }

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

func Example_nestedStruct() {
	// snippet:start nested-struct
	type Size struct{ W, H int }
	type Photo struct {
		Title string
		Dim   Size
	}

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(Photo{Title: "sunset", Dim: Size{W: 640, H: 480}}); err != nil {
		log.Fatal(err)
	}
	// snippet:end

	var p Photo
	if err := gob.NewDecoder(&buf).Decode(&p); err != nil {
		log.Fatal(err)
	}
	fmt.Println(p.Title, p.Dim.W, p.Dim.H)
	// Output: sunset 640 480
}

func Example_encodeSlice() {
	// snippet:start encode-slice
	primes := []int{2, 3, 5, 7, 11}

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(primes); err != nil {
		log.Fatal(err)
	}
	// snippet:end

	var decoded []int
	if err := gob.NewDecoder(&buf).Decode(&decoded); err != nil {
		log.Fatal(err)
	}
	fmt.Println(decoded)
	// Output: [2 3 5 7 11]
}

func Example_encodeMap() {
	// snippet:start encode-map
	counts := map[string]int{"apples": 3, "pears": 7}

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
	fmt.Println("apples:", decoded["apples"])
	fmt.Println("pears:", decoded["pears"])
	// Output:
	// apples: 3
	// pears: 7
}

func Example_interfaceValues() {
	// snippet:start interface-values
	// Register the concrete type so its name travels on the wire.
	gob.Register(Dog{})

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(Pet{Companion: Dog{Name: "Rex"}}); err != nil {
		log.Fatal(err)
	}
	// snippet:end

	var p Pet
	if err := gob.NewDecoder(&buf).Decode(&p); err != nil {
		log.Fatal(err)
	}
	fmt.Println(p.Companion.Sound())
	// Output: woof
}

func Example_streamMultipleValues() {
	// snippet:start stream-multiple-values
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	for _, p := range []Point{{1, 2}, {3, 4}, {5, 6}} {
		if err := enc.Encode(p); err != nil {
			log.Fatal(err)
		}
	}

	dec := gob.NewDecoder(&buf)
	for {
		var p Point
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			log.Fatal(err)
		}
		fmt.Println(p.X, p.Y)
	}
	// snippet:end

	// Output:
	// 1 2
	// 3 4
	// 5 6
}

func Example_timeValues() {
	// snippet:start time-values
	launch := time.Date(2024, time.March, 14, 15, 9, 26, 0, time.UTC)

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

	// Output: 2024-03-14T15:09:26Z
}
