// Copyright 2026 The go-runtime Authors
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

package archive

import (
	"fmt"
	"hash"
	"hash/crc32"
	"io"
)

func newCRC() hash.Hash32 { return crc32.NewIEEE() }

// crcReader checks the CRC-32 of an entry at its end.
type crcReader struct {
	r    io.Reader
	h    hash.Hash32
	want uint32
	size uint64
	n    uint64
}

func (c *crcReader) Read(b []byte) (int, error) {
	n, err := c.r.Read(b)
	c.h.Write(b[:n])
	c.n += uint64(n)
	if err == io.EOF {
		if c.h.Sum32() != c.want {
			return n, fmt.Errorf("%w: the checksum does not match", ErrCorrupt)
		}
		if c.n != c.size {
			return n, fmt.Errorf("%w: the entry has %d bytes, not the %d it claims", ErrCorrupt, c.n, c.size)
		}
	}
	return n, err
}
