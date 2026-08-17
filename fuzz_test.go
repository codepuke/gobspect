// Fuzzing baseline: 2026-08-17. Ran 3h, no failures, 888 corpus entries.
package gobspect_test

import (
	"bytes"
	"testing"

	"github.com/codepuke/gobspect"
)

// FuzzDecode drives the stream decoder over arbitrary bytes. It asserts
// nothing: the property under test is that no input, however malformed, can
// make the decoder panic, hang, or exhaust memory.
//
// Every accessor that walks the stream is exercised, including the ones with
// their own traversal logic (Messages, Stats), and the whole set is repeated
// against an inspector configured with the corrupt-value recovery path
// enabled — resync after a bad message is the decoder's most delicate branch.
func FuzzDecode(f *testing.F) {
	for _, seed := range fuzzSeedCorpus(f, "testdata") {
		f.Add(seed)
	}
	f.Add(multiValueSeed(f))

	strict := gobspect.New()
	lenient := gobspect.New(
		gobspect.WithSkipCorruptValues(true),
		gobspect.WithReadLimit(1<<20),
		gobspect.WithTimeFormat("2006-01-02T15:04:05.999999999Z07:00"),
	)

	drain := func(ins *gobspect.Inspector, data []byte) {
		// Each accessor consumes the stream, so every pass needs a fresh one.
		ins.Stream(bytes.NewReader(data)).Collect() //nolint:errcheck
		ins.Stream(bytes.NewReader(data)).Schema()  //nolint:errcheck
		ins.Stream(bytes.NewReader(data)).Stats()   //nolint:errcheck

		for _, err := range ins.Stream(bytes.NewReader(data)).Values() { //nolint:errcheck
			_ = err
		}
		for msg, err := range ins.Stream(bytes.NewReader(data)).Messages() { //nolint:errcheck
			_, _ = msg, err
		}

		// Type-table accessors, including lookups for IDs the stream never
		// defined and for the reserved bootstrap range.
		s := ins.Stream(bytes.NewReader(data))
		s.Collect() //nolint:errcheck
		for _, ti := range s.Types() {
			s.TypeByID(ti.ID) //nolint:errcheck
		}
		for _, id := range []int{-1, 0, 1, 16, 23, 1 << 20} {
			s.TypeByID(id) //nolint:errcheck
		}
		_ = s.SkipCount()
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		drain(strict, data)
		drain(lenient, data)
	})
}
