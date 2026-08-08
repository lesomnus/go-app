package frame

import "slices"

// Grant is what a credential allows, which is at most what the Actor it names
// allows.
//
// It is an attenuation and never a widening: a credential cannot let its bearer
// do what the subject could not. Whatever decides what the subject may do --
// `server/gate`, and whatever a deployment injects into it -- runs as it always
// did, and this narrows the answer afterwards.
//
// One axis, because a credential has one honest thing to say: which of the
// calls its subject may make it is for. A permission set that varied per
// resource would be a policy, and a policy is not something a credential should
// be carrying around; GitHub's fine-grained tokens do not do it either.
//
// **The zero value allows nothing.** A store that answers with a Grant it
// forgot to fill in hands out a credential that can do nothing, which somebody
// notices immediately; the other way round it hands out one that can do
// everything, which nobody notices at all.
type Grant struct {
	any     bool
	actions []string
}

// Whole is a credential that narrows nothing: it allows whatever the subject it
// names allows. A header and a certificate are always this, since neither has
// anywhere to carry an attenuation.
func Whole() Grant {
	return Grant{any: true}
}

// To narrows a Grant to the given methods, by the name gRPC knows them by --
// "/go_app.CoffeeService/Get". Naming none allows none.
func To(vs ...string) Grant {
	return Grant{actions: slices.Clip(slices.Clone(vs))}
}

// IsWhole reports whether this narrows nothing at all.
func (g Grant) IsWhole() bool { return g.any }

// Actions is what this narrows to, and says nothing when [Grant.IsWhole].
func (g Grant) Actions() []string { return g.actions }

// Allows reports whether the given method is one this credential may be used
// for.
func (g Grant) Allows(method string) bool {
	if g.any {
		return true
	}

	return slices.Contains(g.actions, method)
}
