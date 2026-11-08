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

package syntax_test

import (
	"testing"

	"github.com/tim-riep/go-runtime/lang/syntax"
)

// TestEntityStatements parses and prints the entity statements in their
// canonical form, with comments, local mode, by parent and results.
func TestEntityStatements(t *testing.T) {
	src := `public class /TRX/A

  public static method RUN(companies: array of /TRX/T, keys: array of key of /TRX/T) returns INT4
    # create
    modify business object /TRX/COMPANY
      create /TRX/T fields (ID, NAME, /TRY/EXT) from companies
      # children
      create /TRX/C by parent fields (NAME) from companies
      update /TRX/T fields (NAME) from companies
      delete /TRX/T keys keys
      execute /TRX/T.BLOCK keys keys parameter companies[1] into blocked
      execute /TRX/T.COUNT_ALL
      result outcome
    end
    modify business object /TRX/COMPANY in local mode
      delete /TRX/T keys keys
    end
    var rows = read business object /TRX/COMPANY
      /TRX/T fields (NAME, STATUS) keys keys
      result denied
    end
    var all = read business object /TRX/COMPANY in local mode
      /TRX/C by parent fields all keys keys
    end
    if outcome.FAILURES is empty
      commit business objects result saved
    else
      rollback business objects
    end
    var create = 2
    var execute = create / 2
    return rows.LENGTH() + execute
  end

end
`
	f, errs := syntax.Parse("a.class", src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if out := syntax.Print(f); out != src {
		t.Fatalf("not canonical:\n%s", out)
	}
	for _, bad := range []string{
		"public class /TRX/A\n  public static method RUN()\n    modify business object /TRX/B\n      insert /TRX/T keys x\n    end\n  end\nend\n",
		"public class /TRX/A\n  public static method RUN()\n    var x = read business object /TRX/B\n      /TRX/T fields (A)\n    end\n  end\nend\n",
	} {
		if _, errs := syntax.Parse("b.class", bad); len(errs) == 0 {
			t.Errorf("accepted:\n%s", bad)
		}
	}
}
