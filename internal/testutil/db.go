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

package testutil

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver
)

// PostgresEnv names the environment variable with a PostgreSQL connection
// URL for integration tests. Without it, only SQLite is tested.
const PostgresEnv = "RT_TEST_POSTGRES_DSN"

// Database is a fresh, empty database for one test.
type Database struct {
	Name string // dialect name
	DSN  string // kernel connection string
}

// Databases returns an empty SQLite database and, if PostgresEnv is set, an
// empty PostgreSQL schema. Both are removed when the test ends.
func Databases(t testing.TB) []Database {
	t.Helper()
	dbs := []Database{{Name: "sqlite", DSN: "sqlite:" + filepath.Join(t.TempDir(), "rt.db")}}
	if dsn := os.Getenv(PostgresEnv); dsn != "" {
		dbs = append(dbs, postgresSchema(t, dsn))
	}
	return dbs
}

// postgresSchema creates a schema with a random name and returns a
// connection string whose search_path selects it.
func postgresSchema(t testing.TB, dsn string) Database {
	t.Helper()
	var b [6]byte
	_, _ = rand.Read(b[:])
	schema := fmt.Sprintf("rt_test_%x", b)
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(`CREATE SCHEMA "` + schema + `"`); err != nil {
		admin.Close()
		t.Fatalf("creating test schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`)
		admin.Close()
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	s := u.String()
	if !strings.HasPrefix(s, "postgres") {
		t.Fatalf("%s must be a postgres:// URL", PostgresEnv)
	}
	return Database{Name: "postgres", DSN: s}
}
