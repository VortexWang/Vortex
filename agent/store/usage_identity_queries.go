package store

import "strings"

// Fixed prepared VALUES tables avoid parsing the same JSON identities in each
// branch of a compound lookup. All identities remain bound parameters. NULL
// padding is excluded before joining any durable evidence table.
type usageIdentityQuery struct {
	query string
	slots int
}

func usageIdentityQueries(body string) [4]usageIdentityQuery {
	var result [4]usageIdentityQuery
	for i, slots := range []int{2, 8, 16, 32} {
		values := strings.TrimSuffix(strings.Repeat("(?,?),", slots), ",")
		result[i] = usageIdentityQuery{
			query: "WITH identities(node_id,batch_id) AS (VALUES " + values + "), wanted AS MATERIALIZED (SELECT node_id,batch_id FROM identities WHERE node_id IS NOT NULL)\n" + body,
			slots: slots,
		}
	}
	return result
}

// Callers validate the original group bound (1..32) before selecting a shape.
// Duplicate identities retain their original ordering in the caller's payload
// validation; they appear only once in this evidence lookup.
func bindUsageIdentities(queries [4]usageIdentityQuery, batches []HubUsage) (string, []any) {
	seen := make(map[[2]string]bool, len(batches))
	var keys [][2]string
	for _, b := range batches {
		key := [2]string{b.NodeID, b.BatchID}
		if !seen[key] {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	shape := 0
	for len(keys) > queries[shape].slots {
		shape++
	}
	selected := queries[shape]
	args := make([]any, selected.slots*2)
	for i, key := range keys {
		args[2*i], args[2*i+1] = key[0], key[1]
	}
	return selected.query, args
}
