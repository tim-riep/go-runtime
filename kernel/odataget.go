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
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/tim-riep/go-runtime/internal/odata"
	"github.com/tim-riep/go-runtime/model"
)

// responseFormat returns the format of JSON a request asks for with
// $format or Accept.
func responseFormat(r *http.Request) (odata.Format, *odata.Error) {
	var f odata.Format
	params := func(s string) *odata.Error {
		for _, p := range strings.Split(s, ";")[1:] {
			name, value, _ := strings.Cut(strings.TrimSpace(p), "=")
			switch strings.ToLower(name) {
			case "odata.metadata", "metadata":
				switch value {
				case "minimal":
				case "none":
					f.NoMetadata = true
				default:
					return odata.Errorf(http.StatusNotAcceptable, odata.CodeNotAcceptable, "odata.metadata="+value)
				}
			case "ieee754compatible":
				f.IEEE754 = strings.EqualFold(value, "true")
			}
		}
		return nil
	}
	if v := r.URL.Query().Get("$format"); v != "" {
		kind, _, _ := strings.Cut(v, ";")
		if kind != "json" && kind != "application/json" {
			return f, odata.Errorf(http.StatusNotAcceptable, odata.CodeNotAcceptable, v)
		}
		return f, params(v)
	}
	accept := r.Header.Get("Accept")
	if accept == "" {
		return f, nil
	}
	for _, part := range strings.Split(accept, ",") {
		kind, _, _ := strings.Cut(strings.TrimSpace(part), ";")
		switch kind {
		case "application/json", "application/*", "*/*":
			return f, params(strings.TrimSpace(part))
		}
	}
	return f, odata.Errorf(http.StatusNotAcceptable, odata.CodeNotAcceptable, accept)
}

// failWith answers with an error of OData or an internal error.
func (sr *serviceRequest) failWith(w http.ResponseWriter, what string, err error) {
	var oe *odata.Error
	if errors.As(err, &oe) {
		sr.fail(w, oe)
		return
	}
	sr.internal(w, what, err)
}

// serveRead answers GET on entity sets, entities, navigations and $count.
func (k *Kernel) serveRead(w http.ResponseWriter, r *http.Request, sr *serviceRequest, path *odata.Path) {
	ctx := r.Context()
	if path.Import != nil || path.Action != nil {
		sr.fail(w, odata.Errorf(http.StatusMethodNotAllowed, odata.CodeMethodNotAllowed, "an action", r.Method))
		return
	}
	f, e := responseFormat(r)
	if e != nil {
		sr.fail(w, e)
		return
	}
	t := path.Type()
	query, e := odata.ParseQuery(r.URL.Query(), t)
	if e != nil {
		sr.fail(w, e)
		return
	}
	if err := sr.effective(ctx); err != nil {
		sr.internal(w, "loading the authorizations", err)
		return
	}
	q, found, err := sr.target(ctx, path)
	if err != nil {
		sr.failWith(w, "reading", err)
		return
	}
	if !found {
		if path.Nav != nil && !path.Nav.Collection && path.NavKey == nil {
			w.Header().Set("OData-Version", "4.0")
			w.WriteHeader(http.StatusNoContent)
			sr.entry.status = http.StatusNoContent
			return
		}
		sr.fail(w, odata.Errorf(http.StatusNotFound, odata.CodeNotFound, "the entity"))
		return
	}
	if path.Single() {
		k.readEntity(w, r, sr, path, q, query, f)
		return
	}
	if err := q.filter(query.Filter); err != nil {
		sr.failWith(w, "reading", err)
		return
	}
	if path.Count {
		n, err := sr.count(ctx, q)
		if err != nil {
			sr.internal(w, "counting", err)
			return
		}
		sr.writeBody(w, r, http.StatusOK, "text/plain", []byte(strconv.FormatInt(n, 10)))
		return
	}
	k.readCollection(w, r, sr, path, q, query, f)
}

// target returns the query of the entities a path addresses. For
// navigations it reads the entity the navigation starts from; found is
// false when it does not exist or, for a navigation to one entity, the
// foreign key is null.
func (sr *serviceRequest) target(ctx context.Context, path *odata.Path) (*odataQuery, bool, error) {
	if path.Nav == nil {
		q, err := sr.newQuery(path.Set.Type)
		if err != nil {
			return nil, false, err
		}
		return q, true, q.key(path.Key)
	}
	pq, err := sr.newQuery(path.Set.Type)
	if err != nil {
		return nil, false, err
	}
	if err := pq.key(path.Key); err != nil {
		return nil, false, err
	}
	parents, err := sr.rows(ctx, pq, pq.selectText(nil, 1, 0))
	if err != nil || len(parents) == 0 {
		return nil, false, err
	}
	parent, nav := parents[0], path.Nav
	q, err := sr.newQuery(nav.Type)
	if err != nil {
		return nil, false, err
	}
	for _, c := range nav.Reverse {
		if err := q.equal(c.Field, parent[c.References]); err != nil {
			return nil, false, err
		}
	}
	for _, c := range nav.Constraints {
		v := parent[c.Field]
		if v == nil {
			return nil, false, nil
		}
		if err := q.equal(c.References, v); err != nil {
			return nil, false, err
		}
	}
	if err := q.key(path.NavKey); err != nil {
		return nil, false, err
	}
	return q, true, nil
}

// contextOf returns the context URL of a response: the entity set, or the
// path for contained entities, with the selected and expanded properties.
func (sr *serviceRequest) contextOf(r *http.Request, path *odata.Path, query *odata.Query, single bool) string {
	target := path.Set.Name
	if path.Nav != nil {
		if path.Nav.Target != nil {
			target = path.Nav.Target.Name
		} else {
			target = sr.resourceBase(r, path)
		}
	}
	var proj []string
	if query.Select != nil {
		proj = append(proj, odata.SelectText(query.Select))
	}
	for _, x := range query.Expand {
		proj = append(proj, x.Nav.Name+"("+odata.SelectText(x.Select)+")")
	}
	if len(proj) > 0 {
		target += "(" + strings.Join(proj, ",") + ")"
	}
	if single {
		target += "/$entity"
	}
	return sr.base + "$metadata#" + target
}

// resourceBase returns the path of a contained navigation for context
// URLs, such as Companies('C1')/TROTTO_COMPANY_CHANNELS.
func (sr *serviceRequest) resourceBase(r *http.Request, path *odata.Path) string {
	segs := odataSegments(sr.resource)
	if len(segs) >= 2 {
		nav, _, _ := strings.Cut(segs[1], "(")
		return segs[0] + "/" + nav
	}
	return path.Set.Name
}

// odataSegments splits a resource path at slashes outside quotes.
func odataSegments(resource string) []string {
	var out []string
	start, quoted := 0, false
	for i := 0; i < len(resource); i++ {
		switch resource[i] {
		case '\'':
			quoted = !quoted
		case '/':
			if !quoted {
				out = append(out, resource[start:i])
				start = i + 1
			}
		}
	}
	return append(out, resource[start:])
}

// readEntity answers with one entity, 404 when it does not exist or may
// not be read, and 304 when If-None-Match names its ETag.
func (k *Kernel) readEntity(w http.ResponseWriter, r *http.Request, sr *serviceRequest, path *odata.Path, q *odataQuery, query *odata.Query, f odata.Format) {
	ctx := r.Context()
	rows, err := sr.rows(ctx, q, q.selectText(nil, 1, 0))
	if err != nil {
		sr.internal(w, "reading an entity", err)
		return
	}
	if len(rows) == 0 {
		sr.fail(w, odata.Errorf(http.StatusNotFound, odata.CodeNotFound, "the entity"))
		return
	}
	t := q.t
	etag := ""
	if t.ETag != nil {
		etag = odata.ETag(t.ETag.Domain, rows[0][t.ETag.Field.Name])
		w.Header().Set("ETag", etag)
		if etag != "" && matchesETag(r.Header.Get("If-None-Match"), etag) {
			w.Header().Set("OData-Version", "4.0")
			w.WriteHeader(http.StatusNotModified)
			sr.entry.status = http.StatusNotModified
			return
		}
	}
	expanded, err := sr.expand(ctx, t, rows, query.Expand, f)
	if err != nil {
		sr.failWith(w, "expanding", err)
		return
	}
	o := &odata.Object{}
	if !f.NoMetadata {
		o.Set("@odata.context", sr.contextOf(r, path, query, true))
	}
	ent := f.Entity(t, rows[0], query.Select)
	sr.appendEntity(o, ent, expanded[0])
	sr.writeOData(w, r, http.StatusOK, f, o)
}

// appendEntity copies an entity with its expanded navigations into o.
func (sr *serviceRequest) appendEntity(o, ent *odata.Object, nav *odata.Object) {
	o.Append(ent)
	if nav != nil {
		o.Append(nav)
	}
}

// pageSize returns the size of pages: the setting of the node, or less
// when the client prefers odata.maxpagesize.
func (sr *serviceRequest) pageSize(r *http.Request, w http.ResponseWriter) int64 {
	size := int64(odataSettings(sr.st.kernel).MaxPageSize)
	for _, p := range strings.Split(r.Header.Get("Prefer"), ",") {
		name, value, _ := strings.Cut(strings.TrimSpace(p), "=")
		if strings.EqualFold(name, "odata.maxpagesize") {
			if n, err := strconv.ParseInt(value, 10, 64); err == nil && n > 0 && n < size {
				size = n
				w.Header().Add("Preference-Applied", "odata.maxpagesize="+value)
			}
		}
	}
	return size
}

// readCollection answers with a page of entities, the count if asked for
// and the link to the next page.
func (k *Kernel) readCollection(w http.ResponseWriter, r *http.Request, sr *serviceRequest, path *odata.Path, q *odataQuery, query *odata.Query, f odata.Format) {
	ctx := r.Context()
	var total int64
	if query.Count {
		n, err := sr.count(ctx, q)
		if err != nil {
			sr.internal(w, "counting", err)
			return
		}
		total = n
	}
	size := sr.pageSize(r, w)
	limit := size
	if query.Top >= 0 {
		limit = min(limit, max(query.Top-query.SkipToken, 0))
	}
	rows, err := sr.rows(ctx, q, q.selectText(query.OrderBy, limit+1, query.Skip+query.SkipToken))
	if err != nil {
		sr.internal(w, "reading entities", err)
		return
	}
	more := int64(len(rows)) > limit && (query.Top < 0 || query.SkipToken+limit < query.Top)
	if int64(len(rows)) > limit {
		rows = rows[:limit]
	}
	expanded, err := sr.expand(ctx, q.t, rows, query.Expand, f)
	if err != nil {
		sr.failWith(w, "expanding", err)
		return
	}
	o := &odata.Object{}
	if !f.NoMetadata {
		o.Set("@odata.context", sr.contextOf(r, path, query, false))
	}
	if query.Count {
		o.Set("@odata.count", total)
	}
	value := []any{}
	for i, row := range rows {
		ent := &odata.Object{}
		sr.appendEntity(ent, f.Entity(q.t, row, query.Select), expanded[i])
		value = append(value, ent)
	}
	o.Set("value", value)
	if more {
		next := r.URL.Query()
		next.Set("$skiptoken", strconv.FormatInt(query.SkipToken+limit, 10))
		o.Set("@odata.nextLink", baseURL(r)+r.URL.EscapedPath()+"?"+next.Encode())
	}
	sr.writeOData(w, r, http.StatusOK, f, o)
}

// expand reads the navigations of $expand for rows of a type and returns
// for every row an object with them, or nil without $expand.
func (sr *serviceRequest) expand(ctx context.Context, t *odata.EntityType, rows []map[string]any, ex []odata.Expand, f odata.Format) ([]*odata.Object, error) {
	out := make([]*odata.Object, len(rows))
	if len(ex) == 0 || len(rows) == 0 {
		return out, nil
	}
	for i := range out {
		out[i] = &odata.Object{}
	}
	limit := int64(odataSettings(sr.st.kernel).MaxPageSize)
	for _, x := range ex {
		nav := x.Nav
		pairs, many := nav.Constraints, false
		if len(nav.Reverse) > 0 {
			pairs, many = nav.Reverse, true
		}
		// The fields of the source and of the target that must be equal.
		var src, dst []string
		for _, c := range pairs {
			if many {
				src, dst = append(src, c.References), append(dst, c.Field)
			} else {
				src, dst = append(src, c.Field), append(dst, c.References)
			}
		}
		keyOf := func(row map[string]any, fields []string) (string, []any, bool) {
			var parts []string
			var vals []any
			for _, fl := range fields {
				v := row[fl]
				if v == nil {
					return "", nil, false
				}
				parts = append(parts, odata.CanonicalText(v))
				vals = append(vals, v)
			}
			return strings.Join(parts, "\x00"), vals, true
		}
		seen := map[string]bool{}
		var tuples [][]any
		for _, row := range rows {
			if k, vals, ok := keyOf(row, src); ok && !seen[k] {
				seen[k] = true
				tuples = append(tuples, vals)
			}
		}
		q, err := sr.newQuery(nav.Type)
		if err != nil {
			return nil, err
		}
		if err := q.tuples(dst, tuples); err != nil {
			return nil, err
		}
		targets, err := sr.rows(ctx, q, q.selectText(nil, -1, 0))
		if err != nil {
			return nil, err
		}
		groups := map[string][]map[string]any{}
		for _, tr := range targets {
			if k, _, ok := keyOf(tr, dst); ok {
				groups[k] = append(groups[k], tr)
			}
		}
		for i, row := range rows {
			k, _, ok := keyOf(row, src)
			group := groups[k]
			if !ok {
				group = nil
			}
			if !many {
				if len(group) == 0 {
					out[i].Set(nav.Name, nil)
				} else {
					out[i].Set(nav.Name, f.Entity(nav.Type, group[0], x.Select))
				}
				continue
			}
			if int64(len(group)) > limit {
				return nil, odata.Errorf(http.StatusBadRequest, odata.CodeInvalidRequest,
					"an entity has more than "+strconv.FormatInt(limit, 10)+" entities in "+nav.Name+"; read them through the navigation")
			}
			list := []any{}
			for _, tr := range group {
				list = append(list, f.Entity(nav.Type, tr, x.Select))
			}
			out[i].Set(nav.Name, list)
		}
	}
	return out, nil
}

// writeOData writes a response object in a format of JSON.
func (sr *serviceRequest) writeOData(w http.ResponseWriter, r *http.Request, status int, f odata.Format, o *odata.Object) {
	body, err := json.Marshal(o)
	if err != nil {
		sr.internal(w, "encoding a response", err)
		return
	}
	sr.writeBody(w, r, status, f.ContentType(), body)
}

// escapedResource returns the resource path below the root of a service
// in escaped form.
func escapedResource(r *http.Request) string {
	p := strings.TrimPrefix(r.URL.EscapedPath(), model.ODataPath+"/")
	parts := strings.SplitN(p, "/", 3)
	if len(parts) < 3 {
		return ""
	}
	return parts[2]
}
