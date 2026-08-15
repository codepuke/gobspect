package gobspect

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
)

// Stats is a population-level summary of a gob stream: message counts, byte
// consumption, type distribution, struct-field presence rates, and opaque
// decoder coverage. Obtain one with [Stream.Stats].
//
// A Stats describes the complete stream after Stream.Stats has drained it;
// partial stats from an aborted pass are not exposed.
type Stats struct {
	// TotalMessages counts every length-prefixed message in the stream
	// (type definitions + values).
	TotalMessages int
	// TotalBodyBytes is the sum of every message body's length in bytes
	// (excluding the length prefix itself). It is a close approximation of
	// "bytes on the wire" for the stream.
	TotalBodyBytes int64
	// TypeDefMessages counts messages that carry a type definition.
	TypeDefMessages int
	// ValueMessages counts messages whose top-level value decoded
	// successfully. Corrupt value messages skipped under
	// [WithSkipCorruptValues] appear in Skipped instead, so a message never
	// counts in both.
	ValueMessages int

	// ByType summarises value messages grouped by the top-level type ID.
	// Entries are sorted by ValueCount descending, then by Name ascending for
	// determinism. TypeInfo from the stream's registry is also included.
	ByType []TypeStats

	// DecodedOpaques is the number of opaque values (anywhere in the tree)
	// whose registered decoder returned a non-nil Decoded value.
	DecodedOpaques int
	// UndecodedOpaques is the number of opaque values (anywhere in the tree)
	// for which no decoder matched — their Decoded field is nil.
	UndecodedOpaques int

	// Skipped is the count of corrupt value messages silently skipped because
	// [WithSkipCorruptValues] was enabled. Zero in strict mode.
	Skipped int
}

// TypeStats describes how a single top-level type contributed to the stream.
type TypeStats struct {
	TypeID     int
	Name       string
	Kind       TypeKind
	ValueCount int
	// BodyBytes is the total body-byte count of all value messages of this
	// type (excluding length prefixes). Useful for spotting which types
	// dominate the stream.
	BodyBytes int64
	// FieldPresence counts the number of value messages of this type in
	// which each struct field was present (gob omits zero-valued fields).
	// Keyed by field name; non-struct types leave it nil.
	FieldPresence map[string]int
}

// Stats drains the remainder of the stream, accumulating per-type and
// per-field statistics. On error it returns (nil, err): a Stats always
// describes a completely drained stream, never an aborted pass. Value-level
// decode failures count toward Skipped when [WithSkipCorruptValues] is
// enabled and otherwise abort the pass.
//
// Stats consumes the stream — a subsequent call to Values or Messages will
// panic just as after a normal Collect.
func (s *Stream) Stats() (*Stats, error) {
	if s.consumed {
		panic("gobspect: Stats called on an already-consumed Stream")
	}
	s.consumed = true

	out := &Stats{}
	byID := make(map[int]*TypeStats)

	for {
		rawID, msgR, info, err := s.sd.nextRawMessage()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, s.sd.wrapErr(err)
		}
		out.TotalMessages++
		out.TotalBodyBytes += int64(info.BodyLen)

		// Mirror drainSeq: a message may pack several type definitions and
		// an optional trailing value. A packed message counts toward both
		// TypeDefMessages and ValueMessages.
		hasValue := true
		for rawID < 0 {
			out.TypeDefMessages++
			id, tdErr := typeDefID(rawID)
			if tdErr == nil {
				tdErr = s.sd.processTypeDef(id, msgR)
			}
			if tdErr != nil {
				return nil, s.sd.wrapErr(tdErr)
			}
			if msgR.Len() == 0 {
				hasValue = false
				break
			}
			rawID, err = decodeInt(msgR)
			if err != nil {
				return nil, s.sd.wrapErr(fmt.Errorf("gob: reading type ID: %w", err))
			}
		}
		if !hasValue {
			s.sd.advanceMessage()
			continue
		}

		valueIdx, valueStart := s.sd.msgIdx, s.sd.msgStart
		mr := &messageReader{cur: msgR}
		v, decErr := s.vd.decodeTopLevelValue(int(rawID), mr)
		if decErr == nil {
			if rem := srcRemaining(mr); rem > 0 {
				decErr = fmt.Errorf("gob: %d trailing bytes after value", rem)
			}
		}
		if decErr != nil {
			if s.skipCorrupt {
				s.skipCount++
				s.sd.advanceMessage()
				continue
			}
			return nil, s.sd.wrapErrAt(valueIdx, valueStart, decErr)
		}
		s.sd.advanceMessage()
		out.ValueMessages++

		typeID := topLevelTypeID(v)
		ts, ok := byID[typeID]
		if !ok {
			ts = &TypeStats{TypeID: typeID}
			if ti, found := s.TypeByID(typeID); found {
				ts.Name = ti.Name
				ts.Kind = ti.Kind
			}
			byID[typeID] = ts
		}
		ts.ValueCount++
		ts.BodyBytes += int64(info.BodyLen)

		accumulateFieldPresence(ts, v)
		accumulateOpaques(out, v)
	}

	out.finalize(byID, s)
	return out, nil
}

// finalize sorts ByType deterministically and records the skip counter.
func (st *Stats) finalize(byID map[int]*TypeStats, s *Stream) {
	st.ByType = make([]TypeStats, 0, len(byID))
	for _, ts := range byID {
		st.ByType = append(st.ByType, *ts)
	}
	slices.SortFunc(st.ByType, func(a, b TypeStats) int {
		if c := cmp.Compare(b.ValueCount, a.ValueCount); c != 0 {
			return c
		}
		return cmp.Compare(a.Name, b.Name)
	})
	st.Skipped = s.SkipCount()
}

// topLevelTypeID returns the effective type ID for a top-level value: the
// struct's GobTypeID, the slice/map/array wrapper's GobTypeID, the interface's
// inner type, or 0 for scalars/nil.
func topLevelTypeID(v Value) int {
	if iv, ok := v.(InterfaceValue); ok {
		v = iv.Value
	}
	return v.TypeID()
}

// accumulateFieldPresence credits each present field of a top-level struct
// value to the corresponding TypeStats counter.
func accumulateFieldPresence(ts *TypeStats, v Value) {
	if iv, ok := v.(InterfaceValue); ok {
		v = iv.Value
	}
	sv, ok := v.(StructValue)
	if !ok {
		return
	}
	if ts.FieldPresence == nil {
		ts.FieldPresence = map[string]int{}
	}
	// Count each field name at most once per value: a hostile type definition
	// can list the same name repeatedly, which would push presence above 100%.
	seen := make(map[string]bool, len(sv.Fields))
	for _, f := range sv.Fields {
		if seen[f.Name] {
			continue
		}
		seen[f.Name] = true
		ts.FieldPresence[f.Name]++
	}
}

// accumulateOpaques walks v recursively and updates DecodedOpaques /
// UndecodedOpaques counters.
func accumulateOpaques(out *Stats, v Value) {
	switch n := v.(type) {
	case OpaqueValue:
		if n.Decoded != nil {
			out.DecodedOpaques++
		} else {
			out.UndecodedOpaques++
		}
	case InterfaceValue:
		accumulateOpaques(out, n.Value)
	case StructValue:
		for _, f := range n.Fields {
			accumulateOpaques(out, f.Value)
		}
	case MapValue:
		for _, e := range n.Entries {
			accumulateOpaques(out, e.Key)
			accumulateOpaques(out, e.Value)
		}
	case SliceValue:
		for _, el := range n.Elems {
			accumulateOpaques(out, el)
		}
	case ArrayValue:
		for _, el := range n.Elems {
			accumulateOpaques(out, el)
		}
	}
}

// Format writes a human-readable summary of s to w. The output is a small
// number of lines — total message and byte counts, then a per-type table,
// then opaque coverage.
func (s *Stats) Format(w io.Writer) error {
	var b strings.Builder
	fmt.Fprintf(&b, "messages: %d (values: %d, type defs: %d)\n",
		s.TotalMessages, s.ValueMessages, s.TypeDefMessages)
	fmt.Fprintf(&b, "body bytes: %d\n", s.TotalBodyBytes)
	if s.Skipped > 0 {
		fmt.Fprintf(&b, "skipped corrupt values: %d\n", s.Skipped)
	}
	fmt.Fprintf(&b, "opaque values: %d decoded, %d undecoded\n", s.DecodedOpaques, s.UndecodedOpaques)

	if len(s.ByType) > 0 {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "by type:")
		for _, ts := range s.ByType {
			name := ts.Name
			if name == "" {
				name = fmt.Sprintf("type%d", ts.TypeID)
			}
			fmt.Fprintf(&b, "  %-24s  %6d values  %10d bytes  (%s)\n",
				name, ts.ValueCount, ts.BodyBytes, ts.Kind)
			if len(ts.FieldPresence) > 0 {
				names := make([]string, 0, len(ts.FieldPresence))
				for name := range ts.FieldPresence {
					names = append(names, name)
				}
				slices.Sort(names)
				for _, n := range names {
					count := ts.FieldPresence[n]
					pct := 0.0
					if ts.ValueCount > 0 {
						pct = 100.0 * float64(count) / float64(ts.ValueCount)
					}
					fmt.Fprintf(&b, "    %-22s  %6d (%5.1f%%)\n", n, count, pct)
				}
			}
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// JSON returns the stats as a JSON document. Convenient for downstream tools
// that want to aggregate stats across many files.
func (s *Stats) JSON() ([]byte, error) { return json.Marshal(s) }

// JSONIndent is like [Stats.JSON] but with indentation.
func (s *Stats) JSONIndent(prefix, indent string) ([]byte, error) {
	return json.MarshalIndent(s, prefix, indent)
}
