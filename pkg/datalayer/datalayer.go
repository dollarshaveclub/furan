package datalayer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/gofrs/uuid"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgtype"
	pgtypeuuid "github.com/jackc/pgtype/ext/gofrs-uuid"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"

	"github.com/dollarshaveclub/furan/pkg/models"
)

// DataLayer describes an object that interacts with a data store
type DataLayer interface {
	CreateBuild(context.Context, models.Build) (uuid.UUID, error)
	GetBuildByID(context.Context, uuid.UUID) (models.Build, error)
	SetBuildCompletedTimestamp(context.Context, uuid.UUID, time.Time) error
	SetBuildStatus(context.Context, uuid.UUID, models.BuildStatus) error
	DeleteBuild(context.Context, uuid.UUID) error
	ListenForBuildEvents(ctx context.Context, id uuid.UUID, c chan<- string) error
	AddEvent(ctx context.Context, id uuid.UUID, event string) error
}

// PostgresDBLayer is a DataLayer instance that utilizes a PostgreSQL database
type PostgresDBLayer struct {
	p *pgxpool.Pool
}

var _ DataLayer = &PostgresDBLayer{}

// NewPostgresDBLayer returns a data layer object backed by PostgreSQL
func NewPostgresDBLayer(pguri string) (*PostgresDBLayer, error) {
	dbcfg, err := pgxpool.ParseConfig(pguri)
	if err != nil {
		return nil, fmt.Errorf("error parsing pg db uri: %w", err)
	}
	dbcfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		conn.ConnInfo().RegisterDataType(pgtype.DataType{
			Value: &pgtypeuuid.UUID{},
			Name:  "uuid",
			OID:   pgtype.UUIDOID,
		})
		return nil
	}
	pool, err := pgxpool.ConnectConfig(context.Background(), dbcfg)
	if err != nil {
		return nil, fmt.Errorf("error creating pg connection pool: %w", err)
	}
	return &PostgresDBLayer{p: pool}, nil
}

// Close closes all database connections in the connection pool
func (dl *PostgresDBLayer) Close() {
	dl.p.Close()
}

// CreateBuild inserts a new build into the DB returning the ID
func (dl *PostgresDBLayer) CreateBuild(ctx context.Context, b models.Build) (uuid.UUID, error) {
	id, err := uuid.NewV4()
	if err != nil {
		return id, fmt.Errorf("error generating build id: %w", err)
	}
	_, err = dl.p.Exec(ctx,
		`INSERT INTO builds (id, github_repo, github_ref, image_repo, tags, commit_sha_tag, request, status) VALUES ($1,$2,$3,$4,$5,$6,$7,$8);`,
		id, b.GitHubRepo, b.GitHubRef, b.ImageRepo, b.Tags, b.CommitSHATag, b.Request, models.BuildStatusNotStarted)
	if err != nil {
		return id, fmt.Errorf("error inserting build: %w", err)
	}
	return id, nil
}

// GetBuildByID fetches a build object from the DB
func (dl *PostgresDBLayer) GetBuildByID(ctx context.Context, id uuid.UUID) (models.Build, error) {
	out := models.Build{}
	var updated, completed pgtype.Timestamptz
	err := dl.p.QueryRow(ctx, `SELECT id, created, updated, completed, github_repo, github_ref, image_repo, tags, commit_sha_tag, request, status, events FROM builds WHERE id = $1;`, id).Scan(&out.ID, &out.Created, &updated, &completed, &out.GitHubRepo, &out.GitHubRef, &out.ImageRepo, &out.Tags, &out.CommitSHATag, &out.Request, &out.Status, &out.Events)
	if err != nil {
		return out, fmt.Errorf("error getting build by id: %w", err)
	}
	if updated.Status == pgtype.Present {
		out.Updated = updated.Time
	}
	if completed.Status == pgtype.Present {
		out.Completed = completed.Time
	}
	return out, nil
}

func (dl *PostgresDBLayer) SetBuildCompletedTimestamp(ctx context.Context, id uuid.UUID, completed time.Time) error {
	_, err := dl.p.Exec(ctx, `UPDATE builds SET completed = $1 WHERE id = $2;`, completed, id)
	return err
}

func (dl *PostgresDBLayer) SetBuildStatus(ctx context.Context, id uuid.UUID, s models.BuildStatus) error {
	_, err := dl.p.Exec(ctx, `UPDATE builds SET status = $1 WHERE id = $2;`, s, id)
	return err
}

// DeleteBuild removes a build from the DB.
func (dl *PostgresDBLayer) DeleteBuild(ctx context.Context, id uuid.UUID) (err error) {
	_, err = dl.p.Exec(ctx, `DELETE FROM builds WHERE id = $1;`, id)
	return err
}

// ListenForBuildEvents blocks and listens for the build events to occur for a build, writing any events that are received to c.
// If build is not currently running an error will be returned immediately.
// Always returns a non-nil error.
func (dl *PostgresDBLayer) ListenForBuildEvents(ctx context.Context, id uuid.UUID, c chan<- string) error {
	if c == nil {
		return fmt.Errorf("channel cannot be nil")
	}
	conn, err := dl.p.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("error getting db connection: %w", err)
	}
	defer conn.Release()
	b, err := dl.GetBuildByID(ctx, id)
	if err != nil {
		return fmt.Errorf("error getting build by id: %w", err)
	}
	if !b.CanAddEvent() {
		return fmt.Errorf("build status %v; no events are possible", b.Status.String())
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf("LISTEN %s;", pgChanFromID(id))); err != nil {
		return fmt.Errorf("error listening on postgres channel: %w", err)
	}
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return fmt.Errorf("error waiting for notification: %v: %w", id.String(), err)
		}
		c <- n.Payload
	}
}

func pgChanFromID(id uuid.UUID) string {
	return "build_" + strings.ReplaceAll(id.String(), "-", "_")
}

// AddEvent appends an event to a build and notifies any listeners to that channel
func (dl *PostgresDBLayer) AddEvent(ctx context.Context, id uuid.UUID, event string) error {
	txn, err := dl.p.Begin(ctx)
	if err != nil {
		return fmt.Errorf("error opening txn: %w", err)
	}
	defer txn.Rollback(ctx)
	if _, err := txn.Exec(ctx, `UPDATE builds SET events = array_append(events, $1) WHERE id = $2;`, event, id); err != nil {
		return fmt.Errorf("error appending event: %w", err)
	}
	if _, err := txn.Exec(ctx, fmt.Sprintf("NOTIFY %s, '%s';", pgChanFromID(id), event)); err != nil {
		return fmt.Errorf("error notifying channel: %w", err)
	}
	if err := txn.Commit(ctx); err != nil {
		return fmt.Errorf("error committing txn: %w", err)
	}
	return nil
}

func (dl *PostgresDBLayer) spewerr(err error) {
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) {
		spew.Dump(pgerr)
	}
}
