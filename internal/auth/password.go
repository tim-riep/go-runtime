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

// Package auth signs in the users of http services: passwords hashed with
// Argon2id, API tokens of which only a hash is stored, and the methods
// that read the credentials of a request.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Parameters of Argon2id for new passwords.
const (
	argonMemory  = 64 * 1024 // KiB
	argonTime    = 3
	argonThreads = 2
	argonKeyLen  = 32
	saltLen      = 16
)

var b64 = base64.RawStdEncoding

// HashPassword returns the Argon2id hash of a password in PHC format.
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// CheckPassword reports whether a password matches a hash of HashPassword.
func CheckPassword(hash, password string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var version int
	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false
	}
	salt, err1 := b64.DecodeString(parts[4])
	key, err2 := b64.DecodeString(parts[5])
	if err1 != nil || err2 != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(key)))
	return subtle.ConstantTimeCompare(got, key) == 1
}

// TokenPrefix starts every API token.
const TokenPrefix = "rt_"

// NewToken returns a new API token for the token ID and the hash to store:
// rt_<id>_<32 random bytes in base64url>.
func NewToken(id string) (token, hash string, err error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", "", err
	}
	token = TokenPrefix + strings.ReplaceAll(id, "-", "") + "_" + base64.RawURLEncoding.EncodeToString(secret)
	return token, TokenHash(token), nil
}

// TokenHash returns the stored hash of a token: SHA-256 in hex. Tokens are
// random, so a fast hash suffices.
func TokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ParseToken returns the ID in a token.
func ParseToken(token string) (string, error) {
	rest, ok := strings.CutPrefix(token, TokenPrefix)
	if !ok {
		return "", errors.New("auth: no API token")
	}
	id, _, ok := strings.Cut(rest, "_")
	if !ok || len(id) != 32 {
		return "", errors.New("auth: no API token")
	}
	return id[0:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:], nil
}
