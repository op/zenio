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
)

const maxInt = int(^uint(0) >> 1)

// ReadWriter wraps the Reader and Writer into one, making a composition which
// implements io.ReadWriter.
type ReadWriter struct {
	Reader
	Writer
}

// A Reader reads the wire format used for Scalable Protocols. First Read
// will also parse the header.
type Reader struct {
	rd  io.Reader
	buf [8]byte

	setupDone bool
	peeked    bool
	length    int
}

// NewReader returns a new Reader.
func NewReader(rd io.Reader) *Reader {
	return &Reader{rd: rd}
}

func (r *Reader) Init() error {
	return r.setup()
}

func (r *Reader) setup() error {
	if r.setupDone {
		return nil
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

// Len is populated once Peek has been called and indicates the number of bytes
// waiting to be read in the next call to Read.
func (r *Reader) Len() int {
	return r.length
}

// More is not implemented for SP and will always return false.
func (r *Reader) More() bool {
	return false
}

// Peek reads the length of the payload which is to be read in the next call to
// Read.
func (r *Reader) Peek() error {
	if err := r.setup(); err != nil {
		return err
	} else if r.peeked {
		return nil
	}

	if _, err := io.ReadFull(r.rd, r.buf[:]); err != nil {
		return err
	}

	length := binary.BigEndian.Uint64(r.buf[:])
	if length > uint64(maxInt) {
		return syscall.EFBIG
	}
	r.length = int(length)
	r.peeked = true
	return nil
}

// Read reads the payload data into p.
func (r *Reader) Read(p []byte) (int, error) {
	if err := r.Peek(); err != nil {
		return 0, err
	}
	r.peeked = false

	l := r.length
	if cap(p) < l {
		l = cap(p)
	}
	n, err := r.rd.Read(p[:l])
	if err != nil {
		return n, err
	}

	// If the provided buffer is too small, we need to flush all unread data or
	// the next call to Read will be in a weird state in case there's a call
	// again after the error.
	return r.flush(n, p)
}

func (r *Reader) flush(read int, p []byte) (int, error) {
	unread := r.length - read
	if unread > 0 {
		var flush [128]byte
		for unread > 0 {
			s := unread
			if s < len(flush) {
				s = len(flush)
			}
			fn, ferr := r.rd.Read(flush[:])
			if ferr != nil {
				break
			}
			unread -= fn
		}
		return read, syscall.EFBIG
	}
	return read, nil
}

// A Writer writes the wire format used for Scalable Protocols. First Write
// will initiate the format by writing header data.
type Writer struct {
	wr  io.Writer
	buf [8]byte

	setupDone bool
}

// NewWriter returns a new Writer.
func NewWriter(wr io.Writer) *Writer {
	return &Writer{wr: wr}
}

func (w *Writer) Init() error {
	return w.setup()
}

func (w *Writer) setup() error {
	if w.setupDone {
		return nil
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

// SetMore is not implemented for SP and will panic if tried to be set to true.
func (w *Writer) SetMore(more bool) {
	if more {
		panic("zenio: sp does not support multiple frames")
	}
}

// Write writes the payload.
func (w *Writer) Write(p []byte) (int, error) {
	if err := w.setup(); err != nil {
		return 0, err
	}
	length := uint64(len(p))
	binary.BigEndian.PutUint64(w.buf[:], length)
	if n, err := w.wr.Write(w.buf[:]); err != nil {
		return 0, err
	} else if n != 8 {
		return 0, errors.New("zenio: failed to write frame size")
	}
	return w.wr.Write(p)
}
