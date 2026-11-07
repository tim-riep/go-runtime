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

// Package nativetest holds the native methods of the test kernel package
// TRTEST in testdata/lang/native. They exist only for tests and are not
// part of the registry of the kernel binary.
package nativetest

import (
	"errors"
	"strings"
	"sync/atomic"
	"time"

	"github.com/tim-riep/go-runtime/lang/dec"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
)

//go:generate go run ../../cmd/rt gen natives ../../testdata/lang/native/TRTEST --go-package nativetest -o types_gen.go

// Registry holds the native methods of TRTEST.
var Registry = native.NewRegistry()

const class = "/TRTEST/NATIVES"

func register(method, sig string, fn any) {
	Registry.Register(native.Method{Package: "TRTEST", Class: class, Method: method, Static: true, Signature: sig, Func: fn})
}

// Counter is the Go value of /TRTEST/COUNTER.
type Counter struct{ n int64 }

// Close records that a counter was freed.
func (c *Counter) Close() error {
	Closed.Add(1)
	return nil
}

// Closed counts the counters that were freed.
var Closed atomic.Int64

func init() {
	Registry.RegisterClass("TRTEST", "/TRTEST/COUNTER", (*Counter)(nil))
	const counter = "/TRTEST/COUNTER"
	Registry.Register(native.Method{Package: "TRTEST", Class: counter, Method: "START", Static: true, Signature: "(first: INT8) returns /TRTEST/COUNTER",
		Func: func(c *native.Context, first int64) (*Counter, error) { return &Counter{n: first}, nil }})
	Registry.Register(native.Method{Package: "TRTEST", Class: counter, Method: "NEXT", Signature: "() returns INT8",
		Func: func(c *native.Context, self *Counter) (int64, error) {
			self.n++
			return self.n - 1, nil
		}})
	Registry.Register(native.Method{Package: "TRTEST", Class: counter, Method: "CLOSE", Signature: "()", Releases: true,
		Func: func(c *native.Context, self *Counter) error { return nil }})

	register("ECHO", "(item: /TRTEST/ITEM) returns /TRTEST/ITEM", func(c *native.Context, item Item) (Item, error) {
		item.Name = strings.ToUpper(item.Name)
		if note, ok := item.Note.Get(); ok {
			item.Note = model.Some(note + "!")
		}
		if item.Status == StatusOpen {
			item.Status = StatusClosed
		}
		return item, nil
	})
	register("ECHO_ALL", "(items: /TRTEST/ITEMS) returns /TRTEST/ITEMS", func(c *native.Context, items []Item) ([]Item, error) {
		out := make([]Item, 0, len(items))
		for i := len(items) - 1; i >= 0; i-- {
			out = append(out, items[i])
		}
		return out, nil
	})
	register("TEXT", "(text: STRING?) returns STRING?", func(c *native.Context, text model.Optional[string]) (model.Optional[string], error) {
		s, ok := text.Get()
		if !ok {
			return model.None[string](), nil
		}
		return model.Some("[" + s + "]"), nil
	})
	register("DOUBLE", "(amount: DEC(15,2)) returns DEC(15,2)", func(c *native.Context, amount dec.Dec) (dec.Dec, error) {
		return dec.Add(amount, amount)
	})
	register("BAD_CODE", "() returns /TRTEST/CODE", func(c *native.Context) (string, error) { return "TOOLONG", nil })
	register("FAIL", "(code: INT4) raises /TRTEST/FAILED", func(c *native.Context, code int32) error {
		return native.Raise("/TRTEST/FAILED", "failed with code", code)
	})
	register("UNDECLARED", "()", func(c *native.Context) error { return native.Raise("/TRTEST/FAILED", "not declared", 1) })
	register("BROKEN", "() returns STRING", func(c *native.Context) (string, error) { return "", errors.New("the disk is full") })
	register("PANIC", "()", func(c *native.Context) error { panic("something impossible") })
	register("SLOW", "()", func(c *native.Context) error {
		select {
		case <-c.Done():
			return c.Err()
		case <-time.After(10 * time.Second):
			return nil
		}
	})
	Registry.Register(native.Method{Package: "TRTEST", Class: class, Method: "SLOW_LIMITED", Static: true, Signature: "()", Timeout: 50 * time.Millisecond,
		Func: func(c *native.Context) error {
			<-c.Done()
			return c.Err()
		}})
	register("SIZE", "(data: BYTES) returns INT8", func(c *native.Context, data []byte) (int64, error) {
		if err := c.CheckSize("the data", len(data)); err != nil {
			return 0, err
		}
		return int64(len(data)), nil
	})
	Registry.Register(native.Method{Package: "TRTEST", Class: class, Method: "TICK", Static: true, Signature: "() returns TIMESTAMP",
		Capabilities: []string{model.CapabilityClock}, Func: func(c *native.Context) (time.Time, error) { return c.Now(), nil }})
}
