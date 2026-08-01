package cmd

import (
	"testing"

	"github.com/ncruces/go-sqlite3/vfs/memdb"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/lesomnus/go-app/cmd/config"
)

func TestDbStatus(t *testing.T) {
	x := require.New(t)

	c := config.DbConfig{Driver: "sqlite3", Dsn: memdb.TestDB(t)}
	db, err := c.Open(t.Context())
	x.NoError(err)

	x.Equal(grpc_health_v1.HealthCheckResponse_SERVING, dbStatus(t.Context(), db))

	// The app is not serving anything if the database is gone.
	x.NoError(db.Close())
	x.Equal(grpc_health_v1.HealthCheckResponse_NOT_SERVING, dbStatus(t.Context(), db))
}
