package contracts

import _ "embed"

// RuntimeConfigSchema is the single checked-in schema used by control updates.
//
//go:embed runtime/config.schema.json
var RuntimeConfigSchema []byte
