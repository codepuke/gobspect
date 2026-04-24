package gobspect

import (
	"io"
	"iter"
)

// Stream represents an in-progress decoding of a gob stream. Obtain one with
// [Inspector.Stream]. A Stream is consumed by calling [Stream.Values], which
// returns an iterator over top-level decoded values. Type definitions
// encountered during decoding are accumulated and accessible via [Stream.Types]
// at any point.
//
// A Stream is single-use: calling Values a second time panics. It is not safe
// for concurrent use.
//
// A Stream does not own its reader. The caller is responsible for closing the
// underlying io.Reader if needed.
type Stream struct {
	sd          *streamDecoder
	vd          *valueDecoder
	consumed    bool
	skipCorrupt bool
	skipCount   int
}

// Stream begins decoding the gob stream from r. Decoding is lazy: nothing is
// read from r until the iterator returned by Values is advanced, or a helper
// like Collect or Schema drains the stream. Calling Types before advancing
// returns an empty slice.
func (ins *Inspector) Stream(r io.Reader) *Stream {
	sd := newStreamDecoder(wrapWithLimit(r, ins.maxBytes))
	vd := newValueDecoder(ins, sd)
	return &Stream{sd: sd, vd: vd, skipCorrupt: ins.skipCorruptValues}
}

// drainSeq returns a fresh iteration sequence backed by sd.r. Each call
// produces a new closure that reads from wherever sd.r currently sits.
func (s *Stream) drainSeq() iter.Seq2[Value, error] {
	return func(yield func(Value, error) bool) {
		for {
			rawID, msgR, _, err := s.sd.nextRawMessage()
			if err == io.EOF {
				return
			}
			if err != nil {
				yield(nil, s.sd.wrapErr(err))
				return
			}
			if rawID < 0 {
				if err := s.sd.processTypeDef(int(-rawID), msgR); err != nil {
					yield(nil, s.sd.wrapErr(err))
					return
				}
				s.sd.advanceMessage()
			} else {
				v, err := s.vd.decodeTopLevelValue(int(rawID), &messageReader{cur: msgR})
				if err != nil {
					if s.skipCorrupt {
						s.skipCount++
						s.sd.advanceMessage()
						continue
					}
					yield(nil, s.sd.wrapErr(err))
					return
				}
				s.sd.advanceMessage()
				if !yield(v, nil) {
					return
				}
			}
		}
	}
}

// Messages returns an iterator that yields one [MessageInfo] per length-
// prefixed gob message in the stream, *without* decoding values. This is a
// cheap way to profile a stream (per-message byte count, type-ID distribution)
// or to locate message boundaries for tooling that processes the raw frames.
//
// The stream is consumed by Messages just like by Values: you cannot call
// Values on a stream after Messages has drained it, and you cannot call
// Messages twice.
//
// Unlike [Stream.Values], Messages does not register type definitions against
// the decoder's type registry. If you need both decoded values and per-
// message framing, use Values and call [Stream.TypeByID] / [Stream.Types] at
// each step, or read the underlying Stream.Messages separately on a second
// Inspector.Stream over a rewound reader.
func (s *Stream) Messages() iter.Seq2[MessageInfo, error] {
	if s.consumed {
		panic("gobspect: Messages called on an already-consumed Stream")
	}
	s.consumed = true
	return func(yield func(MessageInfo, error) bool) {
		for {
			_, _, info, err := s.sd.nextRawMessage()
			if err == io.EOF {
				return
			}
			if err != nil {
				yield(MessageInfo{}, s.sd.wrapErr(err))
				return
			}
			s.sd.advanceMessage()
			if !yield(info, nil) {
				return
			}
		}
	}
}

// Values returns an iterator yielding one decoded Value per top-level gob
// value in the stream. On error it yields (nil, err) and stops. Early break
// from the loop is safe; the iterator will not read past the break point.
//
// By the time a value v is yielded, every TypeInfo that v's type graph
// references has been appended to the slice returned by Types, and all
// TypeRef.Name fields within those types have been resolved against
// definitions seen so far. Callers may call Types() from inside the loop
// body to look up the type definition for the value just received.
//
// A Stream is single-use. Calling Values on an already-consumed Stream panics.
func (s *Stream) Values() iter.Seq2[Value, error] {
	if s.consumed {
		panic("gobspect: Values called on an already-consumed Stream")
	}
	s.consumed = true
	return s.drainSeq()
}

// Types returns the live slice of type definitions encountered so far, in the
// order they appeared on the wire. The slice grows as the stream is consumed;
// callers iterating Values may call Types() at each step to see types defined
// up to and including the value just yielded.
//
// TypeRef.Name fields are resolved incrementally as new type definitions
// arrive. When a type references another type that hasn't been seen yet, the
// referring TypeRef.Name is initially empty; it is filled in as soon as the
// referenced type's definition is processed.
//
// The returned slice is owned by the Stream and must not be mutated. If a
// stable snapshot is needed, the caller should copy it with slices.Clone.
func (s *Stream) Types() []TypeInfo {
	return s.sd.types
}

// SkipCount reports the number of value messages silently skipped so far
// because [WithSkipCorruptValues] is enabled and they failed to decode.
// When WithSkipCorruptValues is disabled this is always zero.
func (s *Stream) SkipCount() int { return s.skipCount }

// TypeByID returns the TypeInfo for the given stream-scoped type ID, if known.
func (s *Stream) TypeByID(id int) (TypeInfo, bool) {
	idx, ok := s.sd.byID[id]
	if !ok {
		return TypeInfo{}, false
	}
	return s.sd.types[idx], true
}

// Collect drains the remainder of the stream and returns all decoded values.
// If Values has been partially consumed, Collect returns only the values that
// had not yet been yielded. On error, returns the values collected before the
// error alongside it.
func (s *Stream) Collect() ([]Value, error) {
	s.consumed = true
	var values []Value
	for v, err := range s.drainSeq() {
		if err != nil {
			return values, err
		}
		values = append(values, v)
	}
	return values, nil
}

// Schema drains the remainder of the stream and returns a formatted Schema
// over every type definition encountered. All value messages are decoded and
// discarded; only type information survives. Returns the partial schema
// alongside any error.
//
// This implementation decodes value bodies fully and throws them away. It is
// not optimized for the types-only case.
func (s *Stream) Schema() (*Schema, error) {
	_, err := s.Collect()
	return FormatSchema(s.sd.types), err
}
