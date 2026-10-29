package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/IQVO/product-content/internal/application/ports"
	"github.com/IQVO/product-content/internal/application/readmodel"
	"github.com/IQVO/product-content/internal/domain/category"
)

// categoryJSON is the stored form of one category of the tree.
type categoryJSON struct {
	ID       string `json:"categoryId"`
	ParentID string `json:"parentId,omitempty"`
	Name     string `json:"name"`
	Path     string `json:"path"`
}

// TreeRepository is the Postgres ports.TreeRepository: the one tree is one row.
type TreeRepository struct{ pool *pgxpool.Pool }

var _ ports.TreeRepository = (*TreeRepository)(nil)

// NewTreeRepository constructs a TreeRepository over pool.
func NewTreeRepository(pool *pgxpool.Pool) *TreeRepository { return &TreeRepository{pool: pool} }

// Get implements ports.TreeRepository. Inside a unit of work the row is locked.
func (r *TreeRepository) Get(ctx context.Context) (*category.Tree, error) {
	suffix := ""
	if inUnitOfWork(ctx) {
		suffix = " FOR UPDATE"
	}
	var (
		raw     []byte
		version int64
		updated time.Time
	)
	err := queryFor(ctx, r.pool).QueryRow(ctx, `SELECT version, categories, updated_at FROM category_trees WHERE tree_id = $1`+suffix, category.TreeID).
		Scan(&version, &raw, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("category tree: %w", ports.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("read category tree: %w", err)
	}
	var cs []categoryJSON
	if err := json.Unmarshal(raw, &cs); err != nil {
		return nil, fmt.Errorf("decode category tree: %w", err)
	}
	snap := category.Snapshot{Version: version, UpdatedAt: updated.UTC(), Categories: make([]category.Category, len(cs))}
	for i, c := range cs {
		snap.Categories[i] = category.Category(c)
	}
	return category.Restore(snap)
}

// Save implements ports.TreeRepository under the concurrency rule of the ports
// package: the never-stored tree (LoadedVersion 0) inserts and expects no row.
func (r *TreeRepository) Save(ctx context.Context, t *category.Tree) error {
	s := t.Snapshot()
	cs := make([]categoryJSON, len(s.Categories))
	for i, c := range s.Categories {
		cs[i] = categoryJSON(c)
	}
	raw, err := json.Marshal(cs)
	if err != nil {
		return fmt.Errorf("encode category tree: %w", err)
	}
	var affected int64
	err = inTx(ctx, r.pool, func(q querier) error {
		if t.LoadedVersion() == 0 {
			tag, err := q.Exec(ctx, `
				INSERT INTO category_trees (tree_id, version, categories, updated_at) VALUES ($1, $2, $3, $4)
				ON CONFLICT (tree_id) DO NOTHING`, category.TreeID, s.Version, raw, s.UpdatedAt)
			affected = tag.RowsAffected()
			return err
		}
		tag, err := q.Exec(ctx, `
			UPDATE category_trees SET version = $2, categories = $3, updated_at = $4 WHERE tree_id = $1 AND version = $5`,
			category.TreeID, s.Version, raw, s.UpdatedAt, t.LoadedVersion())
		affected = tag.RowsAffected()
		return err
	})
	if err != nil {
		return fmt.Errorf("save category tree: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("category tree loaded at version %d: %w", t.LoadedVersion(), ports.ErrConcurrentUpdate)
	}
	t.MarkPersisted()
	return nil
}

// CatalogItemRepository is the Postgres ports.CatalogItemRepository over
// catalog_item_view.
type CatalogItemRepository struct{ pool *pgxpool.Pool }

var _ ports.CatalogItemRepository = (*CatalogItemRepository)(nil)

// NewCatalogItemRepository constructs a CatalogItemRepository over pool.
func NewCatalogItemRepository(pool *pgxpool.Pool) *CatalogItemRepository {
	return &CatalogItemRepository{pool: pool}
}

// Get implements ports.CatalogItemRepository.
func (r *CatalogItemRepository) Get(ctx context.Context, id string) (readmodel.CatalogItem, error) {
	var (
		it    readmodel.CatalogItem
		state string
	)
	err := queryFor(ctx, r.pool).QueryRow(ctx, `SELECT catalog_item_id, state, updated_at FROM catalog_item_view WHERE catalog_item_id = $1`, id).
		Scan(&it.CatalogItemID, &state, &it.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return readmodel.CatalogItem{}, fmt.Errorf("catalog item %s: %w", id, ports.ErrNotFound)
	}
	if err != nil {
		return readmodel.CatalogItem{}, fmt.Errorf("read catalog item %s: %w", id, err)
	}
	it.State, it.UpdatedAt = readmodel.ItemState(state), it.UpdatedAt.UTC()
	return it, nil
}

// MarkActive implements ports.CatalogItemRepository: an existing row is left as it is.
func (r *CatalogItemRepository) MarkActive(ctx context.Context, id string, at time.Time) error {
	_, err := queryFor(ctx, r.pool).Exec(ctx, `
		INSERT INTO catalog_item_view (catalog_item_id, state, updated_at) VALUES ($1, 'ACTIVE', $2)
		ON CONFLICT (catalog_item_id) DO NOTHING`, id, at.UTC())
	if err != nil {
		return fmt.Errorf("mark catalog item %s active: %w", id, err)
	}
	return nil
}

// MarkDiscontinued implements ports.CatalogItemRepository: DISCONTINUED is
// terminal and keeps the instant of its first mark.
func (r *CatalogItemRepository) MarkDiscontinued(ctx context.Context, id string, at time.Time) error {
	_, err := queryFor(ctx, r.pool).Exec(ctx, `
		INSERT INTO catalog_item_view (catalog_item_id, state, updated_at) VALUES ($1, 'DISCONTINUED', $2)
		ON CONFLICT (catalog_item_id) DO UPDATE SET state = 'DISCONTINUED', updated_at = EXCLUDED.updated_at
		WHERE catalog_item_view.state <> 'DISCONTINUED'`, id, at.UTC())
	if err != nil {
		return fmt.Errorf("mark catalog item %s discontinued: %w", id, err)
	}
	return nil
}
