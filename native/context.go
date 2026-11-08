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

	"github.com/tim-riep/go-runtime/authorization"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/reflector"
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
	// User is the signed-in user of the execution, if any.
	User() model.Optional[string]
	Language() string
	// Now returns the time of the kernel's clock.
	Now() time.Time
	// AfterCommit registers an effect that runs after the next commit of
	// a session: 0 is the default session.
	AfterCommit(session int, e Effect) error
	// KernelPackage reports whether a package is a kernel package.
	KernelPackage(id model.PackageID) bool
	// AssetRepository opens an asset repository of a package for reading
	// by code of the package reader: its own, those of the packages it
	// requires, and all for kernel packages. It returns
	// ErrAssetRepositoryNotFound or an *AssetAccessDenied.
	AssetRepository(reader, pkg model.PackageID, repo string) (AssetSource, error)
	// AssetRepositories returns the asset repositories of a package, by
	// ID, under the same rule.
	AssetRepositories(reader, pkg model.PackageID) ([]*model.AssetRepository, error)
	// Reflector describes the schema of the execution with the texts of
	// the languages set up.
	Reflector() (*reflector.Reflector, error)
	// CheckAuthorization checks an authorization of the user of the
	// execution in its tenant today and records the check. A failed check
	// of mode CheckStatement returns a *NotAuthorized with a text in the
	// language of the execution.
	CheckAuthorization(c authorization.Check, at CheckLocation, mode CheckMode) (bool, error)
	// GrantForTest changes the authorizations of a test method that rt
	// test runs; elsewhere it fails.
	GrantForTest(g TestGrant) error
	// Components returns the components of a structure or table in the
	// order of its resolved definition.
	Components(structure string) ([]Component, error)
}

// TestGrant changes the authorizations of a test method: Clear removes
// them, All grants all, Role adds the authorizations of a role, Object adds
// an authorization with the entries of its fields in the text form of role
// files: a value, * for all values, a prefix such as D* or a range A..C.
type TestGrant struct {
	Clear  bool
	All    bool
	Role   string
	Object string
	Fields map[string][]string
}

// CheckLocation is where an authorization check is made: code of a
// package, or a function of the kernel.
type CheckLocation struct {
	Package model.PackageID `json:"package,omitempty"`
	Class   string          `json:"class,omitempty"`
	Method  string          `json:"method,omitempty"`
	Line    int             `json:"line,omitempty"`
	// Kernel names the function of the kernel or the native method.
	Kernel string `json:"kernel,omitempty"`
}

// CheckMode is how an authorization check is made.
type CheckMode string

// Modes of checks. A failed statement check raises /RT/NOT_AUTHORIZED and
// is remembered as the last failed check; expressions and filters are
// expected to fail sometimes.
const (
	CheckStatement  CheckMode = "check"
	CheckExpression CheckMode = "expression"
	CheckFilter     CheckMode = "filter"
)

// NotAuthorized is the error of a failed check; the VM raises it as
// /RT/NOT_AUTHORIZED.
type NotAuthorized struct {
	Check authorization.Check
	Text  string
}

func (e *NotAuthorized) Error() string { return e.Text }

// AssetSource is an asset repository opened for reading. Its metadata
// stays the same for the rest of the execution.
type AssetSource interface {
	Repository() *model.AssetRepository
	// Read returns a content completely. Contents are addressed by their
	// hash and may come from a cache.
	Read(ctx context.Context, c *model.AssetContent) ([]byte, error)
	// FS returns the repository as file system with a language, for
	// streaming; it reads with the context ctx.
	FS(ctx context.Context, lang model.Optional[string]) fs.FS
	// MaxRead is the largest content Read may load into the memory of
	// the execution.
	MaxRead() int64
}

// ErrAssetRepositoryNotFound tells that a package has no such asset
// repository.
var ErrAssetRepositoryNotFound = errors.New("native: no such asset repository")

// AssetAccessDenied tells that a package may not read the asset
// repositories of another one.
type AssetAccessDenied struct {
	Reader, Package model.PackageID
}

func (e *AssetAccessDenied) Error() string {
	return fmt.Sprintf("package %s may not read the asset repositories of package %s", e.Reader, e.Package)
}

// Context is passed to every call of a native method. Its context.Context
// ends with the time limit of the execution and of the method;
// implementations that work long must respect it.
type Context struct {
	context.Context
	Host   Host
	Caller Caller
	// Callers are the methods that run, the outermost first and Caller
	// last.
	Callers  []Caller
	MaxInput int64 // largest input in bytes a method should accept
	// Method is the native method called, /CLASS.METHOD.
	Method string
	// Limits restrict the formats the method reads; zero fields mean
	// DefaultLimits.
	Limits Limits
}

// Reader returns the package whose rules apply to reading assets: that of
// the innermost calling method outside of kernel packages, so that code
// of kernel packages does not lend its rights to other packages. If only
// kernel packages run, the kernel itself called them, and it is that of
// the caller.
func (c *Context) Reader() model.PackageID {
	if c.Host == nil {
		return c.Caller.Package
	}
	for i := len(c.Callers) - 1; i >= 0; i-- {
		if p := c.Callers[i].Package; !c.Host.KernelPackage(p) {
			return p
		}
	}
	return c.Caller.Package
}

// Tenant returns the tenant of the execution, "" without kernel.
func (c *Context) Tenant() string {
	if c.Host == nil {
		return ""
	}
	return c.Host.Tenant()
}

// User returns the signed-in user of the execution, if any.
func (c *Context) User() model.Optional[string] {
	if c.Host == nil {
		return model.None[string]()
	}
	return c.Host.User()
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

// AssetRepository opens an asset repository of a package for reading, if
// the Reader may read it.
func (c *Context) AssetRepository(pkg model.PackageID, repo string) (AssetSource, error) {
	if c.Host == nil {
		return nil, errors.New("native: assets need the kernel")
	}
	return c.Host.AssetRepository(c.Reader(), pkg, repo)
}

// Authorize checks an authorization of the user of the execution for a
// native method: values by field, any for fields that are not checked. A
// failed check is a *NotAuthorized, which raises /RT/NOT_AUTHORIZED.
func (c *Context) Authorize(object string, values map[string]string, any ...string) error {
	if c.Host == nil {
		return errors.New("native: authorization checks need the kernel")
	}
	check := authorization.Check{Object: object, Values: values, Any: map[string]bool{}}
	for _, f := range any {
		check.Any[f] = true
	}
	at := CheckLocation{Kernel: c.Method}
	ok, err := c.Host.CheckAuthorization(check, at, CheckStatement)
	if err != nil || ok {
		return err
	}
	return &NotAuthorized{Check: check, Text: "no authorization for " + object}
}

// IsNotAuthorized reports a failed check.
func IsNotAuthorized(err error) bool {
	var na *NotAuthorized
	return errors.As(err, &na)
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
