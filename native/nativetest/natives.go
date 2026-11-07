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

func init() {
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
}
