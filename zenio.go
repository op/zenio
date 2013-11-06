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

// Package zenio implements ØMQ (ZMTP) and nanomsg (SP) in Go without any
// dependencies on zeromq or nanomsg.
package zenio

import "github.com/op/zenio/protocol"

// TODO use envelope as a way to do versioning of protocols?
// TODO do we need to pass in information as eg. Pattern (req/rep)?

type Envelope interface {
	LocalAddr() string
}

// TODO add handshake?
// TODO io.ReadSeeker is nice because we can retry send + get size of data to send

type Protocol interface {
	// Send encodes the protocol data from Envelope and Message and writes it to
	// the underlying writer.
	Send(Envelope, protocol.Message) error

	// Recv reads protocol data from the underlying reader and decodes it into
	// Envelope and Message.
	Recv(Envelope, protocol.Message) error
}
