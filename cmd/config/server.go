package config

import (
	"math"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"

	"github.com/lesomnus/go-app/internal/grpcx"
)

type ServerConfig struct {
	// Addr is the address the gRPC server listens on, e.g. ":50051".
	Addr string `yaml:"addr"`

	TLS TLSConfig `yaml:"tls"`

	// MaxRecvMsgSize is the largest message the server accepts, in bytes.
	// Zero leaves gRPC's own limit, which is 4 MiB.
	MaxRecvMsgSize int `yaml:"max_recv_msg_size"`
	// MaxSendMsgSize is the largest message the server sends, in bytes. Zero
	// leaves gRPC's own limit, which is no limit at all.
	MaxSendMsgSize int `yaml:"max_send_msg_size"`

	// MaxConcurrentStreams is how many calls one connection may have in flight
	// at a time. Zero leaves gRPC's own limit.
	MaxConcurrentStreams uint32 `yaml:"max_concurrent_streams"`

	// Timeout is how long a call that arrived without a deadline of its own is
	// given. A call that named one is left alone, however far away it is.
	//
	// Unset is [grpcx.DefaultTimeout]. Zero, written down, means such a call
	// is not capped at all, which is a thing to mean rather than to end up
	// with -- hence the pointer.
	Timeout *time.Duration `yaml:"timeout"`

	// Reflection serves the reflection service, which is what lets grpcurl and
	// the like ask the server what it offers without holding the protobuf
	// definitions. It is on unless it is turned off.
	Reflection *bool `yaml:"reflection"`

	// GeneralWrites serves `Patch` and `Apply`, the two RPCs every generated
	// service has that can write anything the schema holds. It is **off**
	// unless it is turned on, and turning it on is a decision about the API
	// rather than about a deployment: what a caller may change, and under what
	// conditions, is not something a general write can be told.
	//
	// It closes them at the transport and not in the server stack, so an RPC
	// written by hand goes on being implemented with them. That is what they
	// are for. See the README, "The general write is not an API".
	GeneralWrites *bool `yaml:"general_writes"`

	Limit LimitConfig `yaml:"limit"`

	Keepalive KeepaliveConfig `yaml:"keepalive"`
}

// LimitConfig is how often one caller may call. What "one caller" is, is said
// where callers are worked out and not here; see `gate.ByTenant`.
//
// It is **off** unless a rate is written down, and that is a decision rather
// than an oversight. A deadline can be defaulted because the default only
// touches a call that named none, and a wrong one costs a call. A rate cannot:
// a number picked by a template is a number nobody measured, and the traffic it
// would refuse is real traffic, on the day the app is busiest. So this file
// suggests one and the deployment chooses it.
type LimitConfig struct {
	// Rate is how many calls a second one caller may make, kept up. Not
	// positive -- which is what an absent block says -- is no limit at all.
	Rate float64 `yaml:"rate"`

	// Burst is how many may arrive at once before the rate is what is left.
	// Unset is one second's worth, rounded up, and never less than one: a
	// burst below one is a limiter that refuses everything.
	//
	// It is the number that decides whether an honest client is refused. A
	// client that sends its work in batches is bursty by construction, and a
	// burst of one turns that into an error rather than into a wait.
	Burst int `yaml:"burst"`
}

// Limits reports whether a limit was configured at all.
func (c LimitConfig) Limits() bool {
	return c.Rate > 0
}

// BurstOr is the burst that was configured, or one second's worth of the rate.
func (c LimitConfig) BurstOr() int {
	if c.Burst > 0 {
		return c.Burst
	}

	return max(1, int(math.Ceil(c.Rate)))
}

// Limiter counts the calls of one caller, or is nothing if the configuration
// named no rate.
//
// The nil is written out rather than returned as a typed one, since a typed nil
// in an interface is not nil and [grpcx.Limit] reads it as a limiter that
// refuses to answer.
func (c ServerConfig) Limiter() grpcx.Limiter {
	if !c.Limit.Limits() {
		return nil
	}

	return grpcx.NewLimiter(c.Limit.Rate, c.Limit.BurstOr())
}

// KeepaliveConfig is when a connection is hung up on and when it is asked
// whether it is still there. Every duration is left to gRPC if it is zero.
type KeepaliveConfig struct {
	// MaxConnectionIdle closes a connection that has had no call for this
	// long.
	MaxConnectionIdle time.Duration `yaml:"max_connection_idle"`
	// MaxConnectionAge closes a connection this long after it was opened,
	// whatever it is doing. Behind a load balancer that only balances new
	// connections, this is what makes it balance anything at all.
	MaxConnectionAge time.Duration `yaml:"max_connection_age"`
	// MaxConnectionAgeGrace is how long a call has to finish once the
	// connection it is on has reached its age.
	MaxConnectionAgeGrace time.Duration `yaml:"max_connection_age_grace"`

	// Time is how long the server waits before asking an idle connection
	// whether it is still there, and Timeout is how long it waits for the
	// answer before hanging up.
	Time    time.Duration `yaml:"time"`
	Timeout time.Duration `yaml:"timeout"`

	// MinTime is how often a client may ask the same of the server. One that
	// asks more often is hung up on, which is what "too_many_pings" means.
	// Keep it below what the clients are configured with.
	MinTime time.Duration `yaml:"min_time"`
	// PermitWithoutStream lets a client ask while it has no call in flight.
	PermitWithoutStream bool `yaml:"permit_without_stream"`
}

// ServesReflection reports whether the reflection service is to be served.
func (c ServerConfig) ServesReflection() bool {
	return c.Reflection == nil || *c.Reflection
}

// CallTimeout is how long a call that arrived without a deadline is given.
func (c ServerConfig) CallTimeout() time.Duration {
	if c.Timeout == nil {
		return grpcx.DefaultTimeout
	}

	return *c.Timeout
}

// ServesGeneralWrites reports whether `Patch` and `Apply` are served.
func (c ServerConfig) ServesGeneralWrites() bool {
	return c.GeneralWrites != nil && *c.GeneralWrites
}

// Closed names the methods this server does not serve at all.
func (c ServerConfig) Closed() func(method string) bool {
	if c.ServesGeneralWrites() {
		return nil
	}

	return grpcx.GeneralWrite
}

// GrpcOptions is what the configuration says about the server itself, as
// opposed to what every call goes through, which is `internal/grpcx`.
func (c ServerConfig) GrpcOptions() []grpc.ServerOption {
	opts := []grpc.ServerOption{}
	if v := c.MaxRecvMsgSize; v > 0 {
		opts = append(opts, grpc.MaxRecvMsgSize(v))
	}
	if v := c.MaxSendMsgSize; v > 0 {
		opts = append(opts, grpc.MaxSendMsgSize(v))
	}
	if v := c.MaxConcurrentStreams; v > 0 {
		opts = append(opts, grpc.MaxConcurrentStreams(v))
	}

	k := c.Keepalive
	if k.MaxConnectionIdle > 0 || k.MaxConnectionAge > 0 || k.MaxConnectionAgeGrace > 0 || k.Time > 0 || k.Timeout > 0 {
		opts = append(opts, grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     k.MaxConnectionIdle,
			MaxConnectionAge:      k.MaxConnectionAge,
			MaxConnectionAgeGrace: k.MaxConnectionAgeGrace,
			Time:                  k.Time,
			Timeout:               k.Timeout,
		}))
	}
	if k.MinTime > 0 || k.PermitWithoutStream {
		opts = append(opts, grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             k.MinTime,
			PermitWithoutStream: k.PermitWithoutStream,
		}))
	}

	return opts
}
