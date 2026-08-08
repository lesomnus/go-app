package auth_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"

	"github.com/lesomnus/go-app/server/auth"
	"github.com/lesomnus/go-app/server/frame"
)

// tlsState is a connection state with the given verified chains, and the given
// certificates as what the peer merely sent.
func tlsState(verified [][]*x509.Certificate, sent ...*x509.Certificate) tls.ConnectionState {
	return tls.ConnectionState{VerifiedChains: verified, PeerCertificates: sent}
}

// incoming is a request carrying the given authorization header.
func incoming(v ...string) context.Context {
	md := metadata.MD{}
	if len(v) > 0 {
		md.Set("authorization", v...)
	}
	return metadata.NewIncomingContext(context.Background(), md)
}

// verified is a connection whose certificate this server checked. Only a chain
// the server verified is one the handler may read, which is why the test puts
// the certificate there rather than in PeerCertificates.
func verified(cert *x509.Certificate) context.Context {
	return peer.NewContext(context.Background(), &peer.Peer{
		AuthInfo: credentials.TLSInfo{State: tlsState([][]*x509.Certificate{{cert}})},
	})
}

// presented is a connection whose certificate this server did NOT verify: the
// other end sent it and nothing checked it.
func presented(cert *x509.Certificate) context.Context {
	return peer.NewContext(context.Background(), &peer.Peer{
		AuthInfo: credentials.TLSInfo{State: tlsState(nil, cert)},
	})
}

func certOf(cn string, uris ...string) *x509.Certificate {
	v := &x509.Certificate{Subject: pkix.Name{CommonName: cn}}
	for _, u := range uris {
		p, err := url.Parse(u)
		if err != nil {
			panic(err)
		}
		v.URIs = append(v.URIs, p)
	}
	return v
}

func TestPlain(t *testing.T) {
	x := require.New(t)

	v, err := auth.Plain().Handle(incoming("Plain anna"))
	x.NoError(err)
	x.Equal(auth.MethodPlain, v.Method)
	x.Equal("anna", v.Actor.Subject)
	x.True(v.Grant.IsWhole(), "a header has nowhere to carry an attenuation")

	// Nothing said is not the same as something wrong.
	_, err = auth.Plain().Handle(incoming())
	x.ErrorIs(err, auth.ErrNoCredential)

	_, err = auth.Plain().Handle(incoming("Plain "))
	x.Error(err)
	x.NotErrorIs(err, auth.ErrNoCredential)
}

func TestMTLS(t *testing.T) {
	t.Run("the name is read from the verified chain", func(t *testing.T) {
		x := require.New(t)

		v, err := auth.MTLS().Handle(verified(certOf("anna")))
		x.NoError(err)
		x.Equal(auth.MethodMTLS, v.Method)
		x.Equal("anna", v.Actor.Subject)
	})

	t.Run("a URI is preferred over the common name", func(t *testing.T) {
		x := require.New(t)

		v, err := auth.MTLS().Handle(verified(certOf("bill", "spiffe://example.com/ns/prod/sa/anna")))
		x.NoError(err)
		x.Equal("ns/prod/sa/anna", v.Actor.Subject)
	})

	// The one that matters: what the peer sent is not what this server
	// checked, and only the second is a claim about anybody.
	t.Run("a certificate nobody verified says nothing", func(t *testing.T) {
		x := require.New(t)

		_, err := auth.MTLS().Handle(presented(certOf("anna")))
		x.ErrorIs(err, auth.ErrNoCredential)
	})

	t.Run("no connection, no TLS, no name", func(t *testing.T) {
		x := require.New(t)

		_, err := auth.MTLS().Handle(context.Background())
		x.ErrorIs(err, auth.ErrNoCredential)

		_, err = auth.MTLS().Handle(peer.NewContext(context.Background(), &peer.Peer{}))
		x.ErrorIs(err, auth.ErrNoCredential)

		// Verified, and carrying no name at all: another handler may know them.
		_, err = auth.MTLS().Handle(verified(certOf("")))
		x.ErrorIs(err, auth.ErrNoCredential)
	})

	// There is no such thing as a name that is not one. A subject is whatever
	// the issuer calls somebody, and nothing here reads it -- which is why the
	// check that used to be here is gone rather than moved.
	t.Run("whatever the certificate says is who they are", func(t *testing.T) {
		x := require.New(t)

		v, err := auth.MTLS().Handle(verified(certOf("Acme Corporation, Inc.")))
		x.NoError(err)
		x.Equal("Acme Corporation, Inc.", v.Actor.Subject)
	})
}

func TestBearer(t *testing.T) {
	anna := frame.Actor{Subject: "anna"}

	store := func(t *testing.T) *auth.MemTokenStore {
		t.Helper()
		s := auth.NewMemTokenStore()
		s.Add("s3cret", anna, frame.Whole(), time.Time{})
		return s
	}

	t.Run("a token is exchanged for a name", func(t *testing.T) {
		x := require.New(t)

		v, err := auth.Bearer(store(t)).Handle(incoming("Bearer s3cret"))
		x.NoError(err)
		x.Equal(auth.MethodBearer, v.Method)
		x.Equal(anna, v.Actor)
	})

	t.Run("the token is never in the answer", func(t *testing.T) {
		x := require.New(t)

		v, err := auth.Bearer(store(t)).Handle(incoming("Bearer s3cret"))
		x.NoError(err)
		x.NotContains(v.Method, "s3cret")

		// Nor in what is said when it is refused.
		_, err = auth.Bearer(store(t)).Handle(incoming("Bearer wrong-one"))
		x.Error(err)
		x.NotContains(err.Error(), "wrong-one")
	})

	t.Run("no token said is not a bad token", func(t *testing.T) {
		x := require.New(t)

		_, err := auth.Bearer(store(t)).Handle(incoming())
		x.ErrorIs(err, auth.ErrNoCredential)

		// A scheme this handler does not read is also nothing said.
		_, err = auth.Bearer(store(t)).Handle(incoming("Plain anna"))
		x.ErrorIs(err, auth.ErrNoCredential)
	})

	t.Run("an unknown token is refused and does not fall through", func(t *testing.T) {
		x := require.New(t)

		_, err := auth.Bearer(store(t)).Handle(incoming("Bearer nope"))
		x.ErrorIs(err, auth.ErrUnknownToken)
		x.NotErrorIs(err, auth.ErrNoCredential)
		x.NotErrorIs(err, auth.ErrUnavailable)
	})

	// A token has a life of its own, which a header and a certificate do not.
	t.Run("an expired token is not honoured", func(t *testing.T) {
		x := require.New(t)

		at := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
		s := auth.NewMemTokenStore()
		s.Now = func() time.Time { return at }
		s.Add("s3cret", anna, frame.Whole(), at.Add(time.Hour))

		_, err := auth.Bearer(s).Handle(incoming("Bearer s3cret"))
		x.NoError(err)

		s.Now = func() time.Time { return at.Add(time.Hour) }
		_, err = auth.Bearer(s).Handle(incoming("Bearer s3cret"))
		x.ErrorIs(err, auth.ErrUnknownToken)
	})

	// Told apart, "expired" and "never existed" are an oracle: a guesser learns
	// that a string was a real token once, which is the hard half of having one.
	t.Run("expired and unknown are the same no", func(t *testing.T) {
		x := require.New(t)

		at := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
		s := auth.NewMemTokenStore()
		s.Now = func() time.Time { return at.Add(time.Hour) }
		s.Add("was-real", anna, frame.Whole(), at)

		_, expired := auth.Bearer(s).Handle(incoming("Bearer was-real"))
		_, unknown := auth.Bearer(s).Handle(incoming("Bearer never-was"))
		x.ErrorIs(expired, auth.ErrUnknownToken)
		x.ErrorIs(unknown, auth.ErrUnknownToken)
		x.Equal(unknown.Error(), expired.Error())
	})

	// What the store said about a bad token stays with the store; what it said
	// about itself does not.
	t.Run("only unavailable is passed on", func(t *testing.T) {
		x := require.New(t)

		chatty := auth.TokenStoreFunc(func(context.Context, string) (frame.Actor, frame.Grant, error) {
			return frame.Anonymous, frame.Grant{}, fmt.Errorf("row 41 of tokens: revoked by admin on tuesday")
		})
		_, err := auth.Bearer(chatty).Handle(incoming("Bearer s3cret"))
		x.ErrorIs(err, auth.ErrUnknownToken)
		x.NotContains(err.Error(), "tuesday")

		down := auth.TokenStoreFunc(func(context.Context, string) (frame.Actor, frame.Grant, error) {
			return frame.Anonymous, frame.Grant{}, fmt.Errorf("dial tcp: %w", auth.ErrUnavailable)
		})
		_, err = auth.Bearer(down).Handle(incoming("Bearer s3cret"))
		x.ErrorIs(err, auth.ErrUnavailable)
		x.Contains(err.Error(), "dial tcp")
	})

	t.Run("a forgotten token is a revoked one", func(t *testing.T) {
		x := require.New(t)

		s := store(t)
		s.Remove("s3cret")
		x.Equal(0, s.Len())

		_, err := auth.Bearer(s).Handle(incoming("Bearer s3cret"))
		x.ErrorIs(err, auth.ErrUnknownToken)
	})

	t.Run("a store that cannot answer says so", func(t *testing.T) {
		x := require.New(t)

		down := auth.TokenStoreFunc(func(context.Context, string) (frame.Actor, frame.Grant, error) {
			return frame.Anonymous, frame.Grant{}, auth.ErrUnavailable
		})

		_, err := auth.Bearer(down).Handle(incoming("Bearer s3cret"))
		x.ErrorIs(err, auth.ErrUnavailable)
		x.NotErrorIs(err, auth.ErrNoCredential)
	})
}

func TestSeq(t *testing.T) {
	anna := frame.Actor{Subject: "anna"}
	cert := certOf("bill")

	// The stack the configuration builds for `methods: [bearer, mtls]`, on a
	// connection that has a certificate.
	fallback := func(store auth.TokenStore) auth.Handler {
		return auth.Seq(auth.Bearer(store), auth.MTLS())
	}
	with := func(header string) context.Context {
		ctx := verified(cert)
		md := metadata.MD{}
		if header != "" {
			md.Set("authorization", header)
		}
		return metadata.NewIncomingContext(ctx, md)
	}

	good := auth.NewMemTokenStore()
	good.Add("s3cret", anna, frame.Whole(), time.Time{})

	t.Run("the token answers when there is one", func(t *testing.T) {
		x := require.New(t)

		v, err := fallback(good).Handle(with("Bearer s3cret"))
		x.NoError(err)
		x.Equal(auth.MethodBearer, v.Method)
		x.Equal(anna, v.Actor)
	})

	t.Run("the certificate answers when there is not", func(t *testing.T) {
		x := require.New(t)

		v, err := fallback(good).Handle(with(""))
		x.NoError(err)
		x.Equal(auth.MethodMTLS, v.Method)
		x.Equal("bill", v.Actor.Subject)
	})

	// The point of the whole arrangement: a bad token must not quietly become
	// somebody else. The certificate on this connection names a different
	// Holder, and answering as them would be answering a question nobody asked.
	t.Run("a bad token does not fall through to the certificate", func(t *testing.T) {
		x := require.New(t)

		_, err := fallback(good).Handle(with("Bearer nope"))
		x.ErrorIs(err, auth.ErrUnknownToken)
	})

	t.Run("a store that is down does not fall through either", func(t *testing.T) {
		x := require.New(t)

		down := auth.TokenStoreFunc(func(context.Context, string) (frame.Actor, frame.Grant, error) {
			return frame.Anonymous, frame.Grant{}, auth.ErrUnavailable
		})

		_, err := fallback(down).Handle(with("Bearer s3cret"))
		x.ErrorIs(err, auth.ErrUnavailable)
	})

	t.Run("nothing at all is nothing said", func(t *testing.T) {
		x := require.New(t)

		_, err := auth.Seq(auth.Bearer(good), auth.MTLS()).Handle(incoming())
		x.ErrorIs(err, auth.ErrNoCredential)

		// And no handler at all is the same.
		_, err = auth.Seq().Handle(incoming("Bearer s3cret"))
		x.ErrorIs(err, auth.ErrNoCredential)
	})
}
