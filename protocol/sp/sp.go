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

// TODO come up with a better name (FrameReader, Decoder?)
// A Reader reads the wire format used for Scalable Protocols. First Read
// will also parse the header.
type Reader struct {
	rd  io.Reader
	buf [8]byte

	setupDone bool
	broken    bool
}

// NewReader returns a new Reader.
func NewReader(rd io.Reader) *Reader {
	return &Reader{rd: rd}
}

// TODO split up to support symmetrical handshake. Rename.
func (r *Reader) Init() error {
	err := r.setup()
	if err != nil {
		r.broken = true
	}
	return err
}

func (r *Reader) setup() error {
	if r.setupDone {
		return nil
	} else if r.broken {
		return errors.New("zenio: can't read from broken reader")
	}

	if _, err := io.ReadFull(r.rd, r.buf[0:8]); err != nil {
		return err
	}
	if !bytes.Equal(r.buf[0:4], []byte{0x00, 'S', 'P', 0x00}) {
		return errors.New("zenio: invalid header")
	}
	_ = r.buf[4:6] // TODO what should we do with endpoint?
	if !bytes.Equal(r.buf[6:8], []byte{0x00, 0x00}) {
		return errors.New("zenio: non-zero reserved bytes found")
	}
	r.setupDone = true

	return nil
}

// length reads and returns the length of the payload which is expected to be
// received.
func (r *Reader) length() (int, error) {
	if err := r.setup(); err != nil {
		return 0, err
	}

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
func (r *Reader) Read() (protocol.Message, error) {
	length, err := r.length()
	if err != nil {
		r.broken = true
		return nil, err
	}
	lr := &io.LimitedReader{r.rd, int64(length)}
	return &singleFrame{lr: lr}, err
}

// A Writer writes the wire format used for Scalable Protocols. First Write
// will initiate the format by writing header data.
type Writer struct {
	wr  io.Writer
	buf [8]byte

	setupDone bool
	broken    bool
}

// NewWriter returns a new Writer.
func NewWriter(wr io.Writer) *Writer {
	return &Writer{wr: wr}
}

func (w *Writer) Init() error {
	err := w.setup()
	if err != nil {
		w.broken = true
	}
	return err
}

func (w *Writer) setup() error {
	if w.setupDone {
		return nil
	} else if w.broken {
		return errors.New("zenio: can't write to broken writer")
	}
	// TODO investigate endpoint data
	var buf = [8]byte{
		1: 'S',
		2: 'P',
		3: 0x00, // TODO rfc says 0x01?
		5: 0x10,
	}
	if n, err := w.wr.Write(buf[:]); err != nil {
		return err
	} else if n != 8 {
		return errors.New("zenio: failed to write sp header")
	}
	w.setupDone = true

	return nil
}

// Write writes the payload.
func (w *Writer) Write(src protocol.Message) error {
	frame, err := src.Next()
	if err != nil {
		return err
	}
	if src.More() {
		panic("Unsupported multiple frames")
	} else if err := w.setup(); err != nil {
		w.broken = true
		return err
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
