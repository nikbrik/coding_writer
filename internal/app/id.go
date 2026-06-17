package app

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

func NewID(prefix string) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	prefix = strings.Trim(prefix, "_")
	if prefix == "" {
		prefix = "id"
	}
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UTC().UnixNano(), hex.EncodeToString(b[:]))
}
