package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Baiel1922/zhim-back/backend/internal/repo"
	"github.com/Baiel1922/zhim-back/backend/internal/revalidate"
)

// ObjectStore keeps uploaded files (S3).
type ObjectStore interface {
	Put(ctx context.Context, key, contentType string, data []byte) error
	Delete(ctx context.Context, key string) error
}

// Revalidator refreshes storefront caches by tag; Notify must not block.
type Revalidator interface {
	Notify(tags ...string)
}

type noopRevalidator struct{}

func (noopRevalidator) Notify(...string) {}

func notifierOrNoop(r Revalidator) Revalidator {
	if r == nil {
		return noopRevalidator{}
	}
	return r
}

func productTags(slugs map[int64]string) []string {
	tags := []string{revalidate.TagProducts}
	for _, slug := range slugs {
		tags = append(tags, revalidate.ProductTag(slug))
	}
	return tags
}

// Admin is the back-office: catalog, store settings, admins, orders and dashboard.
// Role checks happen in the HTTP layer; every method assumes an authorized caller.
type Admin struct {
	store   *repo.Store
	objects ObjectStore
	notify  Revalidator
	loc     *time.Location
	log     *slog.Logger
	now     func() time.Time
}

// NewAdmin creates the back-office service. loc is the business timezone for
// date filters and the dashboard.
func NewAdmin(store *repo.Store, objects ObjectStore, notify Revalidator, loc *time.Location, log *slog.Logger) *Admin {
	return &Admin{store: store, objects: objects, notify: notifierOrNoop(notify), loc: loc, log: log, now: time.Now}
}

// lookupErr maps "no rows" to a not_found error for what.
func lookupErr(err error, what string) error {
	if repo.IsNotFound(err) {
		return notFound(what)
	}
	return fmt.Errorf("get %s: %w", what, err)
}

// deleted maps a DELETE's affected row count to a not_found error.
func deleted(n int64, err error, what string) error {
	if err != nil {
		return err
	}
	if n == 0 {
		return notFound(what)
	}
	return nil
}
