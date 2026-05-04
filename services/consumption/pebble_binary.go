package main

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Hand-rolled binary codec for Pebble stored values. Replaces JSON for the hot consumption path
// (CreateConsumptionRecord + MarkOutboxAsPublished) where reflection-driven encode/decode showed
// up as a top CPU hotspot under load. Field order MUST match between marshal and unmarshal;
// struct evolution requires a versioned format (none today since the on-disk store is rebuilt
// for benchmarks).

func putInt64(buf []byte, v int64) []byte {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], uint64(v))
	return append(buf, b[:]...)
}

func getInt64(b []byte, pos int) (int64, int, error) {
	if pos+8 > len(b) {
		return 0, 0, fmt.Errorf("binary: short int64 at %d", pos)
	}
	return int64(binary.LittleEndian.Uint64(b[pos:])), pos + 8, nil
}

func putFloat64(buf []byte, v float64) []byte {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], math.Float64bits(v))
	return append(buf, b[:]...)
}

func getFloat64(b []byte, pos int) (float64, int, error) {
	if pos+8 > len(b) {
		return 0, 0, fmt.Errorf("binary: short float64 at %d", pos)
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(b[pos:])), pos + 8, nil
}

func putString(buf []byte, s string) []byte {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(len(s)))
	buf = append(buf, b[:]...)
	return append(buf, s...)
}

func getString(b []byte, pos int) (string, int, error) {
	if pos+4 > len(b) {
		return "", 0, fmt.Errorf("binary: short string len at %d", pos)
	}
	n := int(binary.LittleEndian.Uint32(b[pos:]))
	pos += 4
	if pos+n > len(b) {
		return "", 0, fmt.Errorf("binary: short string body at %d (need %d)", pos, n)
	}
	return string(b[pos : pos+n]), pos + n, nil
}

func putBool(buf []byte, v bool) []byte {
	if v {
		return append(buf, 1)
	}
	return append(buf, 0)
}

func getBool(b []byte, pos int) (bool, int, error) {
	if pos >= len(b) {
		return false, 0, fmt.Errorf("binary: short bool at %d", pos)
	}
	return b[pos] != 0, pos + 1, nil
}

// storedConsumption: DeviceID, DebitMsat, FractionalMsat, Measure, PricePerUnitMsat, Unit, Timestamp, CreatedAt
func (s *storedConsumption) marshalBinary() []byte {
	sz := 4*8 + 3*4 + len(s.DeviceID) + len(s.Unit) + len(s.Timestamp)
	buf := make([]byte, 0, sz)
	buf = putString(buf, s.DeviceID)
	buf = putInt64(buf, s.DebitMsat)
	buf = putFloat64(buf, s.FractionalMsat)
	buf = putFloat64(buf, s.Measure)
	buf = putInt64(buf, s.PricePerUnitMsat)
	buf = putString(buf, s.Unit)
	buf = putString(buf, s.Timestamp)
	buf = putInt64(buf, s.CreatedAt)
	return buf
}

func (s *storedConsumption) unmarshalBinary(b []byte) error {
	var (
		pos int
		err error
	)
	if s.DeviceID, pos, err = getString(b, pos); err != nil {
		return err
	}
	if s.DebitMsat, pos, err = getInt64(b, pos); err != nil {
		return err
	}
	if s.FractionalMsat, pos, err = getFloat64(b, pos); err != nil {
		return err
	}
	if s.Measure, pos, err = getFloat64(b, pos); err != nil {
		return err
	}
	if s.PricePerUnitMsat, pos, err = getInt64(b, pos); err != nil {
		return err
	}
	if s.Unit, pos, err = getString(b, pos); err != nil {
		return err
	}
	if s.Timestamp, pos, err = getString(b, pos); err != nil {
		return err
	}
	if s.CreatedAt, _, err = getInt64(b, pos); err != nil {
		return err
	}
	return nil
}

// storedOutbox: Published, PublishedAt, Traceparent, CreatedAt
func (s *storedOutbox) marshalBinary() []byte {
	sz := 1 + 2*8 + 4 + len(s.Traceparent)
	buf := make([]byte, 0, sz)
	buf = putBool(buf, s.Published)
	buf = putInt64(buf, s.PublishedAt)
	buf = putString(buf, s.Traceparent)
	buf = putInt64(buf, s.CreatedAt)
	return buf
}

func (s *storedOutbox) unmarshalBinary(b []byte) error {
	var (
		pos int
		err error
	)
	if s.Published, pos, err = getBool(b, pos); err != nil {
		return err
	}
	if s.PublishedAt, pos, err = getInt64(b, pos); err != nil {
		return err
	}
	if s.Traceparent, pos, err = getString(b, pos); err != nil {
		return err
	}
	if s.CreatedAt, _, err = getInt64(b, pos); err != nil {
		return err
	}
	return nil
}
