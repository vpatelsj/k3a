package strings

import (
	"crypto/rand"
	"fmt"
	"strconv"
	"strings"

	"github.com/OneOfOne/xxhash"
)

// UniqueString implements Azure's UniqueString algorithm for deterministic resource naming
func UniqueString(inputs ...string) string {
	h := xxhash.New64()
	for _, input := range inputs {
		_, _ = h.Write([]byte(strings.ToLower(input)))
	}

	v := h.Sum64()
	id := strconv.FormatUint(v, 36)

	return id
}


// GeneratePassword creates a random password of the given length
func GeneratePassword(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		idx, err := randomInt(len(charset))
		if err != nil {
			return "", err
		}
		b[i] = charset[idx]
	}
	return string(b), nil
}


// randomInt returns a random int in [0, max) using crypto/rand for security
func randomInt(max int) (int, error) {
	if max <= 0 {
		return 0, fmt.Errorf("max must be positive")
	}
	var b [1]byte
	_, err := rand.Read(b[:])
	if err != nil {
		return 0, err
	}
	return int(b[0]) % max, nil
}
