package store

import (
	"path/filepath"
	"strings"
	"testing"
)

// A LIMIT alone does not bound a read: the composite batch-ID primary key
// previously scanned and sorted every historical payload on the writer's
// sole connection. Verify both actual delivery queries seek by insertion
// position without a temporary sort, including after an existing file opens.
func TestUsageDeliverySeeksPendingRowsWithoutHistoricalSort(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.db")
	for range 2 {
		s, err := Open(path, "agent")
		if err != nil {
			t.Fatal(err)
		}
		for _, query := range []string{knownUsageDeliveryQuery, unknownUsageDeliveryQuery} {
			rows, err := s.db.Query("EXPLAIN QUERY PLAN "+query, "node", 1000)
			if err != nil {
				t.Fatal(err)
			}
			var plan string
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
					t.Fatal(err)
				}
				plan += detail
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(plan, "TEMP B-TREE") || !strings.Contains(plan, "rowid>?") {
				t.Fatalf("delivery scanned historical rows instead of seeking: %s", plan)
			}
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
