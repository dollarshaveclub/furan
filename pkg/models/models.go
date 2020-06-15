package models

import (
	"time"

	"github.com/gofrs/uuid"

	"github.com/dollarshaveclub/furan/pkg/generated/furanrpc"
)

//go:generate stringer -type=BuildStatus

type BuildStatus int

const (
	BuildStatusUnknown BuildStatus = iota
	BuildStatusNotStarted
	BuildStatusBuilding
	BuildStatusPushing
	BuildStatusSuccess
	BuildStatusBuildFailure
	BuildStatusPushFailure
	BuildStatusNotNeeded
)

type Build struct {
	ID                          uuid.UUID
	Created, Updated, Completed time.Time
	GitHubRepo, GitHubRef       string
	ImageRepo                   string
	Tags                        []string
	CommitSHATag                bool
	Request                     furanrpc.BuildRequest
	Status                      BuildStatus
	Events                      []string
}

func (b Build) CanAddEvent() bool {
	switch b.Status {
	case BuildStatusUnknown:
		fallthrough
	case BuildStatusNotStarted:
		fallthrough
	case BuildStatusSuccess:
		fallthrough
	case BuildStatusBuildFailure:
		fallthrough
	case BuildStatusPushFailure:
		fallthrough
	case BuildStatusNotNeeded:
		return false
	}
	return true
}
