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
	"io"
)

// TODO does this interface even makes sense?

type Message interface {
	Reset()
	Len() int
	Bytes(int) []byte
	AppendFrom(int, io.Reader) error
}

// TODO use bytes.Buffer or just a big array and make slices point into buffer.

type BytesMessage struct {
	buf [][]byte
	idx int
}

func (b *BytesMessage) Reset() {
	b.idx = 0
}

func (b *BytesMessage) Len() int {
	return b.idx
}

func (b *BytesMessage) Bytes(n int) []byte {
	if n < 0 || n > b.idx {
		panic("zenio: invalid frame")
	}

	return b.buf[n]
}

func (b *BytesMessage) AppendFrom(size int, rd io.Reader) error {
	if b.idx >= len(b.buf) {
		b.buf = append(b.buf, make([]byte, size))
	} else if size > cap(b.buf[b.idx]) {
		b.buf[b.idx] = make([]byte, size)
	}
	buf := b.buf[b.idx][:size]
	if _, err := io.ReadFull(rd, buf); err != nil {
		return err
	}
	b.buf[b.idx] = buf
	b.idx++
	return nil
}
