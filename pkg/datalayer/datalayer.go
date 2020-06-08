package datalayer

import (
	"context"
	"fmt"
	"time"

	"github.com/dollarshaveclub/furan/pkg/db"
	"github.com/dollarshaveclub/furan/pkg/generated/furanrpc"
	"github.com/gocql/gocql"
	"github.com/golang/protobuf/proto"
	gocqltrace "gopkg.in/DataDog/dd-trace-go.v1/contrib/gocql/gocql"
)

// DataLayer describes an object that interacts with the persistent data store
type DataLayer interface {
	CreateBuild(context.Context, *furanrpc.BuildRequest) (gocql.UUID, error)
	GetBuildByID(context.Context, gocql.UUID) (*furanrpc.BuildStatusResponse, error)
	SetBuildFlags(context.Context, gocql.UUID, map[string]bool) error
	SetBuildCompletedTimestamp(context.Context, gocql.UUID) error
	SetBuildState(context.Context, gocql.UUID, furanrpc.BuildStatusResponse_BuildState) error
	DeleteBuild(context.Context, gocql.UUID) error
	SetBuildTimeMetric(context.Context, gocql.UUID, string) error
	SetDockerImageSizesMetric(context.Context, gocql.UUID, int64, int64) error
	SaveBuildOutput(context.Context, gocql.UUID, []furanrpc.BuildEvent, string) error
	GetBuildOutput(context.Context, gocql.UUID, string) ([]furanrpc.BuildEvent, error)
}

// DBLayer is an DataLayer instance that interacts with the Cassandra database
type DBLayer struct {
	s     *gocql.Session
	sname string
}

// NewDBLayer returns a data layer object
func NewDBLayer(s *gocql.Session, sname string) *DBLayer {
	return &DBLayer{s: s, sname: sname}
}

func (dl *DBLayer) wrapQuery(ctx context.Context, query *gocql.Query) *gocqltrace.Query {
	return gocqltrace.WrapQuery(query, gocqltrace.WithServiceName(dl.sname)).WithContext(ctx)
}

// CreateBuild inserts a new build into the DB returning the ID
func (dl *DBLayer) CreateBuild(ctx context.Context, req *furanrpc.BuildRequest) (id gocql.UUID, err error) {
	q := `INSERT INTO builds_by_id (id, request, state, finished, failed, cancelled, started)
        VALUES (?,{github_repo: ?, dockerfile_path: ?, tags: ?, tag_with_commit_sha: ?, ref: ?,
					push_registry_repo: ?, push_s3_region: ?, push_s3_bucket: ?,
					push_s3_key_prefix: ?},?,?,?,?,?);`
	id, err = gocql.RandomUUID()
	if err != nil {
		return id, err
	}
	udt := db.UDTFromBuildRequest(req)
	query := dl.s.Query(q, id, udt.GithubRepo, udt.DockerfilePath, udt.Tags, udt.TagWithCommitSha, udt.Ref,
		udt.PushRegistryRepo, udt.PushS3Region, udt.PushS3Bucket, udt.PushS3KeyPrefix,
		furanrpc.BuildStatusResponse_STARTED.String(), false, false, false, time.Now())
	err = dl.wrapQuery(ctx, query).Exec()
	if err != nil {
		return id, err
	}
	q = `INSERT INTO build_metrics_by_id (id) VALUES (?);`
	err = dl.wrapQuery(ctx, dl.s.Query(q, id)).Exec()
	if err != nil {
		return id, err
	}
	q = `INSERT INTO build_events_by_id (id) VALUES (?);`
	return id, dl.wrapQuery(ctx, dl.s.Query(q, id)).Exec()
}

// GetBuildByID fetches a build object from the DB
func (dl *DBLayer) GetBuildByID(ctx context.Context, id gocql.UUID) (bi *furanrpc.BuildStatusResponse, err error) {
	q := `SELECT request, state, finished, failed, cancelled, started, completed,
	      duration FROM builds_by_id WHERE id = ?;`
	var udt db.BuildRequestUDT
	var state string
	var started, completed time.Time
	bi = &furanrpc.BuildStatusResponse{
		BuildId: id.String(),
	}
	query := dl.s.Query(q, id)
	err = dl.wrapQuery(ctx, query).Scan(&udt, &state, &bi.Finished, &bi.Failed,
		&bi.Cancelled, &started, &completed, &bi.Duration)
	if err != nil {
		return bi, err
	}
	bi.State = db.BuildStateFromString(state)
	bi.BuildRequest = db.BuildRequestFromUDT(&udt)
	bi.Started = started.Format(time.RFC3339)
	bi.Completed = completed.Format(time.RFC3339)
	return bi, nil
}

// SetBuildFlags sets the boolean flags on the build object
// Caller must ensure that the flags passed in are valid
func (dl *DBLayer) SetBuildFlags(ctx context.Context, id gocql.UUID, flags map[string]bool) (err error) {
	q := `UPDATE builds_by_id SET %v = ? WHERE id = ?;`
	for k, v := range flags {
		err = dl.wrapQuery(ctx, dl.s.Query(fmt.Sprintf(q, k), v, id)).Exec()
		if err != nil {
			return err
		}
	}
	return nil
}

// SetBuildCompletedTimestamp sets the completed timestamp on a build to time.Now()
func (dl *DBLayer) SetBuildCompletedTimestamp(ctx context.Context, id gocql.UUID) (err error) {
	var started time.Time
	now := time.Now()
	q := `SELECT started FROM builds_by_id WHERE id = ?;`
	err = dl.wrapQuery(ctx, dl.s.Query(q, id)).Scan(&started)
	if err != nil {
		return err
	}
	duration := now.Sub(started).Seconds()
	q = `UPDATE builds_by_id SET completed = ?, duration = ? WHERE id = ?;`
	return dl.s.Query(q, now, duration, id).Exec()
}

// SetBuildState sets the state of a build
func (dl *DBLayer) SetBuildState(ctx context.Context, id gocql.UUID, state furanrpc.BuildStatusResponse_BuildState) (err error) {
	q := `UPDATE builds_by_id SET state = ? WHERE id = ?;`
	return dl.wrapQuery(ctx, dl.s.Query(q, state.String(), id)).Exec()
}

// DeleteBuild removes a build from the DB.
// Only used in case of queue full when we can't actually do a build
func (dl *DBLayer) DeleteBuild(ctx context.Context, id gocql.UUID) (err error) {
	q := `DELETE FROM builds_by_id WHERE id = ?;`
	err = dl.wrapQuery(ctx, dl.s.Query(q, id)).Exec()
	if err != nil {
		return err
	}
	q = `DELETE FROM build_metrics_by_id WHERE id = ?;`
	return dl.s.Query(q, id).Exec()
}

// SetBuildTimeMetric sets a build metric to time.Now()
// metric is the name of the column to update
// if metric is a *_completed column, it will also compute and persist the duration
func (dl *DBLayer) SetBuildTimeMetric(ctx context.Context, id gocql.UUID, metric string) (err error) {
	var started time.Time
	now := time.Now()
	getstarted := true
	var startedcolumn string
	var durationcolumn string
	switch metric {
	case "docker_build_completed":
		startedcolumn = "docker_build_started"
		durationcolumn = "docker_build_duration"
	case "push_completed":
		startedcolumn = "push_started"
		durationcolumn = "push_duration"
	case "clean_completed":
		startedcolumn = "clean_started"
		durationcolumn = "clean_duration"
	default:
		getstarted = false
	}
	q := `UPDATE build_metrics_by_id SET %v = ? WHERE id = ?;`
	err = dl.wrapQuery(ctx, dl.s.Query(fmt.Sprintf(q, metric), now, id)).Exec()
	if err != nil {
		return err
	}
	if getstarted {
		q = `SELECT %v FROM build_metrics_by_id WHERE id = ?;`
		err = dl.s.Query(fmt.Sprintf(q, startedcolumn), id).Scan(&started)
		if err != nil {
			return err
		}
		duration := now.Sub(started).Seconds()

		q = `UPDATE build_metrics_by_id SET %v = ? WHERE id = ?;`
		return dl.s.Query(fmt.Sprintf(q, durationcolumn), duration, id).Exec()
	}
	return nil
}

// SetDockerImageSizesMetric sets the docker image sizes for a build
func (dl *DBLayer) SetDockerImageSizesMetric(ctx context.Context, id gocql.UUID, size int64, vsize int64) (err error) {
	q := `UPDATE build_metrics_by_id SET docker_image_size = ?, docker_image_vsize = ? WHERE id = ?;`
	return dl.wrapQuery(ctx, dl.s.Query(q, size, vsize, id)).Exec()
}

// SaveBuildOutput serializes an array of stream events to the database
func (dl *DBLayer) SaveBuildOutput(ctx context.Context, id gocql.UUID, output []furanrpc.BuildEvent, column string) (err error) {
	serialized := make([][]byte, len(output))
	var b []byte
	for i, e := range output {
		b, err = proto.Marshal(&e)
		if err != nil {
			return err
		}
		serialized[i] = b
	}
	q := `UPDATE build_events_by_id SET %v = ? WHERE id = ?;`
	return dl.wrapQuery(ctx, dl.s.Query(fmt.Sprintf(q, column), serialized, id.String())).Exec()
}

// GetBuildOutput returns an array of stream events from the database
func (dl *DBLayer) GetBuildOutput(ctx context.Context, id gocql.UUID, column string) (output []furanrpc.BuildEvent, err error) {
	var rawoutput [][]byte
	output = []furanrpc.BuildEvent{}
	q := `SELECT %v FROM build_events_by_id WHERE id = ?;`
	err = dl.wrapQuery(ctx, dl.s.Query(fmt.Sprintf(q, column), id)).Scan(&rawoutput)
	if err != nil {
		return output, err
	}
	for _, rawevent := range rawoutput {
		event := furanrpc.BuildEvent{}
		err = proto.Unmarshal(rawevent, &event)
		if err != nil {
			return output, err
		}
		output = append(output, event)
	}
	return output, nil
}
