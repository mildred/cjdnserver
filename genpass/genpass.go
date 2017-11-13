package genpass

import (
	"crypto/rand"
	"github.com/jbenet/go-base58"
)

func Generate(bytes int) string {
	id := make([]byte, bytes)
	rand.Read(id)
	return base58.Encode(id)
}
