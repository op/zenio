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

// Protocol contains the abstraction required for protocols in Zenio.
package protocol

import (
	"io"
)

type Negotiator interface {
	Upgrade(io.Reader, io.Writer) (Reader, Writer, error)
}

// TODO ZMTP has greeting, handshake and traffic
// TODO add heartbeating
type Reader interface {
	Read() (Message, error)
}

type Writer interface {
	Write(Message) error
}

// XXX bytes.Reader implements this interface
type Frame interface {
	io.Reader
	Len() int
}

// TODO look at SizeReaderAt / io.SectionReader
// http://talks.golang.org/2013/oscon-dl/sizereaderat.go
// TODO think about a sized WriterTo / ReaderFrom
type Message interface {
	Next() (Frame, error)
	More() bool
}
