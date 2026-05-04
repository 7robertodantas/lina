package main

import (
	"encoding/binary"
	"fmt"
)

// Hand-rolled binary codec for Pebble stored values. JSON's reflection path is allocation-heavy
// (~6 marshal/unmarshal ops per debit) and showed up as a top CPU hotspot under load. This codec
// uses fixed-width little-endian int64s and uint32-length-prefixed strings — no reflection, no
// field name overhead. Field order MUST match between marshal and unmarshal; struct evolution
// requires a versioned format (none today since the on-disk store is rebuilt for benchmarks).

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

// storedBalance: BalanceMsat, UpdatedAt
func (s *storedBalance) marshalBinary() []byte {
	buf := make([]byte, 0, 16)
	buf = putInt64(buf, s.BalanceMsat)
	buf = putInt64(buf, s.UpdatedAt)
	return buf
}

func (s *storedBalance) unmarshalBinary(b []byte) error {
	var (
		pos int
		err error
	)
	if s.BalanceMsat, pos, err = getInt64(b, pos); err != nil {
		return err
	}
	if s.UpdatedAt, _, err = getInt64(b, pos); err != nil {
		return err
	}
	return nil
}

// storedIdem: Kind, RequestHash, ResponseJSON, CreatedAt
func (s *storedIdem) marshalBinary() []byte {
	buf := make([]byte, 0, 12+len(s.Kind)+len(s.RequestHash)+len(s.ResponseJSON)+8)
	buf = putString(buf, s.Kind)
	buf = putString(buf, s.RequestHash)
	buf = putString(buf, s.ResponseJSON)
	buf = putInt64(buf, s.CreatedAt)
	return buf
}

func (s *storedIdem) unmarshalBinary(b []byte) error {
	var (
		pos int
		err error
	)
	if s.Kind, pos, err = getString(b, pos); err != nil {
		return err
	}
	if s.RequestHash, pos, err = getString(b, pos); err != nil {
		return err
	}
	if s.ResponseJSON, pos, err = getString(b, pos); err != nil {
		return err
	}
	if s.CreatedAt, _, err = getInt64(b, pos); err != nil {
		return err
	}
	return nil
}

// storedAuthorization: AuthorizationID, DeviceID, RequestID, GrantedMsat, RemainingMsat,
// ConsumedMsat, OverflowMsat, IssuedAt, ExpiresAt, Status, CreatedAt
func (s *storedAuthorization) marshalBinary() []byte {
	sz := 5*8 + 6*4 + len(s.AuthorizationID) + len(s.DeviceID) + len(s.RequestID) +
		len(s.IssuedAt) + len(s.ExpiresAt) + len(s.Status)
	buf := make([]byte, 0, sz)
	buf = putString(buf, s.AuthorizationID)
	buf = putString(buf, s.DeviceID)
	buf = putString(buf, s.RequestID)
	buf = putInt64(buf, s.GrantedMsat)
	buf = putInt64(buf, s.RemainingMsat)
	buf = putInt64(buf, s.ConsumedMsat)
	buf = putInt64(buf, s.OverflowMsat)
	buf = putString(buf, s.IssuedAt)
	buf = putString(buf, s.ExpiresAt)
	buf = putString(buf, s.Status)
	buf = putInt64(buf, s.CreatedAt)
	return buf
}

func (s *storedAuthorization) unmarshalBinary(b []byte) error {
	var (
		pos int
		err error
	)
	if s.AuthorizationID, pos, err = getString(b, pos); err != nil {
		return err
	}
	if s.DeviceID, pos, err = getString(b, pos); err != nil {
		return err
	}
	if s.RequestID, pos, err = getString(b, pos); err != nil {
		return err
	}
	if s.GrantedMsat, pos, err = getInt64(b, pos); err != nil {
		return err
	}
	if s.RemainingMsat, pos, err = getInt64(b, pos); err != nil {
		return err
	}
	if s.ConsumedMsat, pos, err = getInt64(b, pos); err != nil {
		return err
	}
	if s.OverflowMsat, pos, err = getInt64(b, pos); err != nil {
		return err
	}
	if s.IssuedAt, pos, err = getString(b, pos); err != nil {
		return err
	}
	if s.ExpiresAt, pos, err = getString(b, pos); err != nil {
		return err
	}
	if s.Status, pos, err = getString(b, pos); err != nil {
		return err
	}
	if s.CreatedAt, _, err = getInt64(b, pos); err != nil {
		return err
	}
	return nil
}

// storedLedgerEntry: EntryID, DeviceID, EntryType, AmountMsat, BalanceAfter, Reason, CorrelationID, CreatedAt
func (s *storedLedgerEntry) marshalBinary() []byte {
	sz := 3*8 + 5*4 + len(s.EntryID) + len(s.DeviceID) + len(s.EntryType) + len(s.Reason) + len(s.CorrelationID)
	buf := make([]byte, 0, sz)
	buf = putString(buf, s.EntryID)
	buf = putString(buf, s.DeviceID)
	buf = putString(buf, s.EntryType)
	buf = putInt64(buf, s.AmountMsat)
	buf = putInt64(buf, s.BalanceAfter)
	buf = putString(buf, s.Reason)
	buf = putString(buf, s.CorrelationID)
	buf = putInt64(buf, s.CreatedAt)
	return buf
}

func (s *storedLedgerEntry) unmarshalBinary(b []byte) error {
	var (
		pos int
		err error
	)
	if s.EntryID, pos, err = getString(b, pos); err != nil {
		return err
	}
	if s.DeviceID, pos, err = getString(b, pos); err != nil {
		return err
	}
	if s.EntryType, pos, err = getString(b, pos); err != nil {
		return err
	}
	if s.AmountMsat, pos, err = getInt64(b, pos); err != nil {
		return err
	}
	if s.BalanceAfter, pos, err = getInt64(b, pos); err != nil {
		return err
	}
	if s.Reason, pos, err = getString(b, pos); err != nil {
		return err
	}
	if s.CorrelationID, pos, err = getString(b, pos); err != nil {
		return err
	}
	if s.CreatedAt, _, err = getInt64(b, pos); err != nil {
		return err
	}
	return nil
}
