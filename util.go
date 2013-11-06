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
	"bufio"
	"bytes"
	"encoding/hex"
	"io"
	"math/rand"
	"os"
	"sync"
	"time"
)

// backoff is a simple way to calculate the backoff to use eg. when doing
// retries.
//
// The backoff time will be a random value between [0, n), where n is
// min(Max, (2^count * Step))) and count is the number of attempts. Every call
// to Backoff() or Wait() counts as an attempt.
type backoff struct {
	// Max is the maximum time to return from Backoff.
	Max time.Duration

	// Step is the factor used when calculating the backoff duration.
	Step time.Duration

	count uint
}

func (bt *backoff) defaultDuration(a, b time.Duration) time.Duration {
	if a == 0 {
		return b
	}
	return a
}

// Backoff returns the backoff calculated for this attempt.
func (bt *backoff) Backoff() time.Duration {
	// Use some sane defaults unless specified
	max := bt.defaultDuration(bt.Max, 60*time.Second)
	step := bt.defaultDuration(bt.Step, 42*time.Millisecond)

	if bt.count < 63 {
		bt.count++
	}

	// TODO verify that this calculation actually makes sense
	random := time.Duration(rand.Int() % ((1 << bt.count) - 1))
	backoff := step * random
	if max > 0 && backoff > max {
		backoff = max
	}

	return backoff
}

// Wait is a shorthand for time.Sleep() on the returned duration from
// Backoff().
func (bt *backoff) Wait() {
	time.Sleep(bt.Backoff())
}

// hexDumper is used to buffer data until Flush is called, then dump all
// written data to the Writer stream in a format that matches `hexdump -C`.
type hexDumper struct {
	// Buffer allows us to buffer data until Flush is called. Embed io.Reader
	// interface from bytes. Buffer to make this easy.
	bytes.Buffer

	// Writer is the writer used when constructing the hex dumper. If none is
	// given, os.Stdout will be used.
	Writer io.Writer

	// Prefix is something optional written before the dump.
	Prefix string

	mu sync.Mutex
}

func (hd *hexDumper) Flush() error {
	// Default to Stdout if not specified.
	var writer = hd.Writer
	if writer == nil {
		writer = os.Stdout
	}

	// Dump the hex dump with the optional prefix.
	if hd.Len() > 0 {
		buf := &bytes.Buffer{}
		dumper := hex.Dumper(buf)
		hd.WriteTo(dumper)
		dumper.Close()
		hd.Reset()

		hd.mu.Lock()
		defer hd.mu.Unlock()

		scanner := bufio.NewScanner(buf)
		for scanner.Scan() {
			if _, err := writer.Write([]byte(hd.Prefix)); err != nil {
				return err
			} else if _, err := writer.Write(scanner.Bytes()); err != nil {
				return err
			} else if _, err := writer.Write([]byte("\n")); err != nil {
				return err
			}
		}
	}
	return nil
}
