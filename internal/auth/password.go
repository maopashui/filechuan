package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    = 1
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonKeyLen  = 32
	saltLen      = 16
)

func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

func VerifyPassword(encoded, password string) bool {
	parts := splitArgon(encoded)
	if parts == nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts.salt)
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts.hash)
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, parts.time, parts.memory, parts.threads, uint32(len(want)))
	if len(got) != len(want) {
		return false
	}
	var diff byte
	for i := range got {
		diff |= got[i] ^ want[i]
	}
	return diff == 0
}

type argonParts struct {
	memory  uint32
	time    uint32
	threads uint8
	salt    string
	hash    string
}

func splitArgon(encoded string) *argonParts {
	// $argon2id$v=19$m=65536,t=1,p=4$salt$hash
	if len(encoded) < 20 || encoded[:10] != "$argon2id$" {
		return nil
	}
	rest := encoded[10:]
	segs := make([]string, 0, 4)
	cur := ""
	for i := 0; i < len(rest); i++ {
		if rest[i] == '$' {
			segs = append(segs, cur)
			cur = ""
			continue
		}
		cur += string(rest[i])
	}
	segs = append(segs, cur)
	if len(segs) != 4 || segs[0] != "v=19" {
		return nil
	}
	var m, t, p int
	if _, err := fmt.Sscanf(segs[1], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return nil
	}
	return &argonParts{memory: uint32(m), time: uint32(t), threads: uint8(p), salt: segs[2], hash: segs[3]}
}

func RandomToken(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func RandomPickupCode() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, 8)
	for i := range out {
		out[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(out), nil
}
