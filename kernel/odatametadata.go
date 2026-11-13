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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/tim-riep/go-runtime/internal/repo"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/tim-riep/go-runtime/internal/odata"
	"github.com/tim-riep/go-runtime/model"
	"github.com/tim-riep/go-runtime/rterr"
)

// Formats of metadata documents.
const (
	MetadataXML  = "xml"
	MetadataJSON = "json"
)

// metadataDoc is the metadata document of a service in a language in both
// formats, with their ETags.
type metadataDoc struct {
	xml, json         []byte
	xmlETag, jsonETag string
}

func newMetadataDoc(md *odata.Metadata, salt string) *metadataDoc {
	d := &metadataDoc{xml: md.XML(), json: md.JSON()}
	d.xmlETag, d.jsonETag = etagOf(d.xml, salt), etagOf(d.json, salt)
	return d
}

// etagOf returns a strong ETag of a document and what else it depends on.
func etagOf(doc []byte, salt string) string {
	h := sha256.New()
	h.Write(doc)
	h.Write([]byte(salt))
	return `"` + hex.EncodeToString(h.Sum(nil))[:32] + `"`
}

// metadataCache holds the metadata documents of a generation by service,
// language and the generation of the activation of implementations.
type metadataCache struct {
	mu   sync.Mutex
	docs map[string]*metadataDoc
}

// metadata returns the metadata document of a service in a language with
// the implementations of the extension point of the metadata, or without
// them. Documents are cached for the generation of the code and of the
// activation of implementations, so that installing, removing, activating
// and deactivating an implementation builds them again. Failures are not
// cached.
func (k *Kernel) metadata(ctx context.Context, g *generation, svc *model.Service, lang string, withExtensions bool) (*metadataDoc, error) {
	g.metadataOnce.Do(func() { g.metadata = &metadataCache{docs: map[string]*metadataDoc{}} })
	var impls []*model.Implementation
	var n int64
	if withExtensions {
		var err error
		if impls, n, err = k.metadataImplementations(ctx, g, svc); err != nil {
			return nil, err
		}
	}
	// Behaviors of object types discard the metadata of services, which
	// counters in the database tell all kernels.
	gens, err := k.repo.MetadataGenerations(ctx, k.db)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%s\x00%s\x00%t\x00%d\x00%d\x00%d", svc.Name, lang, withExtensions, n, gens[svc.Name], gens[repo.AllServices])
	g.metadata.mu.Lock()
	d, ok := g.metadata.docs[key]
	g.metadata.mu.Unlock()
	if ok {
		return d, nil
	}
	st, err := k.activation(ctx)
	if err != nil {
		return nil, err
	}
	md := odata.BuildMetadata(g.schema, g.odataModel(svc), lang)
	md, err = k.extendMetadata(ctx, g, svc, md, impls, odataSettings(st.kernel).MetadataTimeout)
	if err != nil {
		var me *MetadataExtensionError
		if errors.As(err, &me) {
			slog.Error("an extension of the metadata failed", "service", svc.Name, "implementation", me.Implementation, "package", me.Package,
				"reason", me.Reason)
		}
		return nil, err
	}
	var salt []string
	for _, impl := range impls {
		salt = append(salt, impl.Name+"@"+string(impl.Owner))
	}
	d = newMetadataDoc(md, strings.Join(salt, ","))
	g.metadata.mu.Lock()
	g.metadata.docs[key] = d
	g.metadata.mu.Unlock()
	return d, nil
}

// serveMetadata answers $metadata in XML or, if asked for, in JSON.
func (k *Kernel) serveMetadata(w http.ResponseWriter, r *http.Request, sr *serviceRequest) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		sr.fail(w, odata.Errorf(http.StatusMethodNotAllowed, odata.CodeMethodNotAllowed, "$metadata", r.Method))
		return
	}
	format, e := metadataFormat(r)
	if e != nil {
		sr.fail(w, e)
		return
	}
	d, err := k.metadata(r.Context(), sr.g, sr.svc, sr.lang, true)
	if err != nil {
		sr.internal(w, "building the metadata", err)
		return
	}
	body, etag, ctype, version := d.xml, d.xmlETag, "application/xml", "4.0"
	if format == MetadataJSON {
		body, etag, ctype, version = d.json, d.jsonETag, "application/json", "4.01"
	}
	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Vary", "Accept, Accept-Language")
	if matchesETag(r.Header.Get("If-None-Match"), etag) {
		h.Set("OData-Version", version)
		w.WriteHeader(http.StatusNotModified)
		sr.entry.status = http.StatusNotModified
		return
	}
	sr.writeBody(w, r, http.StatusOK, ctype, body)
	h.Set("OData-Version", version)
}

// metadataFormat returns the format a request asks for: $format first,
// then Accept, XML by default.
func metadataFormat(r *http.Request) (string, *odata.Error) {
	switch f := strings.ToLower(r.URL.Query().Get("$format")); {
	case f == "" && acceptsOnlyJSON(r.Header.Get("Accept")):
		return MetadataJSON, nil
	case f == "":
		return MetadataXML, nil
	case f == "xml" || strings.HasPrefix(f, "application/xml"):
		return MetadataXML, nil
	case f == "json" || strings.HasPrefix(f, "application/json"):
		return MetadataJSON, nil
	default:
		return "", odata.Errorf(http.StatusNotAcceptable, odata.CodeNotAcceptable, f)
	}
}

// acceptsOnlyJSON reports whether Accept names JSON and no XML.
func acceptsOnlyJSON(accept string) bool {
	return strings.Contains(accept, "application/json") && !strings.Contains(accept, "xml")
}

// matchesETag reports whether If-None-Match or If-Match names an ETag.
func matchesETag(header, etag string) bool {
	for _, t := range strings.Split(header, ",") {
		t = strings.TrimSpace(t)
		if t == "*" || t == etag || strings.TrimPrefix(t, "W/") == strings.TrimPrefix(etag, "W/") {
			return true
		}
	}
	return false
}

// serveVocabulary answers /rt/odata/$vocabularies/ with the vocabulary of
// the kernel.
func (k *Kernel) serveVocabulary(w http.ResponseWriter, r *http.Request, kr *kernelRequest, name string) {
	switch name {
	case odata.FieldsNamespace + ".xml":
		kr.writeBody(w, r, http.StatusOK, "application/xml", odata.VocabularyXML)
	case odata.FieldsNamespace + ".json":
		kr.writeBody(w, r, http.StatusOK, "application/json", odata.VocabularyJSON)
	default:
		kr.fail(w, odata.Errorf(http.StatusNotFound, odata.CodeNotFound, r.URL.Path))
	}
}

// ServiceMetadataOptions tell which metadata document to return.
type ServiceMetadataOptions struct {
	// Language is the language of the texts; English when it is not set
	// up.
	Language string
	// Format is MetadataXML or MetadataJSON.
	Format string
	// WithoutExtensions leaves out the implementations of the extension
	// point of the metadata.
	WithoutExtensions bool
}

// ServiceMetadata returns the metadata document of a service, as clients
// get it.
func (k *Kernel) ServiceMetadata(ctx context.Context, name string, opts ServiceMetadataOptions) ([]byte, error) {
	g, err := k.generation(ctx)
	if err != nil {
		return nil, err
	}
	svc := g.schema.Services[name]
	if svc == nil {
		return nil, &rterr.NotFound{Kind: "service", Name: name}
	}
	langs, err := k.repo.LanguageCodes(ctx, k.db)
	if err != nil {
		return nil, err
	}
	lang := opts.Language
	if !langs[lang] {
		lang = model.DefaultLanguage
	}
	d, err := k.metadata(ctx, g, svc, lang, !opts.WithoutExtensions)
	if err != nil {
		return nil, err
	}
	switch opts.Format {
	case MetadataJSON:
		return d.json, nil
	case MetadataXML, "":
		return d.xml, nil
	}
	return nil, &rterr.InvalidArgument{Argument: "format", Message: "the format is xml or json, not " + opts.Format}
}
