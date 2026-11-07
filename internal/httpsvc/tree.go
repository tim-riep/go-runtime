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

// Package httpsvc builds the tree of http services: one root node per
// package under /<package ID in lower case>, the service nodes of the
// packages below it and the missing nodes in between, with the settings
// each node inherits and the activation state that makes it reachable.
package httpsvc

import (
	"slices"
	"strings"
	"time"

	"github.com/tim-riep/go-runtime/model"
)

// Settings are the resolved settings of a node.
type Settings struct {
	Authentication  []string               `json:"authentication"`
	Anonymous       bool                   `json:"anonymous"`
	AnonymousTenant model.Optional[string] `json:"anonymousTenant"`
	TenantSources   []string               `json:"tenantSources"`
	LanguageSources []string               `json:"languageSources"`
	MaxBodySize     int64                  `json:"maxBodySize"`
	MaxResponseSize int64                  `json:"maxResponseSize"`
	Timeout         time.Duration          `json:"timeout"`
	Budget          int64                  `json:"budget"`
}

// Defaults are the settings of the kernel for the root node of every
// package: authentication required, no anonymous access.
var Defaults = Settings{
	Authentication:  []string{model.AuthBasic, model.AuthToken},
	TenantSources:   []string{model.SourceHeader, model.SourceQuery, model.SourceUser},
	LanguageSources: []string{model.SourceQuery, model.SourceHeader, model.SourceUser},
	MaxBodySize:     1 << 20,
	MaxResponseSize: 16 << 20,
	Timeout:         30 * time.Second,
	Budget:          10_000_000,
}

// Node is a node of the tree.
type Node struct {
	Path     string // full path, e.g. /trfin/companies
	Package  model.PackageID
	Service  *model.HTTPService // nil for nodes that only exist as parents
	Parent   *Node              // nil for the root node of a package
	Children map[string]*Node   // by segment
	Settings Settings           // resolved
}

// Implicit reports a node that no package file defines.
func (n *Node) Implicit() bool { return n.Service == nil }

// Handlers returns the handler entries of the node.
func (n *Node) Handlers() []model.HTTPHandlerRef {
	if n.Service == nil {
		return nil
	}
	return n.Service.Handlers
}

// Tree is the tree of all service nodes.
type Tree struct {
	roots map[string]*Node // package root by first segment
	nodes map[string]*Node // by full path
}

// Build builds the tree of the service nodes of a schema. Every installed
// package has a root node, also without service nodes.
func Build(s *model.Schema) *Tree {
	t := &Tree{roots: map[string]*Node{}, nodes: map[string]*Node{}}
	for _, id := range s.PackageIDs() {
		root := &Node{Path: model.HTTPPackagePath(id), Package: id, Children: map[string]*Node{}}
		t.roots[strings.TrimPrefix(root.Path, "/")] = root
		t.nodes[root.Path] = root
	}
	paths := make([]string, 0, len(s.HTTPServices))
	for p := range s.HTTPServices {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	for _, p := range paths {
		svc := s.HTTPServices[p]
		n := t.ensure(svc.Owner, p)
		if n != nil {
			n.Service = svc
		}
	}
	for _, root := range t.roots {
		resolve(root, Defaults)
	}
	return t
}

// ensure returns the node of a full path, creating it and its missing
// parents.
func (t *Tree) ensure(owner model.PackageID, path string) *Node {
	segs := strings.Split(strings.TrimPrefix(path, "/"), "/")
	n := t.roots[segs[0]]
	if n == nil {
		return nil
	}
	for _, seg := range segs[1:] {
		c := n.Children[seg]
		if c == nil {
			c = &Node{Path: n.Path + "/" + seg, Package: owner, Parent: n, Children: map[string]*Node{}}
			n.Children[seg] = c
			t.nodes[c.Path] = c
		}
		n = c
	}
	return n
}

// resolve computes the settings of a node and its subtree.
func resolve(n *Node, inherited Settings) {
	s := inherited
	if n.Service != nil {
		own := n.Service.Settings
		if v, ok := own.Authentication.Get(); ok {
			s.Authentication = v
		}
		if v, ok := own.Anonymous.Get(); ok {
			s.Anonymous = v
		}
		if own.AnonymousTenant.Valid() {
			s.AnonymousTenant = own.AnonymousTenant
		}
		if v, ok := own.TenantSources.Get(); ok {
			s.TenantSources = v
		}
		if v, ok := own.LanguageSources.Get(); ok {
			s.LanguageSources = v
		}
		if v, ok := own.MaxBodySize.Get(); ok {
			s.MaxBodySize = v
		}
		if v, ok := own.MaxResponseSize.Get(); ok {
			s.MaxResponseSize = v
		}
		if v, ok := own.TimeoutSeconds.Get(); ok {
			s.Timeout = time.Duration(v) * time.Second
		}
		if v, ok := own.Budget.Get(); ok {
			s.Budget = v
		}
	}
	n.Settings = s
	for _, c := range n.Children {
		resolve(c, s)
	}
}

// Node returns the node of a full path, or nil.
func (t *Tree) Node(path string) *Node { return t.nodes[path] }

// Paths returns the full paths of all nodes in sorted order.
func (t *Tree) Paths() []string {
	out := make([]string, 0, len(t.nodes))
	for p := range t.nodes {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

// Match returns the deepest node whose path is a prefix of the request
// path, segment by segment, and the rest of the path after it without
// leading slash. Paths are case-sensitive.
func (t *Tree) Match(path string) (*Node, string) {
	segs := strings.Split(strings.TrimPrefix(path, "/"), "/")
	n := t.roots[segs[0]]
	if n == nil {
		return nil, ""
	}
	i := 1
	for ; i < len(segs); i++ {
		c := n.Children[segs[i]]
		if c == nil {
			break
		}
		n = c
	}
	return n, strings.Join(segs[i:], "/")
}

// State is the activation state of a node.
type State string

// Activation states. A node is reachable when it and all its ancestors
// are active.
const (
	StateNew      State = "new"      // shipped and never activated
	StateActive   State = "active"   // activated
	StateInactive State = "inactive" // deactivated explicitly
)

// Reachable reports whether a node and all its ancestors are active.
func Reachable(n *Node, states map[string]State) bool {
	for ; n != nil; n = n.Parent {
		if states[n.Path] != StateActive {
			return false
		}
	}
	return true
}

// Activate returns the states after activating a node: the node becomes
// active, and so do the nodes of its subtree that were never activated.
// Nodes that were deactivated explicitly stay inactive.
func Activate(n *Node, states map[string]State) map[string]State {
	out := make(map[string]State, len(states))
	for k, v := range states {
		out[k] = v
	}
	out[n.Path] = StateActive
	var walk func(*Node)
	walk = func(c *Node) {
		for _, ch := range c.Children {
			if out[ch.Path] == StateNew || out[ch.Path] == "" {
				out[ch.Path] = StateActive
			}
			walk(ch)
		}
	}
	walk(n)
	return out
}

// Deactivate returns the states after deactivating a node.
func Deactivate(n *Node, states map[string]State) map[string]State {
	out := make(map[string]State, len(states))
	for k, v := range states {
		out[k] = v
	}
	out[n.Path] = StateInactive
	return out
}

// Sync returns the states for the nodes of a tree: existing states stay,
// new nodes are new, and states of nodes that no longer exist are gone.
func Sync(t *Tree, states map[string]State) map[string]State {
	out := map[string]State{}
	for p := range t.nodes {
		if s, ok := states[p]; ok {
			out[p] = s
		} else {
			out[p] = StateNew
		}
	}
	return out
}
