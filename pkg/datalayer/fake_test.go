package datalayer_test

import (
	"testing"

	"github.com/dollarshaveclub/furan/pkg/datalayer"
	"github.com/dollarshaveclub/furan/pkg/datalayer/testsuite"
)

func TestFakeDBSuite(t *testing.T) {
	testsuite.RunTests(t, func() datalayer.DataLayer {
		return &datalayer.FakeDataLayer{}
	})
}
