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
	t.Run("no rate is no limiter", func(t *testing.T) {
		x := require.New(t)

		// Not a limiter that allows everything -- nothing, so the chain a
		// deployment that said nothing is served with is the one it had.
		x.Nil(config.ServerConfig{}.Limiter())
		x.Nil(config.ServerConfig{Limit: config.LimitConfig{Burst: 10}}.Limiter())
	})
	t.Run("a rate with no burst is one second's worth", func(t *testing.T) {
		x := require.New(t)

		x.Equal(20, config.LimitConfig{Rate: 20}.BurstOr())
		// Rounded up, since a burst is a whole number of calls and rounding
		// down a rate below one would refuse everything.
		x.Equal(1, config.LimitConfig{Rate: 0.5}.BurstOr())
		x.Equal(40, config.LimitConfig{Rate: 20, Burst: 40}.BurstOr())

		x.NotNil(config.ServerConfig{Limit: config.LimitConfig{Rate: 20}}.Limiter())
	})
	t.Run("the environment can turn it off", func(t *testing.T) {
		x := require.New(t)

		var c config.Config
		_, err := config.OverrideFromEnv(&c, []string{
			"GO_APP_SERVER_REFLECTION=false",
			"GO_APP_SERVER_MAX_RECV_MSG_SIZE=1048576",
			"GO_APP_SERVER_KEEPALIVE_MAX_CONNECTION_AGE=30m",
			"GO_APP_SERVER_LIMIT_RATE=50",
		})
		x.NoError(err)

		x.False(c.Server.ServesReflection())
		x.Equal(1<<20, c.Server.MaxRecvMsgSize)
		x.Equal(30*time.Minute, c.Server.Keepalive.MaxConnectionAge)
		// The number a deployment is most likely to want to move without
		// building an image, which is why it is worth a line here.
		x.Equal(float64(50), c.Server.Limit.Rate)
	})
}
