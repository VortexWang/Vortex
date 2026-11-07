package service_test

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "testing"
 "time"

 "github.com/google/uuid"
 "vortex/backend/db/generated"
 "vortex/backend/internal/service"
)

type downgradeAPI interface {
 PreviewDowngrade(context.Context, service.TenantScope, string, int64, []string, time.Time) (json.RawMessage, error)
 ConfirmDowngrade(context.Context, service.TenantScope, string, int64, []string, time.Time, string, string, string) (service.PackageResult, error)
 ProcessDuePackageChanges(context.Context, time.Time) (bool, error)
}

func TestDowngradePreviewConfirmDueAuthority(t *testing.T) {
 f := newPortFixture(t)
 ctx := context.Background()
 old := subscribePackage(t,f,createPackage(t,f,packageTerms()))
 p := packageTerms(); p.Capabilities=[]string{"direct"}; p.RuleLimit=0
 target := createPackage(t,f,p)
 r := ruleInput(f.group); reserveRulePort(t,f,r)
 if _,e := (service.RuleService{Pool:f.pool}).Mutate(ctx,f.scope,r,0,uuid.NewString()); e!=nil { t.Fatal(e) }
 api,ok := any(service.PackageService{Pool:f.pool}).(downgradeAPI)
 if !ok { t.Fatal("read-only downgrade preview and atomic future confirmation service missing") }
 at := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
 tables:=[]string{"subscriptions","package_snapshots","traffic_sources","source_expirations","package_changes","restriction_events","current_restrictions","tasks","operations","outbox","audit_events"}
 before:=authoritySnapshot(t,f.pool,f.tenant,tables)
 raw,e:=api.PreviewDowngrade(ctx,f.scope,target,1,nil,at); if e!=nil { t.Fatal(e) }
 var preview struct { Digest string `json:"digest"`; InvalidRules []string `json:"invalid_rules"`; LostCapabilities []string `json:"lost_capabilities"`; Discarded []any `json:"discarded_amounts"`; Resources []string `json:"affected_resources"` }
 if e=json.Unmarshal(raw,&preview);e!=nil { t.Fatal(e) }
 if len(preview.InvalidRules)!=1||preview.InvalidRules[0]!=r.ID||len(preview.LostCapabilities)==0||len(preview.Discarded)!=1||len(preview.Resources)==0||preview.Digest=="" { t.Fatal(string(raw)) }
 if !bytes.Equal(before,authoritySnapshot(t,f.pool,f.tenant,tables)) { t.Fatal("preview mutated authority") }
 key:=uuid.NewString()
 result,e:=api.ConfirmDowngrade(ctx,f.scope,target,1,nil,at,preview.Digest,key,uuid.NewString()); if e!=nil { t.Fatal(e) }
 replay,e:=api.ConfirmDowngrade(ctx,f.scope,target,1,nil,at,preview.Digest,key,uuid.NewString()); if e!=nil||replay!=result { t.Fatal("confirmation replay",e) }
 if _,e=api.ConfirmDowngrade(ctx,f.scope,target,1,nil,at.Add(time.Second),preview.Digest,key,uuid.NewString());!errors.Is(e,service.ErrConflict) { t.Fatal("payload conflict",e) }
 sub,e:=generated.New(f.pool).GetSubscription(ctx,f.tenant);if e!=nil||sub.SnapshotID!=old.SnapshotID { t.Fatal("future change active early",e) }
 if worked,e:=api.ProcessDuePackageChanges(ctx,at.Add(-time.Microsecond));e!=nil||worked { t.Fatal("early due execution",worked,e) }
 if worked,e:=api.ProcessDuePackageChanges(ctx,at);e!=nil||!worked { t.Fatal("due execution",worked,e) }
 sub,e=generated.New(f.pool).GetSubscription(ctx,f.tenant);if e!=nil||sub.SnapshotID!=result.SnapshotID||sub.Version!=old.Version+1 { t.Fatal("due snapshot not activated",sub,e) }
 if worked,e:=api.ProcessDuePackageChanges(ctx,at);e!=nil||worked { t.Fatal("duplicate due execution",worked,e) }
 var count int
 if e=f.pool.QueryRow(ctx,"SELECT count(*) FROM port_allocations WHERE tenant_id=$1 AND state='retained' AND retained_until=$2",f.tenant,at.Add(7*24*time.Hour)).Scan(&count); e!=nil||count!=1 { t.Fatal("seven-day retention",count,e) }
}
