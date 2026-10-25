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
	"github.com/IQVO/product-content/internal/domain/content"
)

// nullTime stores the zero time as NULL.
func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}

// timeOrZero reads a nullable timestamp as UTC (zero when NULL).
func timeOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.UTC()
}

// attributeJSON and mediaJSON are the stored forms of the value objects.
type attributeJSON struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Unit  string `json:"unit,omitempty"`
}

type mediaJSON struct {
	MediaID string `json:"mediaId"`
	URL     string `json:"url"`
	AltText string `json:"altText"`
	Role    string `json:"role"`
}

// ContentRepository is the Postgres ports.ContentRepository.
type ContentRepository struct{ pool *pgxpool.Pool }

var _ ports.ContentRepository = (*ContentRepository)(nil)

// NewContentRepository constructs a ContentRepository over pool.
func NewContentRepository(pool *pgxpool.Pool) *ContentRepository {
	return &ContentRepository{pool: pool}
}

const contentColumns = `catalog_item_id, state, retired_reason, description, bullets, attributes, media, primary_category_id,
	revision, published_revision, created_at, updated_at, published_at, retired_at, version`

// Get implements ports.ContentRepository. Inside a unit of work the row is
// locked (FOR UPDATE), so concurrent writers of one content serialise.
func (r *ContentRepository) Get(ctx context.Context, id content.ID) (*content.ProductContent, error) {
	suffix := ""
	if inUnitOfWork(ctx) {
		suffix = " FOR UPDATE"
	}
	row := queryFor(ctx, r.pool).QueryRow(ctx, `SELECT `+contentColumns+` FROM product_contents WHERE catalog_item_id = $1`+suffix, string(id))
	c, err := scanContent(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("content %s: %w", id, ports.ErrNotFound)
	}
	return c, err
}

func scanContent(row pgx.Row) (*content.ProductContent, error) {
	var (
		s                           content.Snapshot
		id, state, desc, cat        string
		reason                      *string
		bullets, attrs, media       []byte
		created, updated            time.Time
		published, retired          *time.Time
		revision, publishedRevision int
		version                     int64
	)
	if err := row.Scan(&id, &state, &reason, &desc, &bullets, &attrs, &media, &cat, &revision, &publishedRevision,
		&created, &updated, &published, &retired, &version); err != nil {
		return nil, err
	}
	s.ID, s.State, s.Revision, s.PublishedRevision, s.Version = content.ID(id), content.State(state), revision, publishedRevision, version
	if reason != nil {
		s.RetiredReason = content.RetiredReason(*reason)
	}
	s.CreatedAt, s.UpdatedAt, s.PublishedAt, s.RetiredAt = created.UTC(), updated.UTC(), timeOrZero(published), timeOrZero(retired)
	s.Body.Description, s.Body.PrimaryCategoryID = desc, cat
	if err := unmarshalBody(&s.Body, bullets, attrs, media); err != nil {
		return nil, fmt.Errorf("decode content %s: %w", id, err)
	}
	return content.Restore(s)
}

func unmarshalBody(b *content.Body, bullets, attrs, media []byte) error {
	if err := json.Unmarshal(bullets, &b.Bullets); err != nil {
		return err
	}
	var as []attributeJSON
	if err := json.Unmarshal(attrs, &as); err != nil {
		return err
	}
	for _, a := range as {
		b.Attributes = append(b.Attributes, content.Attribute(a))
	}
	var ms []mediaJSON
	if err := json.Unmarshal(media, &ms); err != nil {
		return err
	}
	for _, m := range ms {
		b.Media = append(b.Media, content.Media{MediaID: m.MediaID, URL: m.URL, AltText: m.AltText, Role: content.Role(m.Role)})
	}
	return nil
}

func marshalBody(b content.Body) (bullets, attrs, media []byte, err error) {
	if bullets, err = json.Marshal(append([]string{}, b.Bullets...)); err != nil {
		return nil, nil, nil, err
	}
	as := make([]attributeJSON, len(b.Attributes))
	for i, a := range b.Attributes {
		as[i] = attributeJSON(a)
	}
	if attrs, err = json.Marshal(as); err != nil {
		return nil, nil, nil, err
	}
	ms := make([]mediaJSON, len(b.Media))
	for i, m := range b.Media {
		ms[i] = mediaJSON{MediaID: m.MediaID, URL: m.URL, AltText: m.AltText, Role: string(m.Role)}
	}
	media, err = json.Marshal(ms)
	return bullets, attrs, media, err
}

// Save implements ports.ContentRepository: a create (LoadedVersion 0) inserts
// and expects the id to be free; any other save updates the row at the loaded
// version. Zero affected rows is ErrConcurrentUpdate.
func (r *ContentRepository) Save(ctx context.Context, c *content.ProductContent) error {
	s := c.Snapshot()
	bullets, attrs, media, err := marshalBody(s.Body)
	if err != nil {
		return fmt.Errorf("encode content %s: %w", s.ID, err)
	}
	var reason *string
	if s.RetiredReason != "" {
		v := string(s.RetiredReason)
		reason = &v
	}
	var affected int64
	err = inTx(ctx, r.pool, func(q querier) error {
		if c.LoadedVersion() == 0 {
			tag, err := q.Exec(ctx, `
				INSERT INTO product_contents (`+contentColumns+`)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
				ON CONFLICT (catalog_item_id) DO NOTHING`,
				string(s.ID), string(s.State), reason, s.Body.Description, bullets, attrs, media, s.Body.PrimaryCategoryID,
				s.Revision, s.PublishedRevision, s.CreatedAt, s.UpdatedAt, nullTime(s.PublishedAt), nullTime(s.RetiredAt), s.Version)
			affected = tag.RowsAffected()
			return err
		}
		tag, err := q.Exec(ctx, `
			UPDATE product_contents SET state = $2, retired_reason = $3, description = $4, bullets = $5, attributes = $6, media = $7,
				primary_category_id = $8, revision = $9, published_revision = $10, updated_at = $11, published_at = $12,
				retired_at = $13, version = $14
			WHERE catalog_item_id = $1 AND version = $15`,
			string(s.ID), string(s.State), reason, s.Body.Description, bullets, attrs, media, s.Body.PrimaryCategoryID,
			s.Revision, s.PublishedRevision, s.UpdatedAt, nullTime(s.PublishedAt), nullTime(s.RetiredAt), s.Version, c.LoadedVersion())
		affected = tag.RowsAffected()
		return err
	})
	if err != nil {
		return fmt.Errorf("save content %s: %w", s.ID, err)
	}
	if affected != 1 {
		return fmt.Errorf("content %s loaded at version %d: %w", s.ID, c.LoadedVersion(), ports.ErrConcurrentUpdate)
	}
	c.MarkPersisted()
	return nil
}

// List implements ports.ContentRepository.
func (r *ContentRepository) List(ctx context.Context, after content.ID, limit int) ([]*content.ProductContent, error) {
	rows, err := queryFor(ctx, r.pool).Query(ctx, `
		SELECT `+contentColumns+` FROM product_contents
		WHERE catalog_item_id > $1 ORDER BY catalog_item_id LIMIT $2`, string(after), limit)
	if err != nil {
		return nil, fmt.Errorf("list contents: %w", err)
	}
	defer rows.Close()
	var out []*content.ProductContent
	for rows.Next() {
		c, err := scanContent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list contents: %w", err)
	}
	return out, nil
}
