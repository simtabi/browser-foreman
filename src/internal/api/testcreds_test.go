package api

import "crypto/rand"

// apiTok and hookSecret are made fresh each run, so no credential is written down in the tests.
var (
	apiTok     = rand.Text()
	hookSecret = rand.Text()
)
