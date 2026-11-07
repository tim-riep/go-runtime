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

// Package natives implements the methods executed by kernel of the kernel
// package TRBASE and registers them in native.Default.
package natives

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/tim-riep/go-runtime/internal/xslt"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
)

const pkg = "TRBASE"

func static(class, method, sig string, fn any, caps ...string) {
	native.Default.Register(native.Method{Package: pkg, Class: class, Method: method, Static: true, Signature: sig, Capabilities: caps, Func: fn})
}

// xsltFailed turns an error of go-xslt into /TRBASE/XSLT_FAILED.
func xsltFailed(err error) error {
	var xe *xslt.Error
	if errors.As(err, &xe) {
		line := model.None[int32]()
		if xe.Line > 0 {
			line = model.Some(int32(xe.Line))
		}
		return native.Raise("/TRBASE/XSLT_FAILED", xe.Text, line)
	}
	return err
}

// stylesheet is the Go value of /TRBASE/XSLT_STYLESHEET.
type stylesheet struct{ s *xslt.Stylesheet }

func init() {
	const raises = " raises /TRBASE/XSLT_FAILED"
	static("/TRBASE/XSLT_UTILITIES", "RUN", "(stylesheet: BYTES, input: BYTES) returns BYTES"+raises,
		func(c *native.Context, sheet, input []byte) ([]byte, error) {
			if err := c.CheckSize("the stylesheet", len(sheet)); err != nil {
				return nil, err
			}
			if err := c.CheckSize("the input", len(input)); err != nil {
				return nil, err
			}
			s, err := xslt.Compile(string(sheet))
			if err != nil {
				return nil, xsltFailed(err)
			}
			out, err := s.Transform(c, string(input), nil, c.MaxInput)
			return out, xsltFailed(err)
		})

	native.Default.RegisterClass(pkg, "/TRBASE/XSLT_STYLESHEET", (*stylesheet)(nil))
	static("/TRBASE/XSLT_STYLESHEET", "COMPILE", "(stylesheet: BYTES) returns /TRBASE/XSLT_STYLESHEET"+raises,
		func(c *native.Context, sheet []byte) (*stylesheet, error) {
			if err := c.CheckSize("the stylesheet", len(sheet)); err != nil {
				return nil, err
			}
			s, err := xslt.Compile(string(sheet))
			if err != nil {
				return nil, xsltFailed(err)
			}
			return &stylesheet{s: s}, nil
		})
	native.Default.Register(native.Method{Package: pkg, Class: "/TRBASE/XSLT_STYLESHEET", Method: "TRANSFORM",
		Signature: "(input: BYTES) returns BYTES" + raises,
		Func: func(c *native.Context, self *stylesheet, input []byte) ([]byte, error) {
			if err := c.CheckSize("the input", len(input)); err != nil {
				return nil, err
			}
			out, err := self.s.Transform(c, string(input), nil, c.MaxInput)
			return out, xsltFailed(err)
		}})

	static("/TRBASE/CLOCK", "NOW", "() returns TIMESTAMP", func(c *native.Context) (time.Time, error) {
		return c.Now().UTC().Truncate(time.Microsecond), nil
	}, model.CapabilityClock)
	static("/TRBASE/CLOCK", "TODAY", "() returns DATE", func(c *native.Context) (native.Date, error) {
		return native.DateOf(c.Now()), nil
	}, model.CapabilityClock)

	static("/TRBASE/UUID", "NEW", "() returns UUID", func(c *native.Context) (native.UUID, error) {
		var u native.UUID
		if _, err := rand.Read(u[:]); err != nil {
			return u, err
		}
		u[6] = u[6]&0x0f | 0x40
		u[8] = u[8]&0x3f | 0x80
		return u, nil
	})

	static("/TRBASE/HASH", "SHA256", "(data: BYTES) returns BYTES", func(c *native.Context, data []byte) ([]byte, error) {
		if err := c.CheckSize("the data", len(data)); err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		return sum[:], nil
	})
}
