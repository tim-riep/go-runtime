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
	"net/http"
	"strconv"

	"github.com/tim-riep/go-runtime/internal/odata"
)

// objectRows returns the objects of an entity set of objects as rows,
// restricted to a key and a filter. They come from the schema of the
// generation, which holds all objects; there is nothing else to authorize
// than the start of the service.
func objectRows(g *generation, t *odata.EntityType, f odata.Format, key []odata.KeyValue, filter odata.Expr) ([]map[string]any, error) {
	var out []map[string]any
	for _, o := range g.schema.ObjectsOf(t.Object.Type.Name) {
		if len(key) > 0 && o.Name != key[0].Value.Text {
			continue
		}
		row, err := t.ObjectRow(o, f)
		if err != nil {
			return nil, err
		}
		if filter != nil {
			ok, e := odata.Matches(filter, row)
			if e != nil {
				return nil, e
			}
			if !ok {
				continue
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// readObjects answers GET on an entity set of objects, its entities and
// $count; filter, order and pages are evaluated in memory.
func (k *Kernel) readObjects(w http.ResponseWriter, r *http.Request, sr *serviceRequest, path *odata.Path, query *odata.Query, f odata.Format) {
	t := path.Set.Type
	if path.Nav != nil {
		sr.fail(w, odata.Errorf(http.StatusNotFound, odata.CodeNotFound, "the navigation"))
		return
	}
	filter := query.Filter
	if path.Single() {
		filter = nil
	}
	rows, err := objectRows(sr.g, t, f, path.Key, filter)
	if err != nil {
		sr.failWith(w, "reading objects", err)
		return
	}
	if path.Single() {
		if len(rows) == 0 {
			sr.fail(w, odata.Errorf(http.StatusNotFound, odata.CodeNotFound, "the entity"))
			return
		}
		o := &odata.Object{}
		if !f.NoMetadata {
			o.Set("@odata.context", sr.contextOf(r, path, query, true))
		}
		o.Append(f.Entity(t, rows[0], query.Select))
		sr.writeOData(w, r, http.StatusOK, f, o)
		return
	}
	if path.Count {
		sr.writeBody(w, r, http.StatusOK, "text/plain", []byte(strconv.Itoa(len(rows))))
		return
	}
	odata.SortRows(rows, query.OrderBy, t.Key)
	total := int64(len(rows))
	start := min(query.Skip+query.SkipToken, total)
	end := total
	if query.Top >= 0 {
		end = min(end, query.Skip+query.Top)
	}
	size := sr.pageSize(r, w)
	more := false
	if end-start > size {
		end, more = start+size, true
	}
	o := &odata.Object{}
	if !f.NoMetadata {
		o.Set("@odata.context", sr.contextOf(r, path, query, false))
	}
	if query.Count {
		o.Set("@odata.count", total)
	}
	value := []any{}
	if start < end {
		for _, row := range rows[start:end] {
			value = append(value, f.Entity(t, row, query.Select))
		}
	}
	o.Set("value", value)
	if more {
		next := r.URL.Query()
		next.Set("$skiptoken", strconv.FormatInt(query.SkipToken+size, 10))
		o.Set("@odata.nextLink", baseURL(r)+r.URL.EscapedPath()+"?"+next.Encode())
	}
	sr.writeOData(w, r, http.StatusOK, f, o)
}
