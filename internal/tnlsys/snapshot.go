package tnlsys

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// IoctlRec is one ioctl `ip` made: what it passed and what came back.
// In, Out and Spill are hex. Out is absent when the call failed and wrote
// nothing; Spill is what the kernel wrote past the argument, absent for
// nothing.
type IoctlRec struct {
	Family int        `json:"family"`
	Req    string     `json:"req"`
	Name   string     `json:"name"`
	In     string     `json:"in"`
	Out    string     `json:"out,omitempty"`
	Spill  string     `json:"spill,omitempty"`
	Errno  unix.Errno `json:"errno,omitempty"`
}

// PropRec is one sysfs property: its content, or the errno opening or
// reading it failed with.
type PropRec struct {
	Dev   string     `json:"dev"`
	Prop  string     `json:"prop"`
	Value string     `json:"value,omitempty"`
	Errno unix.Errno `json:"errno,omitempty"`
}

// Run is the traced `ip` run a snapshot came from, kept so a capture can
// check the recording saw the run the capture did.
type Run struct {
	Argv   []string `json:"argv"`
	RC     int      `json:"rc"`
	Stdout string   `json:"stdout"`
	Stderr string   `json:"stderr"`
}

// Snapshot is a recorded Sys: the JSON sidecar a capture row carries.
// A question it holds no answer for is ErrNotRecorded.
type Snapshot struct {
	Ioctls []IoctlRec `json:"ioctls,omitempty"`
	Props  []PropRec  `json:"props,omitempty"`
	// Fds is nil when the walk was not recorded, and an empty slice
	// when it found nothing.
	Fds *[]TunFD `json:"tunfds,omitempty"`
	Run *Run     `json:"run,omitempty"`

	ioctls []ioctlEntry
}

// ioctlEntry is an IoctlRec decoded.
type ioctlEntry struct {
	family         int
	req            uint32
	name           string
	in, out, spill []byte
	errno          unix.Errno
}

// Load reads a snapshot file.
func Load(path string) (*Snapshot, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("tnlsys: %w", err)
	}
	return Parse(b)
}

// Parse decodes a snapshot, refusing unknown fields, bad hex and unknown
// requests, so a hand-edited sidecar fails loudly.
func Parse(b []byte) (*Snapshot, error) {
	var s Snapshot
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&s); err != nil {
		return nil, fmt.Errorf("tnlsys: snapshot: %w", err)
	}
	if d.More() {
		return nil, errors.New("tnlsys: snapshot: trailing data")
	}
	if err := s.decode(); err != nil {
		return nil, err
	}
	return &s, nil
}

func (s *Snapshot) decode() error {
	s.ioctls = make([]ioctlEntry, 0, len(s.Ioctls))
	for i, r := range s.Ioctls {
		e, err := decodeIoctl(r)
		if err != nil {
			return fmt.Errorf("tnlsys: snapshot ioctl %d: %w", i, err)
		}
		s.ioctls = append(s.ioctls, e)
	}
	return nil
}

func decodeIoctl(r IoctlRec) (ioctlEntry, error) {
	req, ok := reqByName(r.Req)
	if !ok {
		return ioctlEntry{}, fmt.Errorf("unknown request %q", r.Req)
	}
	e := ioctlEntry{family: r.Family, req: req, name: IfName(r.Name), errno: r.Errno}
	var err error
	if e.in, err = hex.DecodeString(r.In); err != nil {
		return ioctlEntry{}, fmt.Errorf("in: %w", err)
	}
	if e.out, err = hex.DecodeString(r.Out); err != nil {
		return ioctlEntry{}, fmt.Errorf("out: %w", err)
	}
	if e.spill, err = hex.DecodeString(r.Spill); err != nil {
		return ioctlEntry{}, fmt.Errorf("spill: %w", err)
	}
	if len(e.out) > len(e.in) {
		return ioctlEntry{}, fmt.Errorf("out is %d bytes, in %d", len(e.out), len(e.in))
	}
	return e, nil
}

// Marshal is the snapshot as the sidecar file holds it.
func (s *Snapshot) Marshal() ([]byte, error) {
	b, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		return nil, fmt.Errorf("tnlsys: snapshot: %w", err)
	}
	return append(b, '\n'), nil
}

// AddIoctl records one ioctl, as Trace and tests build a snapshot.
func (s *Snapshot) AddIoctl(family int, req uint32, name string, in, out, spill []byte, errno unix.Errno) {
	r := IoctlRec{
		Family: family, Req: ReqName(req), Name: IfName(name),
		In: hex.EncodeToString(in), Out: hex.EncodeToString(out),
		Spill: hex.EncodeToString(spill), Errno: errno,
	}
	s.Ioctls = append(s.Ioctls, r)
	s.ioctls = append(s.ioctls, ioctlEntry{
		family: family, req: req, name: r.Name,
		in: bytes.Clone(in), out: bytes.Clone(out), spill: bytes.Clone(spill), errno: errno,
	})
}

// Ioctl answers from the recording: the entry with the same family,
// request, name and input bytes.
func (s *Snapshot) Ioctl(family int, req uint32, name string, arg []byte) ([]byte, error) {
	name = IfName(name)
	for _, e := range s.ioctls {
		if e.family != family || e.req != req || e.name != name || !bytes.Equal(e.in, arg) {
			continue
		}
		copy(arg, e.out)
		if e.errno != 0 {
			return nil, e.errno
		}
		return bytes.Clone(e.spill), nil
	}
	return nil, fmt.Errorf("%w: %s %s family %d", ErrNotRecorded, ReqName(req), name, family)
}

// ReadProp answers from the recording.
func (s *Snapshot) ReadProp(dev, prop string) (string, error) {
	for _, p := range s.Props {
		if p.Dev != dev || p.Prop != prop {
			continue
		}
		if p.Errno != 0 {
			return "", p.Errno
		}
		return p.Value, nil
	}
	return "", fmt.Errorf("%w: prop %s/%s", ErrNotRecorded, dev, prop)
}

// TunFDs answers from the recording.
func (s *Snapshot) TunFDs() ([]TunFD, error) {
	if s.Fds == nil {
		return nil, fmt.Errorf("%w: tun descriptors", ErrNotRecorded)
	}
	return append([]TunFD(nil), (*s.Fds)...), nil
}

// TunProps are the sysfs files print_tuntap reads.
var TunProps = []string{"tun_flags", "owner", "group"}

// RecordProps reads TunProps of every tun device in devs through sys into
// s, failures as their errno. An entry with no tun_flags file (ENOENT, or
// ENOTDIR for a file such as bonding_masters) is not a tun device and is
// skipped: print_tuntap reads the files only for links of kind "tun".
func (s *Snapshot) RecordProps(sys Sys, devs []string) {
	for _, d := range devs {
		if _, err := sys.ReadProp(d, TunProps[0]); errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ENOTDIR) {
			continue
		}
		for _, p := range TunProps {
			v, err := sys.ReadProp(d, p)
			r := PropRec{Dev: d, Prop: p, Value: v}
			if err != nil {
				r.Value, r.Errno = "", errnoOf(err)
			}
			s.Props = append(s.Props, r)
		}
	}
}

// errnoOf is err's errno, EIO for an error that carries none.
func errnoOf(err error) unix.Errno {
	var e unix.Errno
	if errors.As(err, &e) {
		return e
	}
	return unix.EIO
}
