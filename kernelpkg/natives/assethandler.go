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
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/native"
)

func init() {
	static("/TRBASE/ASSET_HANDLER", "SERVE", "(request: /TRBASE/HTTP_REQUEST, response: /TRBASE/HTTP_RESPONSE, pkg: STRING, repository: STRING) returns BOOL",
		serveAsset)
	instance("/TRBASE/HTTP_RESPONSE", "SEND_ASSET", "(request: /TRBASE/HTTP_REQUEST, repository: /TRBASE/ASSET_REPOSITORY, path: STRING) raises "+errAssetNotFound+", "+errPathInvalid,
		func(c *native.Context, resp *HTTPResponse, req *HTTPRequest, a *assetRepository, p string) error {
			content, err := a.asset(p, c.Language())
			if err != nil {
				return err
			}
			sendAsset(c, req, resp, a.src, a.r.Entries[p], content)
			return nil
		})
}

// serveAsset answers GET and HEAD with an asset at the rest path; the
// package of the service node decides which repositories it may read.
func serveAsset(c *native.Context, req *HTTPRequest, resp *HTTPResponse, pkg, repo string) (bool, error) {
	r := req.R
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false, nil
	}
	name := req.Rest
	if model.CheckAssetPath(name) != "" {
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
	e := src.Repository().Entries[name]
	if e == nil || e.Folder {
		return false, nil
	}
	content, _ := model.ChooseAssetContent(e, model.Some(c.Language()))
	if content == nil {
		return false, nil
	}
	sendAsset(c, req, resp, src, e, content)
	return true, nil
}

// sendAsset makes a version of an asset the response: Content-Type,
// Content-Length, ETag, Last-Modified and Cache-Control, 304 for a
// conditional request of an unchanged version, no body for HEAD, and
// otherwise the content streamed from the database after the execution,
// without loading it into its memory.
func sendAsset(c *native.Context, req *HTTPRequest, resp *HTTPResponse, src native.AssetSource, e *model.AssetEntry, content *model.AssetContent) {
	r := req.R
	etag := `"` + content.SHA256 + `"`
	modified := content.ModifiedAt.UTC().Truncate(time.Second)
	resp.Body.Reset()
	h := resp.Header
	h.Set("Content-Type", e.MimeType.Or("application/octet-stream"))
	h.Set("ETag", etag)
	h.Set("Last-Modified", modified.Format(http.TimeFormat))
	h.Set("Cache-Control", "no-cache")
	if notModified(r, etag, modified) {
		resp.Status = http.StatusNotModified
		h.Del("Content-Type")
		h.Del("Content-Length")
		resp.Serve = func(ctx context.Context, w http.ResponseWriter) int64 {
			w.WriteHeader(http.StatusNotModified)
			return 0
		}
		return
	}
	resp.Status = http.StatusOK
	h.Set("Content-Length", strconv.FormatInt(content.Size, 10))
	lang := model.Some(c.Language())
	head := r.Method == http.MethodHead
	resp.Serve = func(ctx context.Context, w http.ResponseWriter) int64 {
		if head {
			w.WriteHeader(http.StatusOK)
			return 0
		}
		f, err := src.FS(ctx, lang).Open(e.Path)
		if err != nil {
			slog.Error("serving an asset", "package", src.Repository().Package, "repository", src.Repository().ID, "path", e.Path, "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			return 0
		}
		defer f.Close()
		w.WriteHeader(http.StatusOK)
		n, err := io.Copy(w, f)
		if err != nil {
			slog.Warn("streaming an asset", "path", e.Path, "error", err)
		}
		return n
	}
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
