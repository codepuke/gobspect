package gobspect_test

// Runnable examples for the gobspect API. Each example hosts one snippet
// region consumed by the documentation site.

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"log"

	gobspect "github.com/codepuke/gobspect"
)

// Example_streamValues demonstrates the core decoding loop: stream a gob
// payload and print each decoded value.
func Example_streamValues() {
	// snippet:start stream-values
	// Encode two values the usual way.
	type User struct {
		Name string
		Age  int
	}
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	enc.Encode(User{Name: "alice", Age: 30})
	enc.Encode(User{Name: "bob", Age: 25})

	// Decode the stream without the original type.
	ins := gobspect.New()
	for v, err := range ins.Stream(&buf).Values() {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(gobspect.Format(v))
	}
	// snippet:end

	// Output:
	// User{
	//   Name: "alice"
	//   Age: 30
	// }
	// User{
	//   Name: "bob"
	//   Age: 25
	// }
}

// Example_streamTypes demonstrates reading the type definitions collected
// while decoding a stream.
func Example_streamTypes() {
	// snippet:start stream-types
	type Point struct {
		X, Y int
	}
	var buf bytes.Buffer
	gob.NewEncoder(&buf).Encode(Point{X: 1, Y: 2})

	ins := gobspect.New()
	s := ins.Stream(&buf)
	if _, err := s.Collect(); err != nil {
		log.Fatal(err)
	}

	// s.Types() now holds every type definition the stream declared.
	for _, ti := range s.Types() {
		fmt.Printf("%s (%s)\n", ti.Name, ti.Kind)
		for _, f := range ti.Fields {
			fmt.Printf("  field %s\n", f.Name)
		}
	}
	// snippet:end

	// Output:
	// Point (struct)
	//   field X
	//   field Y
}

// Example_schemaExtract demonstrates extracting a Go-like schema from a
// stream without keeping the decoded values.
func Example_schemaExtract() {
	// snippet:start schema-extract
	type Customer struct {
		Name string
	}
	type Order struct {
		ID       int
		Customer Customer
		Tags     []string
	}
	var buf bytes.Buffer
	gob.NewEncoder(&buf).Encode(Order{
		ID:       7,
		Customer: Customer{Name: "alice"},
		Tags:     []string{"rush"},
	})

	ins := gobspect.New()
	schema, err := ins.Stream(&buf).Schema()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(schema.String())
	// snippet:end

	// Output:
	// type Customer struct {
	//   Name  string
	// }
	//
	// type Order struct {
	//   ID        int
	//   Customer  Customer
	//   Tags      []string
	// }
}

// Example_formatOptions demonstrates adjusting Format output with options.
func Example_formatOptions() {
	type Blob struct {
		Name string
		Data []byte
	}
	var buf bytes.Buffer
	gob.NewEncoder(&buf).Encode(Blob{Name: "img", Data: []byte{0xde, 0xad, 0xbe, 0xef}})

	ins := gobspect.New()
	values, err := ins.Stream(&buf).Collect()
	if err != nil {
		log.Fatal(err)
	}
	v := values[0]

	// snippet:start format-options
	// Default rendering.
	fmt.Println(gobspect.Format(v))

	// Wider indentation and base64-encoded byte slices.
	fmt.Println(gobspect.Format(v,
		gobspect.WithIndent("    "),
		gobspect.WithBytesFormat(gobspect.BytesBase64),
	))

	// Go-literal byte rendering.
	fmt.Println(gobspect.Format(v, gobspect.WithBytesFormat(gobspect.BytesLiteral)))
	// snippet:end

	// Output:
	// Blob{
	//   Name: "img"
	//   Data: deadbeef
	// }
	// Blob{
	//     Name: "img"
	//     Data: 3q2+7w==
	// }
	// Blob{
	//   Name: "img"
	//   Data: []byte{0xde, 0xad, 0xbe, 0xef}
	// }
}

// Example_redactOutput demonstrates hiding sensitive fields in formatted
// output by field name and by type name.
func Example_redactOutput() {
	// snippet:start redact-output
	type Login struct {
		Username string
		Password string
	}
	var buf bytes.Buffer
	gob.NewEncoder(&buf).Encode(Login{Username: "alice", Password: "hunter2"})

	ins := gobspect.New()
	values, err := ins.Stream(&buf).Collect()
	if err != nil {
		log.Fatal(err)
	}

	// Redact by field or map-key name.
	fmt.Println(gobspect.Format(values[0],
		gobspect.WithRedactKeys(gobspect.RedactConfig{Keys: []string{"Password"}}),
	))

	// Redact every value of a named type.
	fmt.Println(gobspect.Format(values[0],
		gobspect.WithRedactTypes(gobspect.RedactTypesConfig{Types: []string{"Login"}, TextLength: 3}),
	))
	// snippet:end

	// Output:
	// Login{
	//   Username: "alice"
	//   Password: *********
	// }
	// ***
}

// Example_toJSON demonstrates converting a decoded value to JSON.
func Example_toJSON() {
	// snippet:start to-json
	type Point struct {
		X, Y int
	}
	var buf bytes.Buffer
	gob.NewEncoder(&buf).Encode(Point{X: 3, Y: 7})

	ins := gobspect.New()
	values, err := ins.Stream(&buf).Collect()
	if err != nil {
		log.Fatal(err)
	}

	// Type IDs are scoped to the stream that produced the value; zero the
	// ID here so the JSON is reproducible.
	sv := values[0].(gobspect.StructValue)
	sv.GobTypeID = 0

	b, err := gobspect.ToJSONIndent(sv, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(b))
	// snippet:end

	// Output:
	// {
	//   "fields": [
	//     {
	//       "name": "X",
	//       "value": {
	//         "kind": "int",
	//         "v": 3
	//       }
	//     },
	//     {
	//       "name": "Y",
	//       "value": {
	//         "kind": "int",
	//         "v": 7
	//       }
	//     }
	//   ],
	//   "kind": "struct",
	//   "typeId": 0,
	//   "typeName": "Point"
	// }
}

// sessionToken is a GobEncoder type whose blob gobspect cannot decode on its
// own; Example_registerDecoder registers a custom decoder for it.
type sessionToken struct{ payload []byte }

func (t sessionToken) GobEncode() ([]byte, error) { return t.payload, nil }
func (t *sessionToken) GobDecode(b []byte) error {
	t.payload = append([]byte(nil), b...)
	return nil
}

// Example_registerDecoder demonstrates registering a custom decoder for an
// opaque GobEncoder type.
func Example_registerDecoder() {
	// snippet:start register-decoder
	// sessionToken implements GobEncoder; wrap it in an interface field so
	// its type name travels on the wire.
	type Envelope struct{ Token any }
	gob.RegisterName("sessionToken", sessionToken{})

	var buf bytes.Buffer
	gob.NewEncoder(&buf).Encode(Envelope{Token: sessionToken{payload: []byte("tok-12345")}})

	ins := gobspect.New()
	// The key is the type's name from the gob wire format.
	ins.RegisterDecoder("sessionToken", func(data []byte) (any, error) {
		return "session:" + string(data), nil
	})

	values, err := ins.Stream(&buf).Collect()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(gobspect.Format(values[0]))
	// snippet:end

	// Output:
	// Envelope{
	//   Token: (sessionToken) session:tok-12345
	// }
}
