package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"reflect"
	"sync"
	"syscall"

	credit "vortex/engine/runtime/accounting"
	"vortex/engine/runtime/config"
)

func applyCreditSelection(initial, next config.Config, book *credit.CreditBook) error {
	if err := next.Validate(); err != nil {
		return err
	}
	if initial.SourceCreditBook == nil || next.SourceCreditBook == nil {
		return errors.New("credit book: missing pinned book")
	}
	active, revision := next.SourceCreditBook.Active, next.SourceCreditBook.SelectionRevision
	baseBook, nextBook := *initial.SourceCreditBook, *next.SourceCreditBook
	baseBook.Active, nextBook.Active = "", ""
	baseBook.SelectionRevision, nextBook.SelectionRevision = 0, 0
	initial.SourceCreditBook, next.SourceCreditBook = &baseBook, &nextBook
	if !reflect.DeepEqual(initial, next) {
		return errors.New("credit book: reload changes pinned configuration")
	}
	return book.Select(active, revision)
}

func watchCreditBook(ctx context.Context, path string, initial config.Config, book *credit.CreditBook) func() {
	watchCtx, cancel := context.WithCancel(ctx)
	reload := make(chan os.Signal, 1)
	signal.Notify(reload, syscall.SIGHUP)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-watchCtx.Done():
				return
			case <-reload:
				next, err := config.LoadFile(path)
				if err == nil {
					err = applyCreditSelection(initial, next, book)
				}
				if err != nil {
					slog.Warn("runtime credit snapshot reload rejected", "err", err)
					continue
				}
				state := book.State()
				slog.Info("runtime credit snapshot selected", "snapshot", state.Active, "revision", state.SelectionRevision)
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { signal.Stop(reload); cancel(); <-done }) }
}
