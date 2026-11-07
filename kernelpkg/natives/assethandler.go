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

package natives

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
)

// assetMeta is what the kernel tells about an asset beyond fs.FileInfo.
type assetMeta interface {
	ETag() string
	ContentType() string
}

func init() {
	static("/TRBASE/ASSET_HANDLER", "SERVE", "(request: /TRBASE/HTTP_REQUEST, response: /TRBASE/HTTP_RESPONSE, pkg: STRING, repository: STRING) returns BOOL",
		serveAsset)
}

// serveAsset answers GET and HEAD with an asset at the rest path. The
// content is streamed when the request is done.
func serveAsset(c *native.Context, req *HTTPRequest, resp *HTTPResponse, pkg, repo string) (bool, error) {
	r := req.R
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false, nil
	}
	name := req.Rest
	if name == "" || !fs.ValidPath(name) || path.Clean(name) != name {
		return false, nil
	}
	src, err := c.Host.AssetRepository(req.Package, model.PackageID(pkg), repo)
	var denied *native.AssetAccessDenied
	if errors.Is(err, native.ErrAssetRepositoryNotFound) || errors.As(err, &denied) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	fsys := src.FS(c, model.Some(c.Language()))
	info, err := fs.Stat(fsys, name)
	if errors.Is(err, fs.ErrNotExist) || err == nil && info.IsDir() {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	etag, ctype := "", "application/octet-stream"
	if m, ok := info.Sys().(assetMeta); ok {
		etag = m.ETag()
		if t := m.ContentType(); t != "" {
			ctype = t
		}
	}
	modified := info.ModTime().UTC().Truncate(time.Second)
	h := resp.Header
	h.Set("Content-Type", ctype)
	if etag != "" {
		h.Set("ETag", etag)
	}
	h.Set("Last-Modified", modified.Format(http.TimeFormat))
	h.Set("Cache-Control", "no-cache")
	if notModified(r, etag, modified) {
		resp.Status = http.StatusNotModified
		h.Del("Content-Type")
		return true, nil
	}
	resp.Status = http.StatusOK
	h.Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	if r.Method == http.MethodHead {
		return true, nil
	}
	resp.Serve = func(ctx context.Context, w http.ResponseWriter) int64 {
		if c, ok := fsys.(interface{ WithContext(context.Context) fs.FS }); ok {
			fsys = c.WithContext(ctx)
		}
		f, err := fsys.Open(name)
		if err != nil {
			slog.Error("serving an asset", "package", pkg, "repository", repo, "path", name, "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			return 0
		}
		defer f.Close()
		w.WriteHeader(http.StatusOK)
		n, err := io.Copy(w, f)
		if err != nil {
			slog.Warn("streaming an asset", "path", name, "error", err)
		}
		return n
	}
	return true, nil
}

// notModified evaluates If-None-Match, or If-Modified-Since without it.
func notModified(r *http.Request, etag string, modified time.Time) bool {
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		for _, t := range strings.Split(inm, ",") {
			if t = strings.TrimSpace(t); t == "*" || etag != "" && strings.TrimPrefix(t, "W/") == etag {
				return true
			}
		}
		return false
	}
	if ims := r.Header.Get("If-Modified-Since"); ims != "" {
		if t, err := http.ParseTime(ims); err == nil && !modified.After(t) {
			return true
		}
	}
	return false
}
