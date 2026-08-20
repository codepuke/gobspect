package diff_test

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"log"

	"github.com/codepuke/gobspect"
	"github.com/codepuke/gobspect/diff"
)

// Example_diffValues decodes two versions of the same gob-encoded value and
// renders the structural differences between them.
func Example_diffValues() {
	// snippet:start diff-values
	type Config struct {
		Timeout int
		Retries int
		Hosts   []string
	}

	// decode is a small helper: gob-encode v, then decode it with gobspect.
	decode := func(v any) gobspect.Value {
		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(v); err != nil {
			log.Fatal(err)
		}
		values, err := gobspect.New().Stream(&buf).Collect()
		if err != nil {
			log.Fatal(err)
		}
		return values[0]
	}

	before := decode(Config{Timeout: 30, Hosts: []string{"a.example.com"}})
	after := decode(Config{Timeout: 60, Retries: 5,
		Hosts: []string{"a.example.com", "b.example.com"}})

	// Diff returns a Delta AST (nil when the two sides are equal).
	d := diff.Diff(before, after)
	if !diff.HasChanges(d) {
		fmt.Println("no changes")
		return
	}

	// Format renders the Delta as a +/- text diff.
	fmt.Print(diff.Format(d))
	// snippet:end

	// Output:
	// ~ Config {
	//   Timeout:
	//     - 30
	//     + 60
	//   Hosts:
	//     ~ slice {
	//       [1]:
	//         + "b.example.com"
	//     }
	//   Retries:
	//     + 5
	// }
}
