package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lesomnus/go-app/cmd/config"
)

func TestServerConfig(t *testing.T) {
	t.Run("nothing said is nothing given, so gRPC keeps its own", func(t *testing.T) {
		x := require.New(t)

		c := config.ServerConfig{}
		x.Empty(c.GrpcOptions())
		x.True(c.ServesReflection())
	})
	t.Run("what is said is given", func(t *testing.T) {
		x := require.New(t)

		c := config.ServerConfig{
			MaxRecvMsgSize:       1 << 20,
			MaxConcurrentStreams: 100,
			Keepalive: config.KeepaliveConfig{
				MaxConnectionAge: 30 * time.Minute,
				MinTime:          10 * time.Second,
			},
		}

		// A size, a stream limit, the parameters and the policy.
		x.Len(c.GrpcOptions(), 4)
	})
	t.Run("reflection is on unless it is turned off", func(t *testing.T) {
		x := require.New(t)

		off := false
		on := true

		x.True(config.ServerConfig{}.ServesReflection())
		x.True(config.ServerConfig{Reflection: &on}.ServesReflection())
		x.False(config.ServerConfig{Reflection: &off}.ServesReflection())
	})
	t.Run("the environment can turn it off", func(t *testing.T) {
		x := require.New(t)

		var c config.Config
		_, err := config.OverrideFromEnv(&c, []string{
			"GO_APP_SERVER_REFLECTION=false",
			"GO_APP_SERVER_MAX_RECV_MSG_SIZE=1048576",
			"GO_APP_SERVER_KEEPALIVE_MAX_CONNECTION_AGE=30m",
		})
		x.NoError(err)

		x.False(c.Server.ServesReflection())
		x.Equal(1<<20, c.Server.MaxRecvMsgSize)
		x.Equal(30*time.Minute, c.Server.Keepalive.MaxConnectionAge)
	})
}
