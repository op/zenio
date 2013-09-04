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
	"encoding/hex"
	"io"
	"os"
)

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
}

func (hd *hexDumper) Flush() error {
	// Default to Stdout if not specified.
	var writer = hd.Writer
	if writer == nil {
		writer = os.Stdout
	}

	// Dump the hex dump with the optional prefix.
	if hd.Len() > 0 {
		writer.Write([]byte(hd.Prefix))
		dumper := hex.Dumper(writer)
		defer dumper.Close()
		hd.WriteTo(dumper)
	}
	return nil
}
