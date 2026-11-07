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

package kernel

import (
	"container/list"
	"context"
	"fmt"
	"io/fs"
	"sync"

	"github.com/tim-riep/go-runtime/model"
)

// Limits of the cache of asset contents across executions.
const (
	contentCacheSize  = 64 << 20 // bytes of all contents
	contentCacheEntry = 1 << 20  // largest content kept
)

// assetSource is an asset repository that code of an execution opened.
type assetSource struct {
	x *execution
	r *model.AssetRepository
}

func (s *assetSource) Repository() *model.AssetRepository { return s.r }

func (s *assetSource) MaxRead() int64 { return s.x.k.maxAssetRead }

func (s *assetSource) FS(ctx context.Context, lang model.Optional[string]) fs.FS {
	return &assetFS{ctx: ctx, k: s.x.k, repo: s.r, lang: lang}
}

// Read returns a content from the execution, from the cache of the
// kernel or from the database. Contents are addressed by their hash, so
// a cached content is never stale.
func (s *assetSource) Read(ctx context.Context, c *model.AssetContent) ([]byte, error) {
	x := s.x
	if b, ok := x.contents[c.SHA256]; ok {
		return b, nil
	}
	b, ok := x.k.contents.get(c.SHA256)
	if !ok {
		var err error
		if b, err = x.k.readContent(ctx, c); err != nil {
			return nil, err
		}
		x.k.contents.put(c.SHA256, b)
	}
	if x.contents == nil {
		x.contents = map[string][]byte{}
	}
	x.contents[c.SHA256] = b
	return b, nil
}

// readContent reads a content chunk by chunk from the database.
func (k *Kernel) readContent(ctx context.Context, c *model.AssetContent) ([]byte, error) {
	k.contentReads.Add(1)
	out := make([]byte, 0, c.Size)
	for seq := int64(1); int64(len(out)) < c.Size; seq++ {
		data, err := k.repo.ReadChunk(ctx, k.db, c.SHA256, seq)
		if err != nil {
			return nil, err
		}
		if len(data) == 0 {
			return nil, fmt.Errorf("kernel: content %s ends after %d of %d bytes", c.SHA256, len(out), c.Size)
		}
		out = append(out, data...)
	}
	if int64(len(out)) != c.Size {
		return nil, fmt.Errorf("kernel: content %s has %d bytes, not %d", c.SHA256, len(out), c.Size)
	}
	return out, nil
}

// contentCache keeps small contents by hash, the least recently used
// going first.
type contentCache struct {
	mu        sync.Mutex
	max, each int64
	size      int64
	order     *list.List // of *cached, the most recent first
	byHash    map[string]*list.Element
}

type cached struct {
	hash string
	data []byte
}

func newContentCache(max, each int64) *contentCache {
	return &contentCache{max: max, each: each, order: list.New(), byHash: map[string]*list.Element{}}
}

func (c *contentCache) get(hash string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.byHash[hash]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(e)
	return e.Value.(*cached).data, true
}

func (c *contentCache) put(hash string, data []byte) {
	n := int64(len(data))
	if n > c.each {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.byHash[hash]; ok {
		return
	}
	c.byHash[hash] = c.order.PushFront(&cached{hash, data})
	c.size += n
	for c.size > c.max {
		last := c.order.Back()
		v := last.Value.(*cached)
		c.order.Remove(last)
		delete(c.byHash, v.hash)
		c.size -= int64(len(v.data))
	}
}
