// Copyright 2013 The Zenio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package sp implements the wire format used for Scalable Protocols.
//
// The original implementation of Scalable Protocols can be found in nanomsg.
// For more information, see: http://nanomsg.org/
package sp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"syscall"

	"github.com/op/zenio/protocol"
)

const maxInt = int(^uint(0) >> 1)

type Negotiator struct{}

// Upgrade performs the handshake as defined by the Scalable Protocols and
// upgrades the given r and w to a reader and writer capable of communicating
// with the other node using Scalable Protocols.
func (h *Negotiator) Upgrade(r io.Reader, w io.Writer) (protocol.Reader, protocol.Writer, error) {
	// TODO make asymmetric and react to what's beeing received.
	if err := h.send(w); err != nil {
		return nil, nil, err
	}
	// FIXME HACK
	if err := protocol.Flush(w); err != nil {
		return nil, nil, err
	}
	if err := h.recv(r); err != nil {
		return nil, nil, err
	}
	return newReader(r), newWriter(w), nil
}

func (h *Negotiator) recv(r io.Reader) error {
	var buf [8]byte
	if _, err := io.ReadFull(r, buf[0:8]); err != nil {
		return err
	}
	if !bytes.Equal(buf[0:4], []byte{0x00, 'S', 'P', 0x00}) {
		return errors.New("zenio: invalid header")
	}
	_ = buf[4:6] // TODO what should we do with endpoint?
	if !bytes.Equal(buf[6:8], []byte{0x00, 0x00}) {
		return errors.New("zenio: non-zero reserved bytes found")
	}
	return nil
}

func (h *Negotiator) send(w io.Writer) error {
	// TODO investigate endpoint data
	var buf = [8]byte{
		1: 'S',
		2: 'P',
		3: 0x00, // TODO rfc says 0x01?
		5: 0x10,
	}
	if n, err := w.Write(buf[:]); err != nil {
		return err
	} else if n != 8 {
		return errors.New("zenio: failed to write sp header")
	}
	return nil
}

// TODO come up with a better name (FrameReader, Decoder?)
// reader reads the wire format used for Scalable Protocols.
type reader struct {
	rd     io.Reader
	buf    [8]byte
	broken bool
}

func newReader(rd io.Reader) *reader {
	return &reader{rd: rd}
}

// length reads and returns the length of the payload which is expected to be
// received.
func (r *reader) length() (int, error) {
	if _, err := io.ReadFull(r.rd, r.buf[:]); err != nil {
		return 0, err
	}
	length := binary.BigEndian.Uint64(r.buf[:])
	if length > uint64(maxInt) {
		return 0, syscall.EFBIG
	}
	return int(length), nil
}

// Read reads the payload data into p.
func (r *reader) Read() (protocol.Message, error) {
	length, err := r.length()
	if err != nil {
		r.broken = true
		return nil, err
	}
	lr := &io.LimitedReader{r.rd, int64(length)}
	return &singleFrame{lr: lr}, err
}

// A writer writes the wire format used for Scalable Protocols.
type writer struct {
	wr     io.Writer
	buf    [8]byte
	broken bool
}

func newWriter(wr io.Writer) *writer {
	return &writer{wr: wr}
}

// Write writes the payload.
func (w *writer) Write(src protocol.Message) error {
	frame, err := src.Next()
	if err != nil {
		return err
	}
	if src.More() {
		panic("Unsupported multiple frames")
	}

	binary.BigEndian.PutUint64(w.buf[:], uint64(frame.Len()))
	if written, err := w.wr.Write(w.buf[:]); err != nil {
		w.broken = true
		return err
	} else if written != 8 {
		w.broken = true
		return errors.New("zenio: failed to write frame size")
	}

	if _, err := io.Copy(w.wr, frame); err != nil {
		w.broken = true
		return err
	}
	return nil
}

// singleFrame implements the protocol.Frame for Scalable Protocols, where
// there is no such thing as message framing.
type singleFrame struct {
	lr   *io.LimitedReader
	read bool
}

func (sf *singleFrame) Read(p []byte) (n int, err error) {
	return sf.lr.Read(p)
}

func (sf *singleFrame) Len() int {
	return int(sf.lr.N)
}

func (sf *singleFrame) More() bool {
	return !sf.read
}

func (sf *singleFrame) Next() (protocol.Frame, error) {
	if sf.read {
		// TODO return error
		panic("out of range")
	}
	sf.read = true
	return sf, nil
}
