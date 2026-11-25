package cli

import "crypto/rand"

// apiTok is made fresh each run, so no credential is written down in the tests.
var apiTok = rand.Text()
