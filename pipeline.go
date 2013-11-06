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
	"github.com/op/zenio/protocol"
)

// pipelinedReader is a protocol reader which allows multiple reads, but makes
// sure they are done in sequence and only when the previous reader is finished
// reading the message.
type pipelinedReader struct {
	r     protocol.Reader
	queue chan protocol.Reader
}

func newPipelinedReader(r protocol.Reader) *pipelinedReader {
	p := &pipelinedReader{r: r}
	p.queue = make(chan protocol.Reader, 1)
	p.queue <- p
	return p
}

func (p *pipelinedReader) reader() <-chan protocol.Reader {
	return p.queue
}

func (p *pipelinedReader) Read() (protocol.Message, error) {
	// Wrap the message which triggers the reader to gets added back to the
	// queue once the previous action is finished.
	msg, err := p.r.Read()
	if msg != nil {
		msg = &pipelinedMessage{
			m:    msg,
			done: func() { p.queue <- p },
		}
	}
	return msg, err
}

// pipelinedMessage will call the function done once the whole message has been
// consumed.
type pipelinedMessage struct {
	m    protocol.Message
	done func()
}

func (p *pipelinedMessage) Next() (protocol.Frame, error) {
	return p.m.Next()
}

func (p *pipelinedMessage) More() bool {
	if !p.m.More() {
		if p.done != nil {
			p.done()
			p.done = nil
		}
		return false
	}
	return true
}
