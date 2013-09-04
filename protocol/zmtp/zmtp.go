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

// ReadWriter wraps the Reader and Writer into one, making a composition which
// implements io.ReadWriter.
type ReadWriter struct {
	Reader
	Writer
}

// A Reader reads ZMTP data from a reader. First Read will also read header
// data.
type Reader struct {
	rd  io.Reader
	buf [8]byte

	setupDone bool
	peeked    bool
	length    int
	more      bool

	// Identity is used as an addressing mechanism in ZMTP. It will be populated
	// once Peek or Read has been called.
	Identity []byte
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

	if _, err := io.ReadFull(r.rd, r.buf[0:2]); err != nil {
		return err
	}

	length := uint8(r.buf[0]) - 1
	// skip buf[1] (idflags isn't used)

	if length > 0 {
		r.Identity = make([]byte, length)
		if _, err := io.ReadFull(r.rd, r.Identity); err != nil {
			return err
		}
	}

	r.setupDone = true

	return nil
}

// Len is populated once Peek has been called and indicates the number of bytes
// waiting to be read in the next call to Read.
func (r *Reader) Len() int {
	return r.length
}

// More is populated once Peek has been called and indicates that there is more
// frames to read for the same message.
//
// One ZMTP message might consist of multiple frames.
func (r *Reader) More() bool {
	return r.more
}

// Peek reads the length and flags of the payload which is to be read in
// the next call to Read.
func (r *Reader) Peek() error {
	if err := r.setup(); err != nil {
		return err
	} else if r.peeked == true {
		return nil
	}

	if _, err := io.ReadFull(r.rd, r.buf[0:1]); err != nil {
		return err
	}
	var length uint64
	if r.buf[0] == 0xff {
		if _, err := io.ReadFull(r.rd, r.buf[0:8]); err != nil {
			return err
		}
		length = binary.BigEndian.Uint64(r.buf[0:8])
	} else {
		length = uint64(r.buf[0])
	}

	if length == 0 {
		return errors.New("zenio: invalid frame header")
	} else if length-1 > uint64(maxInt) {
		return syscall.EFBIG
	}
	r.length = int(length - 1)

	// Handle flags in the frame
	if _, err := io.ReadFull(r.rd, r.buf[0:1]); err != nil {
		return err
	}
	var flags = r.buf[0]
	if flags > 1 {
		return errors.New(fmt.Sprintf("zenio: invalid flag: 0x%02x", flags))
	}
	r.more = (flags & frameFlagMore) == frameFlagMore
	r.peeked = true

	return nil
}

// Read reads the payload data into p.
func (r *Reader) Read(p []byte) (int, error) {
	if err := r.Peek(); err != nil {
		return 0, err
	}
	r.peeked = false

	// TODO share code with sp for the whole next block
	l := r.length
	if cap(p) < l {
		l = cap(p)
	}
	n, err := r.rd.Read(p[:l])
	if err != nil {
		return n, err
	}
	return r.flush(n, p)
}

// TODO copied from sp.go.
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

// Writer writes the data according to ZMTP. First write will initiate the
// format by writing header data.
type Writer struct {
	wr  io.Writer
	buf [10]byte

	setupDone bool
	more      bool

	// Identity is used as an addressing mechanism in ZMTP. If not specified, the
	// anonymous identity will be used. The maximum length is 254 bytes and should
	// not start with 0x00.
	//
	// This field is only relevant before the first call to Write.
	Identity []byte
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

	// The identity can be at max 254 bytes (1 byte for flags)
	if len(w.Identity) >= 0xff {
		return errIdentTooBig
	}

	w.buf[0] = uint8(len(w.Identity)) + 1
	w.buf[1] = 0
	if n, err := w.wr.Write(w.buf[0:2]); err != nil {
		return err
	} else if n != 2 {
		return errors.New("zenio: failed to write identity header")
	}

	// Write no identity if there's none (known as anonymous)
	if len(w.Identity) > 0 {
		if w.Identity[0] == 0 {
			return errIdentReserved
		}
		if n, err := w.wr.Write(w.Identity); err != nil {
			return err
		} else if n != len(w.Identity) {
			return errors.New("zenio: failed to write identity")
		}
	}

	w.setupDone = true

	return nil
}

// SetMore sets the flag which indicates that there are more frames to deliver
// after the next call to Write. A ZMTP message might consist of multiple
// frames.
func (w *Writer) SetMore(more bool) {
	w.more = more
}

// Write writes the payload. Use the More flag to control the behaviour of this
// method.
func (w *Writer) Write(p []byte) (int, error) {
	if err := w.setup(); err != nil {
		return 0, err
	}

	// Setup the length of the frame. If the length is < 0xff, only one
	// byte is needed. For bigger frames, the first byte is 0xff and the
	// next 8 bytes consists of the length.
	var size int
	var length = uint64(len(p)) + 1
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
	if w.more {
		flags |= frameFlagMore
	}
	w.buf[size] = flags
	size++

	if n, err := w.wr.Write(w.buf[0:size]); err != nil {
		return 0, err
	} else if n != size {
		return 0, errors.New("zenio: failed to write frame header")
	}

	return w.wr.Write(p)
}
