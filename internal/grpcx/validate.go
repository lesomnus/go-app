package grpcx

import (
	"context"
	"errors"

	"buf.build/go/protovalidate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Validate checks every request against what its own definition says about it.
//
// The constraints are `buf.validate` options in the proto, next to the fields
// they are about, so a field that is added carries its rule with it and no
// server has to be told. What is left for `server/core` is what a declaration
// cannot say: normalizing a value on the way in, a rule about two fields at
// once, and anything that has to ask the database.
//
// It only sees what a request carries, which is not a gap here so much as the
// shape of this app. `Patch` and `Apply` do not carry fields -- an Apply that
// writes an alias is a document, not an `alias` on a request, and nothing
// looking at requests can see it. That is not a hole to plug with a stricter
// checker: they are not an API a caller gets to use. See the README, "The
// general write is not an API". What a caller does use is either generated
// CRUD, whose fields are right here, or an RPC somebody wrote by hand -- and a
// hand-written RPC has a hand-written request message, which is exactly where a
// constraint goes.
func Validate() ([]grpc.ServerOption, error) {
	v, err := protovalidate.New()
	if err != nil {
		return nil, err
	}

	return []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(ValidateUnary(v)),
	}, nil
}

func ValidateUnary(v protovalidate.Validator) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		m, ok := req.(proto.Message)
		if !ok {
			return handler(ctx, req)
		}

		if err := v.Validate(m); err != nil {
			// A violation is the request's fault and the caller can fix it, so
			// it is said in the caller's terms. Anything else means the
			// constraints themselves do not compile, which no request can
			// correct -- and which a deployment finds out about at startup,
			// since [Validate] builds the validator there.
			var violations *protovalidate.ValidationError
			if errors.As(err, &violations) {
				return nil, status.Error(codes.InvalidArgument, err.Error())
			}

			return nil, status.Errorf(codes.Internal, "validate the request: %s", err)
		}

		return handler(ctx, req)
	}
}
