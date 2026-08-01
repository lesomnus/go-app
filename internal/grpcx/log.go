package grpcx

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/lesomnus/otx/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Log writes a line for every call that is served.
func Log() []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(LogUnary()),
		grpc.ChainStreamInterceptor(LogStream()),
	}
}

func LogUnary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		t := time.Now()
		v, err := handler(ctx, req)
		logServed(ctx, info.FullMethod, time.Since(t), err)

		return v, err
	}
}

func LogStream() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		t := time.Now()
		err := handler(srv, ss)
		logServed(ss.Context(), info.FullMethod, time.Since(t), err)

		return err
	}
}

func logServed(ctx context.Context, method string, took time.Duration, err error) {
	if isNoise(method) {
		return
	}

	l := log.From(ctx)
	c := status.Code(err)
	vs := []any{
		slog.String("grpc.method", method),
		slog.String("grpc.code", c.String()),
		// As text, since a duration is otherwise a count of nanoseconds that
		// nobody can read.
		slog.String("took", took.String()),
	}
	if err != nil {
		vs = append(vs, slog.String("error", err.Error()))
	}

	switch c {
	case codes.OK:
		l.InfoContext(ctx, "served", vs...)
	case codes.Internal, codes.Unknown, codes.DataLoss, codes.Unavailable:
		// The server is the one to blame.
		l.ErrorContext(ctx, "served", vs...)
	default:
		l.WarnContext(ctx, "served", vs...)
	}
}

// isNoise tells whether the method is polled often enough that logging it says
// nothing.
func isNoise(method string) bool {
	return strings.HasPrefix(method, "/grpc.health.v1.Health/")
}
