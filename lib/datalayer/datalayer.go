package datalayer

import (
	"fmt"
	"time"

	"github.com/dollarshaveclub/furan/generated/lib"
	"github.com/dollarshaveclub/furan/lib/db"
	"github.com/dollarshaveclub/furan/lib/tracing"
	"github.com/gocql/gocql"
	"github.com/golang/protobuf/proto"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

// DataLayer describes an object that interacts with the persistent data store
type DataLayer interface {
	CreateBuild(tracer.Span, *lib.BuildRequest) (gocql.UUID, error)
	GetBuildByID(tracer.Span, gocql.UUID) (*lib.BuildStatusResponse, error)
	SetBuildFlags(tracer.Span, gocql.UUID, map[string]bool) error
	SetBuildCompletedTimestamp(tracer.Span, gocql.UUID) error
	SetBuildState(tracer.Span, gocql.UUID, lib.BuildStatusResponse_BuildState) error
	DeleteBuild(tracer.Span, gocql.UUID) error
	SetBuildTimeMetric(tracer.Span, gocql.UUID, string) error
	SetDockerImageSizesMetric(tracer.Span, gocql.UUID, int64, int64) error
	SaveBuildOutput(tracer.Span, gocql.UUID, []lib.BuildEvent, string) error
	GetBuildOutput(tracer.Span, gocql.UUID, string) ([]lib.BuildEvent, error)
}

// DBLayer is an DataLayer instance that interacts with the Cassandra database
type DBLayer struct {
	s *gocql.Session
}

// NewDBLayer returns a data layer object
func NewDBLayer(s *gocql.Session) *DBLayer {
	return &DBLayer{s: s}
}

// CreateBuild inserts a new build into the DB returning the ID
func (dl *DBLayer) CreateBuild(parentSpan tracer.Span, req *lib.BuildRequest) (gocql.UUID, error) {
	queryString := `INSERT INTO builds_by_id (id, request, state, finished, failed, cancelled, started)
        VALUES (?,{github_repo: ?, dockerfile_path: ?, tags: ?, tag_with_commit_sha: ?, ref: ?,
					push_registry_repo: ?, push_s3_region: ?, push_s3_bucket: ?,
					push_s3_key_prefix: ?},?,?,?,?,?);`
	id, err := gocql.RandomUUID()
	if err != nil {
		return id, err
	}
	udt := db.UDTFromBuildRequest(req)
	query := dl.s.Query(queryString, id, udt.GithubRepo, udt.DockerfilePath, udt.Tags, udt.TagWithCommitSha, udt.Ref,
		udt.PushRegistryRepo, udt.PushS3Region, udt.PushS3Bucket, udt.PushS3KeyPrefix,
		lib.BuildStatusResponse_STARTED.String(), false, false, false, time.Now())
	tracedQuery := tracing.GetTracedQuery(query, parentSpan)
	err = tracedQuery.Exec()
	if err != nil {
		return id, err
	}

	queryString = `INSERT INTO build_metrics_by_id (id) VALUES (?);`
	tracedQuery = tracing.GetTracedQuery(dl.s.Query(queryString, id), parentSpan)
	tracedQuery.Exec()
	if err != nil {
		return id, err
	}
	queryString = `INSERT INTO build_events_by_id (id) VALUES (?);`
	tracedQuery = tracing.GetTracedQuery(dl.s.Query(queryString, id), parentSpan)
	return id, dl.s.Query(queryString, id).Exec()
}

// GetBuildByID fetches a build object from the DB
func (dl *DBLayer) GetBuildByID(parentSpan tracer.Span, id gocql.UUID) (*lib.BuildStatusResponse, error) {
	queryString := `SELECT request, state, finished, failed, cancelled, started, completed,
	      duration FROM builds_by_id WHERE id = ?;`
	var udt db.BuildRequestUDT
	var state string
	var started, completed time.Time
	bi := &lib.BuildStatusResponse{
		BuildId: id.String(),
	}
	tracedQuery := tracing.GetTracedQuery(dl.s.Query(queryString, id), parentSpan)
	err := tracedQuery.Scan(&udt, &state, &bi.Finished, &bi.Failed,
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
func (dl *DBLayer) SetBuildFlags(parentSpan tracer.Span, id gocql.UUID, flags map[string]bool) error {
	var err error
	queryString := `UPDATE builds_by_id SET %v = ? WHERE id = ?;`
	for k, v := range flags {
		query := dl.s.Query(fmt.Sprintf(queryString, k), v, id)
		tracedQuery := tracing.GetTracedQuery(query, parentSpan)
		err = tracedQuery.Exec()
		if err != nil {
			return err
		}
	}
	return nil
}

// SetBuildCompletedTimestamp sets the completed timestamp on a build to time.Now()
func (dl *DBLayer) SetBuildCompletedTimestamp(parentSpan tracer.Span, id gocql.UUID) error {
	var started time.Time
	now := time.Now()

	queryString := `SELECT started FROM builds_by_id WHERE id = ?;`
	query := dl.s.Query(queryString, id)
	tracedQuery := tracing.GetTracedQuery(query, parentSpan)
	err := tracedQuery.Scan(&started)
	if err != nil {
		return err
	}
	duration := now.Sub(started).Seconds()
	queryString = `UPDATE builds_by_id SET completed = ?, duration = ? WHERE id = ?;`
	query = dl.s.Query(queryString, now, duration, id)
	tracedQuery = tracing.GetTracedQuery(query, parentSpan)
	return tracedQuery.Exec()
}

// SetBuildState sets the state of a build
func (dl *DBLayer) SetBuildState(parentSpan tracer.Span, id gocql.UUID, state lib.BuildStatusResponse_BuildState) error {
	queryString := `UPDATE builds_by_id SET state = ? WHERE id = ?;`
	query := dl.s.Query(queryString, state.String(), id)
	tracedQuery := tracing.GetTracedQuery(query, parentSpan)
	return tracedQuery.Exec()
}

// DeleteBuild removes a build from the DB.
// Only used in case of queue full when we can't actually do a build
func (dl *DBLayer) DeleteBuild(parentSpan tracer.Span, id gocql.UUID) error {
	queryString := `DELETE FROM builds_by_id WHERE id = ?;`
	query := dl.s.Query(queryString, id)
	tracedQuery := tracing.GetTracedQuery(query, parentSpan)
	err := tracedQuery.Exec()
	if err != nil {
		return err
	}
	queryString = `DELETE FROM build_metrics_by_id WHERE id = ?;`
	query = dl.s.Query(queryString, id)
	tracedQuery = tracing.GetTracedQuery(query, parentSpan)
	return tracedQuery.Exec()
}

// SetBuildTimeMetric sets a build metric to time.Now()
// metric is the name of the column to update
// if metric is a *_completed column, it will also compute and persist the duration
func (dl *DBLayer) SetBuildTimeMetric(parentSpan tracer.Span, id gocql.UUID, metric string) error {
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
	queryString := `UPDATE build_metrics_by_id SET %v = ? WHERE id = ?;`
	query := dl.s.Query(fmt.Sprintf(queryString, metric), now, id)
	tracedQuery := tracing.GetTracedQuery(query, parentSpan)
	err := tracedQuery.Exec()
	if err != nil {
		return err
	}
	if getstarted {
		queryString = `SELECT %v FROM build_metrics_by_id WHERE id = ?;`
		query = dl.s.Query(fmt.Sprintf(queryString, startedcolumn), id)
		tracedQuery = tracing.GetTracedQuery(query, parentSpan)
		err := tracedQuery.Scan(&started)
		if err != nil {
			return err
		}
		duration := now.Sub(started).Seconds()

		queryString = `UPDATE build_metrics_by_id SET %v = ? WHERE id = ?;`
		query = dl.s.Query(fmt.Sprintf(queryString, durationcolumn), duration, id)
		tracedQuery = tracing.GetTracedQuery(query, parentSpan)
		return tracedQuery.Exec()
	}
	return nil
}

// SetDockerImageSizesMetric sets the docker image sizes for a build
func (dl *DBLayer) SetDockerImageSizesMetric(parentSpan tracer.Span, id gocql.UUID, size int64, vsize int64) error {
	queryString := `UPDATE build_metrics_by_id SET docker_image_size = ?, docker_image_vsize = ? WHERE id = ?;`
	query := dl.s.Query(queryString, size, vsize, id)
	tracedQuery := tracing.GetTracedQuery(query, parentSpan)
	return tracedQuery.Exec()
}

// SaveBuildOutput serializes an array of stream events to the database
func (dl *DBLayer) SaveBuildOutput(parentSpan tracer.Span, id gocql.UUID, output []lib.BuildEvent, column string) error {
	serialized := make([][]byte, len(output))
	var err error
	var b []byte
	for i, e := range output {
		b, err = proto.Marshal(&e)
		if err != nil {
			return err
		}
		serialized[i] = b
	}
	queryString := `UPDATE build_events_by_id SET %v = ? WHERE id = ?;`
	query := dl.s.Query(fmt.Sprintf(queryString, column), serialized, id.String())
	tracedQuery := tracing.GetTracedQuery(query, parentSpan)
	return tracedQuery.Exec()
}

// GetBuildOutput returns an array of stream events from the database
func (dl *DBLayer) GetBuildOutput(parentSpan tracer.Span, id gocql.UUID, column string) ([]lib.BuildEvent, error) {
	var rawoutput [][]byte
	output := []lib.BuildEvent{}
	queryString := `SELECT %v FROM build_events_by_id WHERE id = ?;`
	query := dl.s.Query(fmt.Sprintf(queryString, column), id)
	tracedQuery := tracing.GetTracedQuery(query, parentSpan)
	err := tracedQuery.Scan(&rawoutput)
	if err != nil {
		return output, err
	}
	for _, rawevent := range rawoutput {
		event := lib.BuildEvent{}
		err = proto.Unmarshal(rawevent, &event)
		if err != nil {
			return output, err
		}
		output = append(output, event)
	}
	return output, nil
}
