package service_test

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "vortex/backend/internal/repository"
)

func TestServingRejectsDeletedMember(t *testing.T) {
 f := newPortFixture(t)
 a := f.prepare(t, 20000)
 f.commit(t, a)
 ctx := context.Background()
 if _, err := f.pool.Exec(ctx, "UPDATE nodes SET deleted_at=now() WHERE id=$1", f.nodes[0]); err != nil { t.Fatal(err) }
 members, err := (repository.PortRepository{Pool:f.pool}).Serving(ctx,f.scope,f.group,uuid.NewString())
 if err != nil { t.Fatal(err) }
 for _, member := range members { if member.ID == f.nodes[0] { t.Fatal("deleted member must never enter serving despite retained confirmed ports") } }
}
