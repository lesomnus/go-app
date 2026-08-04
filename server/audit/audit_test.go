package audit_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/lesomnus/protobuf-patch/patch"
	"github.com/lesomnus/protobuf-patch/patchpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ox"
	"github.com/lesomnus/go-app/server/audit"
)

// of answers with the trail of one thing, newest first.
func of(ctx context.Context, x *ox.X, c *ox.Client, id []byte) []*go_app.Audit {
	x.TB().Helper()

	v, err := c.Audit().List(ctx, go_app.AuditListRequest_builder{
		Filters: []*go_app.AuditFilter{
			go_app.AuditFilter_builder{ObjectId: id}.Build(),
		},
	}.Build())
	x.NoError(err)

	return v.GetItems()
}

func TestTrail(t *testing.T) {
	t.Run("adding leaves who did it and to what", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		tenant := c.CreateTenant(ctx, x, "acme")
		v := c.CreateHolder(ctx, x, tenant.Ref(), "john")

		vs := of(ctx, x, c, v.GetId())
		x.Len(vs, 1)

		u := vs[0]
		x.Equal(go_app.HolderService_Add_FullMethodName, u.GetAction())
		x.Equal(v.GetId(), u.GetObjectId())

		// The test is served as the admin of the root tenant, and that is who
		// the trail says did it.
		x.Equal(c.Server.Admin.GetId(), u.GetActorId())
		x.Equal(c.Server.Root.GetId(), u.GetTenantId())

		// An add is said in full by the action and the identifier; there is no
		// document because there was none.
		x.Empty(u.GetPatch())
	}))

	// The action is what was asked for, and not which of the servers behind the
	// request happened to do the writing. Adding a Tenant writes two rows --
	// the Tenant, and the admin Holder that every Tenant comes with -- and both
	// of them are the one call the caller made. An RPC written by hand that
	// ends in a Patch is on the trail under its own name for the same reason.
	t.Run("the action is the request, not the leg of it that wrote", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		v := c.CreateTenant(ctx, x, "acme")

		tenant := of(ctx, x, c, v.GetId())
		x.Len(tenant, 1)
		x.Equal(go_app.TenantService_Add_FullMethodName, tenant[0].GetAction())

		admin, err := c.Holder().Get(ctx, go_app.HolderGetBySlug("admin", v.Ref()))
		x.NoError(err)

		// Nobody called HolderService/Add. `core` did, on the way down, and the
		// row says what the caller asked for -- which row it was about is the
		// identifier's to say.
		holder := of(ctx, x, c, admin.GetId())
		x.Len(holder, 1)
		x.Equal(go_app.TenantService_Add_FullMethodName, holder[0].GetAction())
		x.Equal(admin.GetId(), holder[0].GetObjectId())
	}))

	t.Run("patching keeps the document the request became", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		tenant := c.CreateTenant(ctx, x, "acme")
		v := c.CreateHolder(ctx, x, tenant.Ref(), "john")

		name := "Johnny"
		_, err := c.Holder().Patch(ctx, go_app.HolderPatchRequest_builder{
			Ref:  v.Ref(),
			Name: &name,
		}.Build())
		x.NoError(err)

		vs := of(ctx, x, c, v.GetId())
		x.Len(vs, 2)

		// Newest first.
		u := vs[0]
		x.Equal(go_app.HolderService_Patch_FullMethodName, u.GetAction())

		// Patch carries no document of its own -- the request became one on the
		// way down, below anything that could have watched for it, and that is
		// the document the trail holds.
		doc := &patchpb.Patch{}
		x.NoError(proto.Unmarshal(u.GetPatch(), doc))
		x.NotEmpty(doc.GetDelta().GetEntries())
	}))

	// The reason the trail is kept where the write happens rather than in a
	// layer in front of it. `core` normalizes an alias on the way down, so a
	// trail kept in front would say the caller wrote " JOHNNY " into a row that
	// holds "johnny" -- which is a trail that disagrees with the data it is
	// about, in the one place somebody would look to find out what the data is.
	t.Run("what is kept is what was stored, not what was asked for", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		tenant := c.CreateTenant(ctx, x, "acme")
		v := c.CreateHolder(ctx, x, tenant.Ref(), "john")

		alias := " JOHNNY "
		u, err := c.Holder().Patch(ctx, go_app.HolderPatchRequest_builder{
			Ref:   v.Ref(),
			Alias: &alias,
		}.Build())
		x.NoError(err)
		x.Equal("johnny", u.GetAlias())

		doc := &patchpb.Patch{}
		x.NoError(proto.Unmarshal(of(ctx, x, c, v.GetId())[0].GetPatch(), doc))

		// The one assignment the document carries is the alias as it was
		// stored, and not as it was written.
		es := doc.GetDelta().GetEntries()
		x.Len(es, 1)
		x.Equal("johnny", es[0].GetAssign().GetValue().GetS())
	}))

	t.Run("applying keeps the document the caller wrote", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		tenant := c.CreateTenant(ctx, x, "acme")
		v := c.CreateHolder(ctx, x, tenant.Ref(), "john")

		sent, err := patch.New("go_app.Holder",
			patch.Target(patch.Name("name")).Assign(patch.Str("Johnny")),
		)
		x.NoError(err)

		_, err = c.Holder().Apply(ctx, go_app.HolderApplyRequest_builder{
			Ref:   v.Ref(),
			Patch: sent,
		}.Build())
		x.NoError(err)

		u := of(ctx, x, c, v.GetId())[0]
		x.Equal(go_app.HolderService_Apply_FullMethodName, u.GetAction())

		got := &patchpb.Patch{}
		x.NoError(proto.Unmarshal(u.GetPatch(), got))
		x.True(proto.Equal(sent, got), "the document is kept as it was sent")
	}))

	t.Run("erasing says which one it was", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		tenant := c.CreateTenant(ctx, x, "acme")
		v := c.CreateHolder(ctx, x, tenant.Ref(), "john")

		// Named by its slug, which is not what the trail is read back with.
		_, err := c.Holder().Erase(ctx, go_app.HolderBySlug("john", tenant.Ref()))
		x.NoError(err)

		vs := of(ctx, x, c, v.GetId())
		x.Len(vs, 2)
		x.Equal(go_app.HolderService_Erase_FullMethodName, vs[0].GetAction())
		x.Equal(v.GetId(), vs[0].GetObjectId())
	}))

	t.Run("a refused request leaves nothing behind", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		acme := c.CreateTenant(ctx, x, "acme")
		hooli := c.CreateTenant(ctx, x, "hooli")
		john := c.CreateHolder(ctx, x, acme.Ref(), "john")

		before := of(ctx, x, c, john.GetId())

		// As somebody of another tenant, which the gate refuses.
		as := c.AsHolder(ctx, john)
		_, err := c.Holder().Add(as, go_app.HolderAddRequest_builder{
			Tenant: hooli.Ref(),
			Alias:  "erlich",
		}.Build())
		x.ErrCode(codes.PermissionDenied, err)

		// Nothing was written, so nothing is on the trail: it is kept by what
		// does the writing, not by what receives the request.
		x.Len(of(ctx, x, c, john.GetId()), len(before))
	}))

	t.Run("what the deployment does to itself is on it too", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		// The root tenant and its admin are made before anything is served, so
		// there is nobody they were done by.
		vs := of(ctx, x, c, c.Server.Root.GetId())
		x.Len(vs, 1)
		x.Equal(go_app.TenantService_Add_FullMethodName, vs[0].GetAction())
		x.Equal(uuid.Nil[:], vs[0].GetActorId())
		x.Equal(uuid.Nil[:], vs[0].GetTenantId())
	}))
}

func TestTrailIsNotWritable(t *testing.T) {
	// The RPCs are there so that this is expressible at all, and they answer
	// the same way to everyone, the root admin included.
	t.Run("by hand", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		_, err := c.Audit().Add(ctx, go_app.AuditAddRequest_builder{
			Action: "/go_app.HolderService/Add",
		}.Build())
		x.ErrCode(codes.Unimplemented, err)
	}))
	t.Run("afterwards", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		tenant := c.CreateTenant(ctx, x, "acme")
		u := of(ctx, x, c, tenant.GetId())[0]

		action := "something else"
		_, err := c.Audit().Patch(ctx, go_app.AuditPatchRequest_builder{
			Ref:    u.Ref(),
			Action: &action,
		}.Build())
		x.ErrCode(codes.Unimplemented, err)

		doc, err := patch.New("go_app.Audit",
			patch.Target(patch.Name("action")).Assign(patch.Str("something else")),
		)
		x.NoError(err)
		_, err = c.Audit().Apply(ctx, go_app.AuditApplyRequest_builder{
			Ref:   u.Ref(),
			Patch: doc,
		}.Build())
		x.ErrCode(codes.Unimplemented, err)

		_, err = c.Audit().Erase(ctx, u.Ref())
		x.ErrCode(codes.Unimplemented, err)

		// And it is all still there.
		x.Len(of(ctx, x, c, tenant.GetId()), 1)
	}))
}

func TestTrailIsWalled(t *testing.T) {
	t.Run("what another tenant did is not there to be seen", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		acme := c.CreateTenant(ctx, x, "acme")
		hooli := c.CreateTenant(ctx, x, "hooli")

		john := c.CreateHolder(ctx, x, acme.Ref(), "john")
		erlich := c.CreateHolder(ctx, x, hooli.Ref(), "erlich")

		// John changes something of his own, so there is a row of acme's.
		as := c.AsHolder(ctx, john)
		name := "Johnny"
		_, err := c.Holder().Patch(as, go_app.HolderPatchRequest_builder{
			Ref:  john.Ref(),
			Name: &name,
		}.Build())
		x.NoError(err)

		// Everything about John was done by the root admin except that one.
		vs, err := c.Audit().List(as, &go_app.AuditListRequest{})
		x.NoError(err)
		x.Len(vs.GetItems(), 1)
		x.Equal(go_app.HolderService_Patch_FullMethodName, vs.GetItems()[0].GetAction())

		// And erlich, of the other tenant, sees none of it.
		vs, err = c.Audit().List(c.AsHolder(ctx, erlich), &go_app.AuditListRequest{})
		x.NoError(err)
		x.Empty(vs.GetItems())
	}))

	t.Run("one row of another tenant is not there either", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		acme := c.CreateTenant(ctx, x, "acme")
		john := c.CreateHolder(ctx, x, acme.Ref(), "john")

		// Written by the root admin, so it belongs to the root tenant.
		u := of(ctx, x, c, john.GetId())[0]

		_, err := c.Audit().Get(c.AsHolder(ctx, john), u.Pick())
		x.ErrCode(codes.NotFound, err)

		// The one who wrote it can, and the whole of it comes back.
		v, err := c.Audit().Get(ctx, u.Pick())
		x.NoError(err)
		x.Equal(u.GetId(), v.GetId())
	}))

	// The wall is part of the query, so what another Tenant does cannot push a
	// caller's own trail past the end of the answer. Filtering the answer
	// instead would make this an audit control that any co-tenant can switch
	// off by writing enough rows -- and the trail would then say, with no error
	// and nothing to mark it, that nothing ever happened.
	t.Run("another tenant cannot crowd a trail out of the answer", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		acme := c.CreateTenant(ctx, x, "acme")
		john := c.CreateHolder(ctx, x, acme.Ref(), "john")

		as := c.AsHolder(ctx, john)
		name := "Johnny"
		_, err := c.Holder().Patch(as, go_app.HolderPatchRequest_builder{
			Ref:  john.Ref(),
			Name: &name,
		}.Build())
		x.NoError(err)

		// The root admin now writes more than a whole answer's worth.
		for i := range audit.ListLimit + 20 {
			c.CreateHolder(ctx, x, acme.Ref(), fmt.Sprintf("filler-%d", i))
		}

		vs, err := c.Audit().List(as, &go_app.AuditListRequest{})
		x.NoError(err)
		x.Len(vs.GetItems(), 1, "john's one row is still the whole of john's trail")
		x.Equal(go_app.HolderService_Patch_FullMethodName, vs.GetItems()[0].GetAction())
	}))

	t.Run("a row of one's own comes back whole", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		acme := c.CreateTenant(ctx, x, "acme")
		john := c.CreateHolder(ctx, x, acme.Ref(), "john")

		as := c.AsHolder(ctx, john)
		name := "Johnny"
		_, err := c.Holder().Patch(as, go_app.HolderPatchRequest_builder{
			Ref:  john.Ref(),
			Name: &name,
		}.Build())
		x.NoError(err)

		vs, err := c.Audit().List(as, &go_app.AuditListRequest{})
		x.NoError(err)
		x.Len(vs.GetItems(), 1)

		// Naming no selection asks for the row, and the row is what comes back.
		// Reading whose it is to decide that is the wall's business and not the
		// caller's, so it is the one thing taken back out.
		v, err := c.Audit().Get(as, vs.GetItems()[0].Ref().Pick())
		x.NoError(err)
		x.Equal(go_app.HolderService_Patch_FullMethodName, v.GetAction())
		x.Equal(john.GetId(), v.GetObjectId())
		x.NotEmpty(v.GetPatch())
		x.NotNil(v.GetDateCreated())
		x.Empty(v.GetTenantId())
	}))

	t.Run("asking for all of a row of one's own keeps all of it", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		acme := c.CreateTenant(ctx, x, "acme")
		john := c.CreateHolder(ctx, x, acme.Ref(), "john")

		// John does something, so the row is acme's to read.
		as := c.AsHolder(ctx, john)
		name := "Johnny"
		_, err := c.Holder().Patch(as, go_app.HolderPatchRequest_builder{
			Ref:  john.Ref(),
			Name: &name,
		}.Build())
		x.NoError(err)

		vs, err := c.Audit().List(as, &go_app.AuditListRequest{})
		x.NoError(err)
		x.Len(vs.GetItems(), 1)

		// Whose it is comes back, because `all` is asking for it -- the wall
		// reads that column to decide, and what it read is not to be taken out
		// from under a caller who said they wanted everything.
		v, err := c.Audit().Get(as, vs.GetItems()[0].Ref().Pick().
			WithSelect(func(s *go_app.AuditSelect) { s.SetAll(true) }))
		x.NoError(err)
		x.Equal(acme.GetId(), v.GetTenantId())

		// And without saying so, it does not.
		u, err := c.Audit().Get(as, vs.GetItems()[0].Pick())
		x.NoError(err)
		x.Empty(u.GetTenantId())
	}))
}
