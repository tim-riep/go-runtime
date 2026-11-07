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

package native

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/tim-riep/go-runtime/model"
)

// Caller is the method whose code called a native method.
type Caller struct {
	Package model.PackageID
	Class   string
	Method  string
}

// Host is what the kernel provides to native methods of one execution.
type Host interface {
	Tenant() string
	Language() string
	// Now returns the time of the kernel's clock.
	Now() time.Time
	// AfterCommit registers an effect that runs after the next commit of
	// a session: 0 is the default session.
	AfterCommit(session int, e Effect) error
	// Assets returns the asset repository repo of a package for reading,
	// with lang as language; nil when the package has no such repository.
	Assets(pkg model.PackageID, repo string, lang model.Optional[string]) (fs.FS, error)
}

// Context is passed to every call of a native method. Its context.Context
// ends with the time limit of the execution and of the method;
// implementations that work long must respect it.
type Context struct {
	context.Context
	Host     Host
	Caller   Caller
	MaxInput int64 // largest input in bytes a method should accept
}

// Tenant returns the tenant of the execution, "" without kernel.
func (c *Context) Tenant() string {
	if c.Host == nil {
		return ""
	}
	return c.Host.Tenant()
}

// Language returns the language of the execution.
func (c *Context) Language() string {
	if c.Host == nil {
		return model.DefaultLanguage
	}
	return c.Host.Language()
}

// Now returns the time of the kernel's clock, for methods with the
// capability clock.
func (c *Context) Now() time.Time {
	if c.Host == nil {
		return time.Now().UTC()
	}
	return c.Host.Now()
}

// CheckSize fails with /RT/INPUT_TOO_LARGE when an input of n bytes is
// larger than MaxInput.
func (c *Context) CheckSize(what string, n int) error {
	if c.MaxInput > 0 && int64(n) > c.MaxInput {
		return &InputTooLarge{What: what, Size: int64(n), Max: c.MaxInput}
	}
	return nil
}

// AfterCommit registers an effect that runs after the next successful
// commit of a session and is discarded by rollback. Session 0 is the
// default session.
func (c *Context) AfterCommit(session int, e Effect) error {
	if c.Host == nil {
		return errors.New("native: effects need the kernel")
	}
	return c.Host.AfterCommit(session, e)
}

// Assets returns an asset repository of a package for reading.
func (c *Context) Assets(pkg model.PackageID, repo string, lang model.Optional[string]) (fs.FS, error) {
	if c.Host == nil {
		return nil, errors.New("native: assets need the kernel")
	}
	return c.Host.Assets(pkg, repo, lang)
}

// Effect is an effect outside of the VM and the database. It is
// serializable so that a failed effect can be run again.
type Effect struct {
	Kind    string // registered with Registry.RegisterEffect
	Payload json.RawMessage
}

// EffectFunc runs an effect.
type EffectFunc func(ctx context.Context, payload json.RawMessage) error

// RegisterEffect registers the function that runs effects of a kind.
func (r *Registry) RegisterEffect(kind string, fn EffectFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.effects[kind]; dup {
		panic(fmt.Sprintf("native: effect %s is registered twice", kind))
	}
	r.effects[kind] = fn
}

// Effect returns the function of an effect kind.
func (r *Registry) Effect(kind string) (EffectFunc, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fn, ok := r.effects[kind]
	return fn, ok
}

// Exception is an error that becomes an exception of the language: Class
// must be declared with raises by the method, or be a subclass of a
// declared class. Args are passed to its constructor.
type Exception struct {
	Class string
	Args  []any
}

func (e *Exception) Error() string { return fmt.Sprintf("%s %v", e.Class, e.Args) }

// Raise returns an error that raises the exception class with the
// arguments of its constructor.
func Raise(class string, args ...any) error { return &Exception{Class: class, Args: args} }

// InputTooLarge is the error of CheckSize.
type InputTooLarge struct {
	What      string
	Size, Max int64
}

func (e *InputTooLarge) Error() string {
	return fmt.Sprintf("%s has %d bytes, more than the limit of %d", e.What, e.Size, e.Max)
}
