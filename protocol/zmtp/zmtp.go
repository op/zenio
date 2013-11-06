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

// Package zmtp implements the wire format used in ZMTP, the ZeroMQ Message
// Transport Protocol.
//
// For more information, see: http://zmtp.org/
package zmtp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"syscall"

	"github.com/op/zenio/protocol"
)

var (
	errIdentReserved = errors.New("zmq: zmtp identities starting with 0x0 is reserved")
	errIdentTooBig   = errors.New("zenio: zmtp identitiy too big")
)

type Type int

const (
	REQ Type = 3
	REP      = 4
)

// Frame flags
const (
	frameFlagMore byte = (1 << 0)
)

const maxInt = int(^uint(0) >> 1)

type Negotiator struct {
	// Identity is used as an addressing mechanism in ZMTP. If not specified, the
	// anonymous identity will be used. The maximum length is 254 bytes and should
	// not start with 0x00.
	Identity []byte
}

// Upgrade performs the handshake as defined by ZMTP and upgrades the given r
// and w to a reader and writer capable of communicating with the other node
// using ZMTP.
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
	var buf [2]byte
	if _, err := io.ReadFull(r, buf[0:2]); err != nil {
		return err
	}

	length := uint8(buf[0]) - 1
	// skip buf[1] (idflags isn't used)

	if length > 0 {
		// TODO expose received identity?
		identity := make([]byte, length)
		if _, err := io.ReadFull(r, identity); err != nil {
			return err
		}
	}
	return nil
}

func (h *Negotiator) send(w io.Writer) error {
	// The identity can be at max 254 bytes (1 byte for flags)
	if len(h.Identity) >= 0xff {
		return errIdentTooBig
	}

	var buf [2]byte
	buf[0] = uint8(len(h.Identity)) + 1
	buf[1] = 0
	if n, err := w.Write(buf[0:2]); err != nil {
		return err
	} else if n != 2 {
		return errors.New("zenio: failed to write identity header")
	}

	// Write no identity if there's none (known as anonymous)
	if len(h.Identity) > 0 {
		if h.Identity[0] == 0 {
			return errIdentReserved
		}
		if n, err := w.Write(h.Identity); err != nil {
			return err
		} else if n != len(h.Identity) {
			return errors.New("zenio: failed to write identity")
		}
	}
	return nil
}

// reader reads ZMTP data from a reader.
type reader struct {
	rd     io.Reader
	buf    [10]byte
	broken bool
}

func newReader(rd io.Reader) *reader {
	return &reader{rd: rd}
}

func (r *reader) readFrame() (f *frame, more bool, err error) {
	if r.broken {
		return nil, false, errors.New("zenio: can't read from broken stream")
	}

	// We know that we need to read at least 2 bytes. The simple case is that we
	// read 1 byte for the length and 1 byte for flags. If the length is 0xff, we
	// need to read 8 additional bytes for the length.
	if _, err := io.ReadFull(r.rd, r.buf[0:2]); err != nil {
		return nil, false, err
	}
	var length uint64
	var flags uint8
	if r.buf[0] == 0xff {
		if _, err := io.ReadFull(r.rd, r.buf[2:10]); err != nil {
			return nil, false, err
		}
		length = binary.BigEndian.Uint64(r.buf[1:9])
		flags = r.buf[9]
	} else {
		length = uint64(r.buf[0])
		flags = r.buf[1]
	}

	// Length is always > 0 since the flag is also included in the size.
	if length == 0 {
		return nil, false, errors.New("zenio: invalid frame header")
	} else if length-1 > uint64(maxInt) {
		return nil, false, syscall.EFBIG
	}
	lr := &io.LimitedReader{r.rd, int64(length - 1)}
	frame := &frame{lr}

	// Handle flags in the frame
	if flags > 1 {
		return frame, false, fmt.Errorf("zenio: invalid flag: 0x%02x", flags)
	}
	more = (flags & frameFlagMore) == frameFlagMore
	return frame, more, nil
}

// Read reads the payload data into p.
func (r *reader) Read() (protocol.Message, error) {
	// FIXME make sure broken is populated correctly
	if r.broken {
		return nil, errors.New("zenio: can't read from broken stream")
	}
	return &frameReader{r: r, more: true}, nil
}

// writer writes the data according to ZMTP. First write will initiate the
// format by writing header data.
type writer struct {
	wr     io.Writer
	buf    [10]byte
	broken bool
}

func newWriter(wr io.Writer) *writer {
	return &writer{wr: wr}
}

// Write writes the payload.
func (w *writer) Write(src protocol.Message) error {
	for {
		frame, err := src.Next()
		if err = w.writeFrame(frame, src.More()); err != nil {
			w.broken = true
			return err
		}
		if !src.More() {
			break
		}
	}
	return nil
}

func (w *writer) writeFrame(frame protocol.Frame, more bool) error {
	// Setup the length of the frame. If the length is < 0xff, only one
	// byte is needed. For bigger frames, the first byte is 0xff and the
	// next 8 bytes consists of the length.
	var size int
	var length = uint64(frame.Len()) + 1
	if length < 0xff {
		w.buf[0] = byte(length)
		size = 1
	} else {
		w.buf[0] = 0xff
		binary.BigEndian.PutUint64(w.buf[1:9], length)
		size = 9
	}

	// Add flags for the frame.
	var flags = uint8(0)
	if more {
		flags |= frameFlagMore
	}
	w.buf[size] = flags
	size++

	if n, err := w.wr.Write(w.buf[0:size]); err != nil {
		return err
	} else if n != size {
		return errors.New("zenio: failed to write frame header")
	}

	// TODO can performance be improved if frame implemented WriteTo?
	if _, err := io.Copy(w.wr, frame); err != nil {
		return err
	}
	return nil
}

type frame struct {
	lr *io.LimitedReader
}

func (f *frame) Read(p []byte) (int, error) {
	return f.lr.Read(p)
}

func (f *frame) Len() int {
	return int(f.lr.N)
}

type frameReader struct {
	r    *reader
	last *frame
	more bool
}

func (fr *frameReader) More() bool {
	return fr.more
}

func (fr *frameReader) Next() (frame protocol.Frame, err error) {
	if fr.last != nil {
		if fr.last.Len() > 0 {
			panic("previous frame not fully consumed")
		} else if !fr.more {
			// TODO return io.EOF?
			panic("no more frames available")
		}
	}
	fr.last, fr.more, err = fr.r.readFrame()
	return fr.last, err
}
