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

package kernel_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/tim-riep/go-runtime/internal/odata"
	"github.com/tim-riep/go-runtime/internal/testutil"
)

// TestODataMetadataSchema checks the metadata documents in JSON and the
// vocabulary of the kernel against the JSON Schema of CSDL 4.01 of OASIS.
func TestODataMetadataSchema(t *testing.T) {
	root := testutil.ModuleRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "testdata", "odata", "oasis", "csdl.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft4)
	const url = "https://docs.oasis-open.org/odata/odata-csdl-json/v4.01/os/schemas/csdl.schema.json"
	if err := c.AddResource(url, doc); err != nil {
		t.Fatal(err)
	}
	schema, err := c.Compile(url)
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(root, "testdata", "golden", "odata", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no metadata documents: %v", err)
	}
	docs := map[string][]byte{"vocabulary RT.Fields": odata.VocabularyJSON}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		docs[filepath.Base(f)] = b
	}
	// The schema rejects what is no CSDL, such as a key that is no list.
	bad, _ := jsonschema.UnmarshalJSON(bytes.NewReader([]byte(`{"$Version":"4.01","N":{"T":{"$Kind":"EntityType","$Key":"ID"}}}`)))
	if schema.Validate(bad) == nil {
		t.Fatal("the schema accepts a key that is no list")
	}
	for name, b := range docs {
		inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := schema.Validate(inst); err != nil {
			t.Errorf("%s does not follow CSDL JSON: %v", name, err)
		}
	}
}
