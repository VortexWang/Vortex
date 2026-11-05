package main

import "testing"

func TestAccountingEventLimit(t *testing.T) {
	for _, test := range []struct {
		name, evidence, configured string
		want                       int
		wantErr                    bool
	}{
		{name: "default", want: 1000000},
		{name: "bounded test override", evidence: "/tmp/evidence.jsonl", configured: "4000000", want: 4000000},
		{name: "production ignores test override", configured: "invalid", want: 1000000},
		{name: "bounded long-run override", evidence: "/tmp/evidence.jsonl", configured: "50000000", want: 50000000},
		{name: "reject above hard cap", evidence: "/tmp/evidence.jsonl", configured: "50000001", wantErr: true},
		{name: "reject invalid override", evidence: "/tmp/evidence.jsonl", configured: "many", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := accountingEventLimit(test.evidence, test.configured)
			if (err != nil) != test.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Fatalf("limit=%d want=%d", got, test.want)
			}
		})
	}
}
