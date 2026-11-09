package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"time"
	"vortex/backend/internal/auth"
	"vortex/backend/internal/repository"
)

// issueSession is a trusted local operator command. It never accepts a role or
// authenticates an HTTP caller by a request-selected actor identifier.
func issueSession(ctx context.Context, pool *pgxpool.Pool, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("issue-session", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	actor := flags.String("actor", "", "persisted principal UUID")
	tenant := flags.String("tenant", "", "ordinary user's single tenant UUID; omit for platform principal")
	if e := flags.Parse(args); e != nil {
		return e
	}
	if flags.NArg() != 0 || out == nil {
		return errors.New("invalid session command arguments")
	}
	id, e := uuid.Parse(*actor)
	if e != nil || id == uuid.Nil || id.String() != *actor {
		return auth.ErrToken
	}
	if *tenant != "" {
		id, e = uuid.Parse(*tenant)
		if e != nil || id == uuid.Nil || id.String() != *tenant {
			return auth.ErrToken
		}
	}
	store := repository.TokenRepository{Pool: pool}
	principal, e := store.LocalSessionPrincipal(ctx, *actor, *tenant)
	if e != nil {
		return e
	}
	tokens := auth.Tokens{Store: store, AccessTTL: 5 * time.Minute, RefreshTTL: 30 * 24 * time.Hour}
	pair, e := tokens.Issue(ctx, principal, uuid.NewString())
	if e != nil {
		return e
	}
	return json.NewEncoder(out).Encode(pair)
}
