// Code generated by protoc-gen-validate. DO NOT EDIT.
// source: xtcppb.proto

package xtcppb

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/protobuf/types/known/anypb"
)

// ensure the imports are used
var (
	_ = bytes.MinRead
	_ = errors.New("")
	_ = fmt.Print
	_ = utf8.UTFMax
	_ = (*regexp.Regexp)(nil)
	_ = (*strings.Reader)(nil)
	_ = net.IPv4len
	_ = time.Duration(0)
	_ = (*url.URL)(nil)
	_ = (*mail.Address)(nil)
	_ = anypb.Any{}
	_ = sort.Sort
)

// Validate checks the field values on XtcpRecord with the rules defined in the
// proto definition for this message. If any rules are violated, the first
// error encountered is returned, or nil if there are no violations.
func (m *XtcpRecord) Validate() error {
	return m.validate(false)
}

// ValidateAll checks the field values on XtcpRecord with the rules defined in
// the proto definition for this message. If any rules are violated, the
// result is a list of violation errors wrapped in XtcpRecordMultiError, or
// nil if none found.
func (m *XtcpRecord) ValidateAll() error {
	return m.validate(true)
}

func (m *XtcpRecord) validate(all bool) error {
	if m == nil {
		return nil
	}

	var errors []error

	if all {
		switch v := interface{}(m.GetEpochTime()).(type) {
		case interface{ ValidateAll() error }:
			if err := v.ValidateAll(); err != nil {
				errors = append(errors, XtcpRecordValidationError{
					field:  "EpochTime",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		case interface{ Validate() error }:
			if err := v.Validate(); err != nil {
				errors = append(errors, XtcpRecordValidationError{
					field:  "EpochTime",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		}
	} else if v, ok := interface{}(m.GetEpochTime()).(interface{ Validate() error }); ok {
		if err := v.Validate(); err != nil {
			return XtcpRecordValidationError{
				field:  "EpochTime",
				reason: "embedded message failed validation",
				cause:  err,
			}
		}
	}

	// no validation rules for Hostname

	// no validation rules for Tag

	if all {
		switch v := interface{}(m.GetInetDiagMsg()).(type) {
		case interface{ ValidateAll() error }:
			if err := v.ValidateAll(); err != nil {
				errors = append(errors, XtcpRecordValidationError{
					field:  "InetDiagMsg",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		case interface{ Validate() error }:
			if err := v.Validate(); err != nil {
				errors = append(errors, XtcpRecordValidationError{
					field:  "InetDiagMsg",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		}
	} else if v, ok := interface{}(m.GetInetDiagMsg()).(interface{ Validate() error }); ok {
		if err := v.Validate(); err != nil {
			return XtcpRecordValidationError{
				field:  "InetDiagMsg",
				reason: "embedded message failed validation",
				cause:  err,
			}
		}
	}

	if all {
		switch v := interface{}(m.GetMemInfo()).(type) {
		case interface{ ValidateAll() error }:
			if err := v.ValidateAll(); err != nil {
				errors = append(errors, XtcpRecordValidationError{
					field:  "MemInfo",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		case interface{ Validate() error }:
			if err := v.Validate(); err != nil {
				errors = append(errors, XtcpRecordValidationError{
					field:  "MemInfo",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		}
	} else if v, ok := interface{}(m.GetMemInfo()).(interface{ Validate() error }); ok {
		if err := v.Validate(); err != nil {
			return XtcpRecordValidationError{
				field:  "MemInfo",
				reason: "embedded message failed validation",
				cause:  err,
			}
		}
	}

	if all {
		switch v := interface{}(m.GetTcpInfo()).(type) {
		case interface{ ValidateAll() error }:
			if err := v.ValidateAll(); err != nil {
				errors = append(errors, XtcpRecordValidationError{
					field:  "TcpInfo",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		case interface{ Validate() error }:
			if err := v.Validate(); err != nil {
				errors = append(errors, XtcpRecordValidationError{
					field:  "TcpInfo",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		}
	} else if v, ok := interface{}(m.GetTcpInfo()).(interface{ Validate() error }); ok {
		if err := v.Validate(); err != nil {
			return XtcpRecordValidationError{
				field:  "TcpInfo",
				reason: "embedded message failed validation",
				cause:  err,
			}
		}
	}

	// no validation rules for CongestionAlgorithmString

	// no validation rules for CongestionAlgorithmEnum

	// no validation rules for TypeOfService

	// no validation rules for TrafficClass

	if all {
		switch v := interface{}(m.GetSkMemInfo()).(type) {
		case interface{ ValidateAll() error }:
			if err := v.ValidateAll(); err != nil {
				errors = append(errors, XtcpRecordValidationError{
					field:  "SkMemInfo",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		case interface{ Validate() error }:
			if err := v.Validate(); err != nil {
				errors = append(errors, XtcpRecordValidationError{
					field:  "SkMemInfo",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		}
	} else if v, ok := interface{}(m.GetSkMemInfo()).(interface{ Validate() error }); ok {
		if err := v.Validate(); err != nil {
			return XtcpRecordValidationError{
				field:  "SkMemInfo",
				reason: "embedded message failed validation",
				cause:  err,
			}
		}
	}

	// no validation rules for ShutdownState

	if all {
		switch v := interface{}(m.GetVegasInfo()).(type) {
		case interface{ ValidateAll() error }:
			if err := v.ValidateAll(); err != nil {
				errors = append(errors, XtcpRecordValidationError{
					field:  "VegasInfo",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		case interface{ Validate() error }:
			if err := v.Validate(); err != nil {
				errors = append(errors, XtcpRecordValidationError{
					field:  "VegasInfo",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		}
	} else if v, ok := interface{}(m.GetVegasInfo()).(interface{ Validate() error }); ok {
		if err := v.Validate(); err != nil {
			return XtcpRecordValidationError{
				field:  "VegasInfo",
				reason: "embedded message failed validation",
				cause:  err,
			}
		}
	}

	if all {
		switch v := interface{}(m.GetDctcpInfo()).(type) {
		case interface{ ValidateAll() error }:
			if err := v.ValidateAll(); err != nil {
				errors = append(errors, XtcpRecordValidationError{
					field:  "DctcpInfo",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		case interface{ Validate() error }:
			if err := v.Validate(); err != nil {
				errors = append(errors, XtcpRecordValidationError{
					field:  "DctcpInfo",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		}
	} else if v, ok := interface{}(m.GetDctcpInfo()).(interface{ Validate() error }); ok {
		if err := v.Validate(); err != nil {
			return XtcpRecordValidationError{
				field:  "DctcpInfo",
				reason: "embedded message failed validation",
				cause:  err,
			}
		}
	}

	if all {
		switch v := interface{}(m.GetBbrInfo()).(type) {
		case interface{ ValidateAll() error }:
			if err := v.ValidateAll(); err != nil {
				errors = append(errors, XtcpRecordValidationError{
					field:  "BbrInfo",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		case interface{ Validate() error }:
			if err := v.Validate(); err != nil {
				errors = append(errors, XtcpRecordValidationError{
					field:  "BbrInfo",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		}
	} else if v, ok := interface{}(m.GetBbrInfo()).(interface{ Validate() error }); ok {
		if err := v.Validate(); err != nil {
			return XtcpRecordValidationError{
				field:  "BbrInfo",
				reason: "embedded message failed validation",
				cause:  err,
			}
		}
	}

	// no validation rules for ClassId

	// no validation rules for SockOpt

	// no validation rules for CGroup

	if len(errors) > 0 {
		return XtcpRecordMultiError(errors)
	}

	return nil
}

// XtcpRecordMultiError is an error wrapping multiple validation errors
// returned by XtcpRecord.ValidateAll() if the designated constraints aren't met.
type XtcpRecordMultiError []error

// Error returns a concatenation of all the error messages it wraps.
func (m XtcpRecordMultiError) Error() string {
	var msgs []string
	for _, err := range m {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// AllErrors returns a list of validation violation errors.
func (m XtcpRecordMultiError) AllErrors() []error { return m }

// XtcpRecordValidationError is the validation error returned by
// XtcpRecord.Validate if the designated constraints aren't met.
type XtcpRecordValidationError struct {
	field  string
	reason string
	cause  error
	key    bool
}

// Field function returns field value.
func (e XtcpRecordValidationError) Field() string { return e.field }

// Reason function returns reason value.
func (e XtcpRecordValidationError) Reason() string { return e.reason }

// Cause function returns cause value.
func (e XtcpRecordValidationError) Cause() error { return e.cause }

// Key function returns key value.
func (e XtcpRecordValidationError) Key() bool { return e.key }

// ErrorName returns error name.
func (e XtcpRecordValidationError) ErrorName() string { return "XtcpRecordValidationError" }

// Error satisfies the builtin error interface
func (e XtcpRecordValidationError) Error() string {
	cause := ""
	if e.cause != nil {
		cause = fmt.Sprintf(" | caused by: %v", e.cause)
	}

	key := ""
	if e.key {
		key = "key for "
	}

	return fmt.Sprintf(
		"invalid %sXtcpRecord.%s: %s%s",
		key,
		e.field,
		e.reason,
		cause)
}

var _ error = XtcpRecordValidationError{}

var _ interface {
	Field() string
	Reason() string
	Key() bool
	Cause() error
	ErrorName() string
} = XtcpRecordValidationError{}

// Validate checks the field values on Timespec64T with the rules defined in
// the proto definition for this message. If any rules are violated, the first
// error encountered is returned, or nil if there are no violations.
func (m *Timespec64T) Validate() error {
	return m.validate(false)
}

// ValidateAll checks the field values on Timespec64T with the rules defined in
// the proto definition for this message. If any rules are violated, the
// result is a list of violation errors wrapped in Timespec64TMultiError, or
// nil if none found.
func (m *Timespec64T) ValidateAll() error {
	return m.validate(true)
}

func (m *Timespec64T) validate(all bool) error {
	if m == nil {
		return nil
	}

	var errors []error

	// no validation rules for Sec

	// no validation rules for Nsec

	if len(errors) > 0 {
		return Timespec64TMultiError(errors)
	}

	return nil
}

// Timespec64TMultiError is an error wrapping multiple validation errors
// returned by Timespec64T.ValidateAll() if the designated constraints aren't met.
type Timespec64TMultiError []error

// Error returns a concatenation of all the error messages it wraps.
func (m Timespec64TMultiError) Error() string {
	var msgs []string
	for _, err := range m {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// AllErrors returns a list of validation violation errors.
func (m Timespec64TMultiError) AllErrors() []error { return m }

// Timespec64TValidationError is the validation error returned by
// Timespec64T.Validate if the designated constraints aren't met.
type Timespec64TValidationError struct {
	field  string
	reason string
	cause  error
	key    bool
}

// Field function returns field value.
func (e Timespec64TValidationError) Field() string { return e.field }

// Reason function returns reason value.
func (e Timespec64TValidationError) Reason() string { return e.reason }

// Cause function returns cause value.
func (e Timespec64TValidationError) Cause() error { return e.cause }

// Key function returns key value.
func (e Timespec64TValidationError) Key() bool { return e.key }

// ErrorName returns error name.
func (e Timespec64TValidationError) ErrorName() string { return "Timespec64TValidationError" }

// Error satisfies the builtin error interface
func (e Timespec64TValidationError) Error() string {
	cause := ""
	if e.cause != nil {
		cause = fmt.Sprintf(" | caused by: %v", e.cause)
	}

	key := ""
	if e.key {
		key = "key for "
	}

	return fmt.Sprintf(
		"invalid %sTimespec64T.%s: %s%s",
		key,
		e.field,
		e.reason,
		cause)
}

var _ error = Timespec64TValidationError{}

var _ interface {
	Field() string
	Reason() string
	Key() bool
	Cause() error
	ErrorName() string
} = Timespec64TValidationError{}

// Validate checks the field values on SocketID with the rules defined in the
// proto definition for this message. If any rules are violated, the first
// error encountered is returned, or nil if there are no violations.
func (m *SocketID) Validate() error {
	return m.validate(false)
}

// ValidateAll checks the field values on SocketID with the rules defined in
// the proto definition for this message. If any rules are violated, the
// result is a list of violation errors wrapped in SocketIDMultiError, or nil
// if none found.
func (m *SocketID) ValidateAll() error {
	return m.validate(true)
}

func (m *SocketID) validate(all bool) error {
	if m == nil {
		return nil
	}

	var errors []error

	// no validation rules for SourcePort

	// no validation rules for DestinationPort

	// no validation rules for Source

	// no validation rules for Destination

	// no validation rules for Interface

	// no validation rules for Cookie

	// no validation rules for DestAsn

	// no validation rules for NextHopAsn

	if len(errors) > 0 {
		return SocketIDMultiError(errors)
	}

	return nil
}

// SocketIDMultiError is an error wrapping multiple validation errors returned
// by SocketID.ValidateAll() if the designated constraints aren't met.
type SocketIDMultiError []error

// Error returns a concatenation of all the error messages it wraps.
func (m SocketIDMultiError) Error() string {
	var msgs []string
	for _, err := range m {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// AllErrors returns a list of validation violation errors.
func (m SocketIDMultiError) AllErrors() []error { return m }

// SocketIDValidationError is the validation error returned by
// SocketID.Validate if the designated constraints aren't met.
type SocketIDValidationError struct {
	field  string
	reason string
	cause  error
	key    bool
}

// Field function returns field value.
func (e SocketIDValidationError) Field() string { return e.field }

// Reason function returns reason value.
func (e SocketIDValidationError) Reason() string { return e.reason }

// Cause function returns cause value.
func (e SocketIDValidationError) Cause() error { return e.cause }

// Key function returns key value.
func (e SocketIDValidationError) Key() bool { return e.key }

// ErrorName returns error name.
func (e SocketIDValidationError) ErrorName() string { return "SocketIDValidationError" }

// Error satisfies the builtin error interface
func (e SocketIDValidationError) Error() string {
	cause := ""
	if e.cause != nil {
		cause = fmt.Sprintf(" | caused by: %v", e.cause)
	}

	key := ""
	if e.key {
		key = "key for "
	}

	return fmt.Sprintf(
		"invalid %sSocketID.%s: %s%s",
		key,
		e.field,
		e.reason,
		cause)
}

var _ error = SocketIDValidationError{}

var _ interface {
	Field() string
	Reason() string
	Key() bool
	Cause() error
	ErrorName() string
} = SocketIDValidationError{}

// Validate checks the field values on MemInfo with the rules defined in the
// proto definition for this message. If any rules are violated, the first
// error encountered is returned, or nil if there are no violations.
func (m *MemInfo) Validate() error {
	return m.validate(false)
}

// ValidateAll checks the field values on MemInfo with the rules defined in the
// proto definition for this message. If any rules are violated, the result is
// a list of violation errors wrapped in MemInfoMultiError, or nil if none found.
func (m *MemInfo) ValidateAll() error {
	return m.validate(true)
}

func (m *MemInfo) validate(all bool) error {
	if m == nil {
		return nil
	}

	var errors []error

	// no validation rules for Rmem

	// no validation rules for Wmem

	// no validation rules for Fmem

	// no validation rules for Tmem

	if len(errors) > 0 {
		return MemInfoMultiError(errors)
	}

	return nil
}

// MemInfoMultiError is an error wrapping multiple validation errors returned
// by MemInfo.ValidateAll() if the designated constraints aren't met.
type MemInfoMultiError []error

// Error returns a concatenation of all the error messages it wraps.
func (m MemInfoMultiError) Error() string {
	var msgs []string
	for _, err := range m {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// AllErrors returns a list of validation violation errors.
func (m MemInfoMultiError) AllErrors() []error { return m }

// MemInfoValidationError is the validation error returned by MemInfo.Validate
// if the designated constraints aren't met.
type MemInfoValidationError struct {
	field  string
	reason string
	cause  error
	key    bool
}

// Field function returns field value.
func (e MemInfoValidationError) Field() string { return e.field }

// Reason function returns reason value.
func (e MemInfoValidationError) Reason() string { return e.reason }

// Cause function returns cause value.
func (e MemInfoValidationError) Cause() error { return e.cause }

// Key function returns key value.
func (e MemInfoValidationError) Key() bool { return e.key }

// ErrorName returns error name.
func (e MemInfoValidationError) ErrorName() string { return "MemInfoValidationError" }

// Error satisfies the builtin error interface
func (e MemInfoValidationError) Error() string {
	cause := ""
	if e.cause != nil {
		cause = fmt.Sprintf(" | caused by: %v", e.cause)
	}

	key := ""
	if e.key {
		key = "key for "
	}

	return fmt.Sprintf(
		"invalid %sMemInfo.%s: %s%s",
		key,
		e.field,
		e.reason,
		cause)
}

var _ error = MemInfoValidationError{}

var _ interface {
	Field() string
	Reason() string
	Key() bool
	Cause() error
	ErrorName() string
} = MemInfoValidationError{}

// Validate checks the field values on SkMemInfo with the rules defined in the
// proto definition for this message. If any rules are violated, the first
// error encountered is returned, or nil if there are no violations.
func (m *SkMemInfo) Validate() error {
	return m.validate(false)
}

// ValidateAll checks the field values on SkMemInfo with the rules defined in
// the proto definition for this message. If any rules are violated, the
// result is a list of violation errors wrapped in SkMemInfoMultiError, or nil
// if none found.
func (m *SkMemInfo) ValidateAll() error {
	return m.validate(true)
}

func (m *SkMemInfo) validate(all bool) error {
	if m == nil {
		return nil
	}

	var errors []error

	// no validation rules for RmemAlloc

	// no validation rules for RcvBuf

	// no validation rules for WmemAlloc

	// no validation rules for SndBuf

	// no validation rules for FwdAlloc

	// no validation rules for WmemQueued

	// no validation rules for Optmem

	// no validation rules for Backlog

	// no validation rules for Drops

	if len(errors) > 0 {
		return SkMemInfoMultiError(errors)
	}

	return nil
}

// SkMemInfoMultiError is an error wrapping multiple validation errors returned
// by SkMemInfo.ValidateAll() if the designated constraints aren't met.
type SkMemInfoMultiError []error

// Error returns a concatenation of all the error messages it wraps.
func (m SkMemInfoMultiError) Error() string {
	var msgs []string
	for _, err := range m {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// AllErrors returns a list of validation violation errors.
func (m SkMemInfoMultiError) AllErrors() []error { return m }

// SkMemInfoValidationError is the validation error returned by
// SkMemInfo.Validate if the designated constraints aren't met.
type SkMemInfoValidationError struct {
	field  string
	reason string
	cause  error
	key    bool
}

// Field function returns field value.
func (e SkMemInfoValidationError) Field() string { return e.field }

// Reason function returns reason value.
func (e SkMemInfoValidationError) Reason() string { return e.reason }

// Cause function returns cause value.
func (e SkMemInfoValidationError) Cause() error { return e.cause }

// Key function returns key value.
func (e SkMemInfoValidationError) Key() bool { return e.key }

// ErrorName returns error name.
func (e SkMemInfoValidationError) ErrorName() string { return "SkMemInfoValidationError" }

// Error satisfies the builtin error interface
func (e SkMemInfoValidationError) Error() string {
	cause := ""
	if e.cause != nil {
		cause = fmt.Sprintf(" | caused by: %v", e.cause)
	}

	key := ""
	if e.key {
		key = "key for "
	}

	return fmt.Sprintf(
		"invalid %sSkMemInfo.%s: %s%s",
		key,
		e.field,
		e.reason,
		cause)
}

var _ error = SkMemInfoValidationError{}

var _ interface {
	Field() string
	Reason() string
	Key() bool
	Cause() error
	ErrorName() string
} = SkMemInfoValidationError{}

// Validate checks the field values on DctcpInfo with the rules defined in the
// proto definition for this message. If any rules are violated, the first
// error encountered is returned, or nil if there are no violations.
func (m *DctcpInfo) Validate() error {
	return m.validate(false)
}

// ValidateAll checks the field values on DctcpInfo with the rules defined in
// the proto definition for this message. If any rules are violated, the
// result is a list of violation errors wrapped in DctcpInfoMultiError, or nil
// if none found.
func (m *DctcpInfo) ValidateAll() error {
	return m.validate(true)
}

func (m *DctcpInfo) validate(all bool) error {
	if m == nil {
		return nil
	}

	var errors []error

	// no validation rules for Enabled

	// no validation rules for CeState

	// no validation rules for Alpha

	// no validation rules for AbEcn

	// no validation rules for AbTot

	if len(errors) > 0 {
		return DctcpInfoMultiError(errors)
	}

	return nil
}

// DctcpInfoMultiError is an error wrapping multiple validation errors returned
// by DctcpInfo.ValidateAll() if the designated constraints aren't met.
type DctcpInfoMultiError []error

// Error returns a concatenation of all the error messages it wraps.
func (m DctcpInfoMultiError) Error() string {
	var msgs []string
	for _, err := range m {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// AllErrors returns a list of validation violation errors.
func (m DctcpInfoMultiError) AllErrors() []error { return m }

// DctcpInfoValidationError is the validation error returned by
// DctcpInfo.Validate if the designated constraints aren't met.
type DctcpInfoValidationError struct {
	field  string
	reason string
	cause  error
	key    bool
}

// Field function returns field value.
func (e DctcpInfoValidationError) Field() string { return e.field }

// Reason function returns reason value.
func (e DctcpInfoValidationError) Reason() string { return e.reason }

// Cause function returns cause value.
func (e DctcpInfoValidationError) Cause() error { return e.cause }

// Key function returns key value.
func (e DctcpInfoValidationError) Key() bool { return e.key }

// ErrorName returns error name.
func (e DctcpInfoValidationError) ErrorName() string { return "DctcpInfoValidationError" }

// Error satisfies the builtin error interface
func (e DctcpInfoValidationError) Error() string {
	cause := ""
	if e.cause != nil {
		cause = fmt.Sprintf(" | caused by: %v", e.cause)
	}

	key := ""
	if e.key {
		key = "key for "
	}

	return fmt.Sprintf(
		"invalid %sDctcpInfo.%s: %s%s",
		key,
		e.field,
		e.reason,
		cause)
}

var _ error = DctcpInfoValidationError{}

var _ interface {
	Field() string
	Reason() string
	Key() bool
	Cause() error
	ErrorName() string
} = DctcpInfoValidationError{}

// Validate checks the field values on BbrInfo with the rules defined in the
// proto definition for this message. If any rules are violated, the first
// error encountered is returned, or nil if there are no violations.
func (m *BbrInfo) Validate() error {
	return m.validate(false)
}

// ValidateAll checks the field values on BbrInfo with the rules defined in the
// proto definition for this message. If any rules are violated, the result is
// a list of violation errors wrapped in BbrInfoMultiError, or nil if none found.
func (m *BbrInfo) ValidateAll() error {
	return m.validate(true)
}

func (m *BbrInfo) validate(all bool) error {
	if m == nil {
		return nil
	}

	var errors []error

	// no validation rules for BwLo

	// no validation rules for BwHi

	// no validation rules for MinRtt

	// no validation rules for PacingGain

	// no validation rules for CwndGain

	if len(errors) > 0 {
		return BbrInfoMultiError(errors)
	}

	return nil
}

// BbrInfoMultiError is an error wrapping multiple validation errors returned
// by BbrInfo.ValidateAll() if the designated constraints aren't met.
type BbrInfoMultiError []error

// Error returns a concatenation of all the error messages it wraps.
func (m BbrInfoMultiError) Error() string {
	var msgs []string
	for _, err := range m {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// AllErrors returns a list of validation violation errors.
func (m BbrInfoMultiError) AllErrors() []error { return m }

// BbrInfoValidationError is the validation error returned by BbrInfo.Validate
// if the designated constraints aren't met.
type BbrInfoValidationError struct {
	field  string
	reason string
	cause  error
	key    bool
}

// Field function returns field value.
func (e BbrInfoValidationError) Field() string { return e.field }

// Reason function returns reason value.
func (e BbrInfoValidationError) Reason() string { return e.reason }

// Cause function returns cause value.
func (e BbrInfoValidationError) Cause() error { return e.cause }

// Key function returns key value.
func (e BbrInfoValidationError) Key() bool { return e.key }

// ErrorName returns error name.
func (e BbrInfoValidationError) ErrorName() string { return "BbrInfoValidationError" }

// Error satisfies the builtin error interface
func (e BbrInfoValidationError) Error() string {
	cause := ""
	if e.cause != nil {
		cause = fmt.Sprintf(" | caused by: %v", e.cause)
	}

	key := ""
	if e.key {
		key = "key for "
	}

	return fmt.Sprintf(
		"invalid %sBbrInfo.%s: %s%s",
		key,
		e.field,
		e.reason,
		cause)
}

var _ error = BbrInfoValidationError{}

var _ interface {
	Field() string
	Reason() string
	Key() bool
	Cause() error
	ErrorName() string
} = BbrInfoValidationError{}

// Validate checks the field values on VegasInfo with the rules defined in the
// proto definition for this message. If any rules are violated, the first
// error encountered is returned, or nil if there are no violations.
func (m *VegasInfo) Validate() error {
	return m.validate(false)
}

// ValidateAll checks the field values on VegasInfo with the rules defined in
// the proto definition for this message. If any rules are violated, the
// result is a list of violation errors wrapped in VegasInfoMultiError, or nil
// if none found.
func (m *VegasInfo) ValidateAll() error {
	return m.validate(true)
}

func (m *VegasInfo) validate(all bool) error {
	if m == nil {
		return nil
	}

	var errors []error

	// no validation rules for Enabled

	// no validation rules for RttCnt

	// no validation rules for Rtt

	// no validation rules for MinRtt

	if len(errors) > 0 {
		return VegasInfoMultiError(errors)
	}

	return nil
}

// VegasInfoMultiError is an error wrapping multiple validation errors returned
// by VegasInfo.ValidateAll() if the designated constraints aren't met.
type VegasInfoMultiError []error

// Error returns a concatenation of all the error messages it wraps.
func (m VegasInfoMultiError) Error() string {
	var msgs []string
	for _, err := range m {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// AllErrors returns a list of validation violation errors.
func (m VegasInfoMultiError) AllErrors() []error { return m }

// VegasInfoValidationError is the validation error returned by
// VegasInfo.Validate if the designated constraints aren't met.
type VegasInfoValidationError struct {
	field  string
	reason string
	cause  error
	key    bool
}

// Field function returns field value.
func (e VegasInfoValidationError) Field() string { return e.field }

// Reason function returns reason value.
func (e VegasInfoValidationError) Reason() string { return e.reason }

// Cause function returns cause value.
func (e VegasInfoValidationError) Cause() error { return e.cause }

// Key function returns key value.
func (e VegasInfoValidationError) Key() bool { return e.key }

// ErrorName returns error name.
func (e VegasInfoValidationError) ErrorName() string { return "VegasInfoValidationError" }

// Error satisfies the builtin error interface
func (e VegasInfoValidationError) Error() string {
	cause := ""
	if e.cause != nil {
		cause = fmt.Sprintf(" | caused by: %v", e.cause)
	}

	key := ""
	if e.key {
		key = "key for "
	}

	return fmt.Sprintf(
		"invalid %sVegasInfo.%s: %s%s",
		key,
		e.field,
		e.reason,
		cause)
}

var _ error = VegasInfoValidationError{}

var _ interface {
	Field() string
	Reason() string
	Key() bool
	Cause() error
	ErrorName() string
} = VegasInfoValidationError{}

// Validate checks the field values on TcpInfo with the rules defined in the
// proto definition for this message. If any rules are violated, the first
// error encountered is returned, or nil if there are no violations.
func (m *TcpInfo) Validate() error {
	return m.validate(false)
}

// ValidateAll checks the field values on TcpInfo with the rules defined in the
// proto definition for this message. If any rules are violated, the result is
// a list of violation errors wrapped in TcpInfoMultiError, or nil if none found.
func (m *TcpInfo) ValidateAll() error {
	return m.validate(true)
}

func (m *TcpInfo) validate(all bool) error {
	if m == nil {
		return nil
	}

	var errors []error

	// no validation rules for State

	// no validation rules for CaState

	// no validation rules for Retransmits

	// no validation rules for Probes

	// no validation rules for Backoff

	// no validation rules for Options

	// no validation rules for SendScale

	// no validation rules for RcvScale

	// no validation rules for DeliveryRateAppLimited

	// no validation rules for FastOpenClientFailed

	// no validation rules for Rto

	// no validation rules for Ato

	// no validation rules for SndMss

	// no validation rules for RcvMss

	// no validation rules for Unacked

	// no validation rules for Sacked

	// no validation rules for Lost

	// no validation rules for Retrans

	// no validation rules for Fackets

	// no validation rules for LastDataSent

	// no validation rules for LastAckSent

	// no validation rules for LastDataRecv

	// no validation rules for LastAckRecv

	// no validation rules for Pmtu

	// no validation rules for RcvSsthresh

	// no validation rules for Rtt

	// no validation rules for RttVar

	// no validation rules for SndSsthresh

	// no validation rules for SndCwnd

	// no validation rules for AdvMss

	// no validation rules for Reordering

	// no validation rules for RcvRtt

	// no validation rules for RcvSpace

	// no validation rules for TotalRetrans

	// no validation rules for PacingRate

	// no validation rules for MaxPacingRate

	// no validation rules for BytesAcked

	// no validation rules for BytesReceived

	// no validation rules for SegsOut

	// no validation rules for SegsIn

	// no validation rules for NotSentBytes

	// no validation rules for MinRtt

	// no validation rules for DataSegsIn

	// no validation rules for DataSegsOut

	// no validation rules for DeliveryRate

	// no validation rules for BusyTime

	// no validation rules for RwndLimited

	// no validation rules for SndbufLimited

	// no validation rules for Delivered

	// no validation rules for DeliveredCe

	// no validation rules for BytesSent

	// no validation rules for BytesRetrans

	// no validation rules for DsackDups

	// no validation rules for ReordSeen

	// no validation rules for RcvOoopack

	// no validation rules for SndWnd

	// no validation rules for RcvWnd

	// no validation rules for Rehash

	// no validation rules for TotalRto

	// no validation rules for TotalRtoRecoveries

	// no validation rules for TotalRtoTime

	if len(errors) > 0 {
		return TcpInfoMultiError(errors)
	}

	return nil
}

// TcpInfoMultiError is an error wrapping multiple validation errors returned
// by TcpInfo.ValidateAll() if the designated constraints aren't met.
type TcpInfoMultiError []error

// Error returns a concatenation of all the error messages it wraps.
func (m TcpInfoMultiError) Error() string {
	var msgs []string
	for _, err := range m {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// AllErrors returns a list of validation violation errors.
func (m TcpInfoMultiError) AllErrors() []error { return m }

// TcpInfoValidationError is the validation error returned by TcpInfo.Validate
// if the designated constraints aren't met.
type TcpInfoValidationError struct {
	field  string
	reason string
	cause  error
	key    bool
}

// Field function returns field value.
func (e TcpInfoValidationError) Field() string { return e.field }

// Reason function returns reason value.
func (e TcpInfoValidationError) Reason() string { return e.reason }

// Cause function returns cause value.
func (e TcpInfoValidationError) Cause() error { return e.cause }

// Key function returns key value.
func (e TcpInfoValidationError) Key() bool { return e.key }

// ErrorName returns error name.
func (e TcpInfoValidationError) ErrorName() string { return "TcpInfoValidationError" }

// Error satisfies the builtin error interface
func (e TcpInfoValidationError) Error() string {
	cause := ""
	if e.cause != nil {
		cause = fmt.Sprintf(" | caused by: %v", e.cause)
	}

	key := ""
	if e.key {
		key = "key for "
	}

	return fmt.Sprintf(
		"invalid %sTcpInfo.%s: %s%s",
		key,
		e.field,
		e.reason,
		cause)
}

var _ error = TcpInfoValidationError{}

var _ interface {
	Field() string
	Reason() string
	Key() bool
	Cause() error
	ErrorName() string
} = TcpInfoValidationError{}

// Validate checks the field values on InetDiagMsg with the rules defined in
// the proto definition for this message. If any rules are violated, the first
// error encountered is returned, or nil if there are no violations.
func (m *InetDiagMsg) Validate() error {
	return m.validate(false)
}

// ValidateAll checks the field values on InetDiagMsg with the rules defined in
// the proto definition for this message. If any rules are violated, the
// result is a list of violation errors wrapped in InetDiagMsgMultiError, or
// nil if none found.
func (m *InetDiagMsg) ValidateAll() error {
	return m.validate(true)
}

func (m *InetDiagMsg) validate(all bool) error {
	if m == nil {
		return nil
	}

	var errors []error

	// no validation rules for Family

	// no validation rules for State

	// no validation rules for Timer

	// no validation rules for Retrans

	if all {
		switch v := interface{}(m.GetSocketID()).(type) {
		case interface{ ValidateAll() error }:
			if err := v.ValidateAll(); err != nil {
				errors = append(errors, InetDiagMsgValidationError{
					field:  "SocketID",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		case interface{ Validate() error }:
			if err := v.Validate(); err != nil {
				errors = append(errors, InetDiagMsgValidationError{
					field:  "SocketID",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		}
	} else if v, ok := interface{}(m.GetSocketID()).(interface{ Validate() error }); ok {
		if err := v.Validate(); err != nil {
			return InetDiagMsgValidationError{
				field:  "SocketID",
				reason: "embedded message failed validation",
				cause:  err,
			}
		}
	}

	// no validation rules for Expires

	// no validation rules for Rqueue

	// no validation rules for Wqueue

	// no validation rules for UID

	// no validation rules for Inode

	if len(errors) > 0 {
		return InetDiagMsgMultiError(errors)
	}

	return nil
}

// InetDiagMsgMultiError is an error wrapping multiple validation errors
// returned by InetDiagMsg.ValidateAll() if the designated constraints aren't met.
type InetDiagMsgMultiError []error

// Error returns a concatenation of all the error messages it wraps.
func (m InetDiagMsgMultiError) Error() string {
	var msgs []string
	for _, err := range m {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// AllErrors returns a list of validation violation errors.
func (m InetDiagMsgMultiError) AllErrors() []error { return m }

// InetDiagMsgValidationError is the validation error returned by
// InetDiagMsg.Validate if the designated constraints aren't met.
type InetDiagMsgValidationError struct {
	field  string
	reason string
	cause  error
	key    bool
}

// Field function returns field value.
func (e InetDiagMsgValidationError) Field() string { return e.field }

// Reason function returns reason value.
func (e InetDiagMsgValidationError) Reason() string { return e.reason }

// Cause function returns cause value.
func (e InetDiagMsgValidationError) Cause() error { return e.cause }

// Key function returns key value.
func (e InetDiagMsgValidationError) Key() bool { return e.key }

// ErrorName returns error name.
func (e InetDiagMsgValidationError) ErrorName() string { return "InetDiagMsgValidationError" }

// Error satisfies the builtin error interface
func (e InetDiagMsgValidationError) Error() string {
	cause := ""
	if e.cause != nil {
		cause = fmt.Sprintf(" | caused by: %v", e.cause)
	}

	key := ""
	if e.key {
		key = "key for "
	}

	return fmt.Sprintf(
		"invalid %sInetDiagMsg.%s: %s%s",
		key,
		e.field,
		e.reason,
		cause)
}

var _ error = InetDiagMsgValidationError{}

var _ interface {
	Field() string
	Reason() string
	Key() bool
	Cause() error
	ErrorName() string
} = InetDiagMsgValidationError{}