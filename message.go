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

package zenio

import (
	"bytes"
	"io"

	"github.com/op/zenio/protocol"
)

type BytesMessage struct {
	buf [][]byte
	idx int
}

func NewBytesMessage(frames [][]byte) *BytesMessage {
	return &BytesMessage{frames, 0}
}

func (b *BytesMessage) Reset() {
	b.idx = 0
}

func (b *BytesMessage) More() bool {
	return b.idx < len(b.buf)
}

func (b *BytesMessage) Next() (protocol.Frame, error) {
	if b.idx >= len(b.buf) {
		// TODO error
		panic("zenio: out of range")
	}

	defer func() { b.idx++ }()
	return bytes.NewReader(b.buf[b.idx]), nil
}

// frameTeeReader is just like a io.TeeReader except it also
// adds the Len() method expected by the protocol.Frame interface.
type frameTeeReader struct {
	tee io.Reader
	f   protocol.Frame
}

func newFrameTeeReader(f protocol.Frame, w io.Writer) *frameTeeReader {
	return &frameTeeReader{io.TeeReader(f, w), f}
}

func (f *frameTeeReader) Read(p []byte) (int, error) {
	return f.tee.Read(p)
}

func (f *frameTeeReader) Len() int {
	return f.f.Len()
}

// BufferMessage adds buffering to a message. It makes it possible to read the
// whole message into memory and rewind it again.
type BufferMessage struct {
	M   protocol.Message
	idx int
	buf []*bytes.Buffer
}

func NewBufferMessage(m protocol.Message) *BufferMessage {
	return &BufferMessage{M: m}
}

func (b *BufferMessage) Reset() {
	b.idx = 0
}

func (b *BufferMessage) More() bool {
	if b.idx < len(b.buf) {
		return true
	}
	return b.M.More()
}

func (b *BufferMessage) Next() (protocol.Frame, error) {
	defer func() { b.idx++ }()
	if b.idx < len(b.buf) {
		return bytes.NewReader(b.buf[b.idx].Bytes()), nil
	}

	f, err := b.M.Next()
	if err != nil {
		return nil, err
	}

	buf := &bytes.Buffer{}
	b.buf = append(b.buf, buf)
	return newFrameTeeReader(f, buf), nil
}
