package store

import "crypto/rand"

// hash1 and hash2 stand in for stored password hashes; they are made fresh each run, so none is written down.
var (
	hash1 = "$argon2id$" + rand.Text()
	hash2 = "$argon2id$" + rand.Text()
)
