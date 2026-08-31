package authorization_test

import (
	"errors"
	"testing"

	"market-lens/server/internal/authorization"
)

func TestAuthorizationPolicyTable(t *testing.T) {
	const (
		self  = "10000000-0000-4000-8000-000000000001"
		other = "10000000-0000-4000-8000-000000000002"
	)
	anonymous := authorization.Principal{}
	member := authorization.Principal{UserID: self, Role: authorization.RoleMember, Authenticated: true}
	otherMember := authorization.Principal{UserID: other, Role: authorization.RoleMember, Authenticated: true}
	owner := authorization.Principal{UserID: other, Role: authorization.RoleOwner, Authenticated: true}
	deactivated := authorization.Principal{UserID: self, Role: authorization.RoleMember, Authenticated: true, Deactivated: true}

	shared := authorization.Resource{Scope: authorization.ScopeShared}
	ownScope := authorization.PrivateScopeFor(self)
	ownerScope := authorization.Resource{Scope: authorization.ScopeOwner}

	tests := []struct {
		name      string
		principal authorization.Principal
		resource  authorization.Resource
		want      bool
		reason    authorization.Reason
	}{
		{"anonymous cannot read shared data", anonymous, shared, false, authorization.ReasonAnonymous},
		{"anonymous cannot read private data", anonymous, ownScope, false, authorization.ReasonAnonymous},
		{"anonymous cannot read owner data", anonymous, ownerScope, false, authorization.ReasonAnonymous},

		{"active member reads shared data", member, shared, true, authorization.ReasonAllowed},
		{"active member reads their own private data", member, ownScope, true, authorization.ReasonAllowed},
		{"active member cannot read another member's private data", otherMember, ownScope, false, authorization.ReasonNotSubject},
		{"active member cannot read owner administration", member, ownerScope, false, authorization.ReasonNotOwner},

		{"owner reads shared data", owner, shared, true, authorization.ReasonAllowed},
		{"owner reads owner administration", owner, ownerScope, true, authorization.ReasonAllowed},
		// Owning the instance grants administration of access, never a window into another
		// person's private financial activity.
		{"owner cannot read a member's private data", owner, ownScope, false, authorization.ReasonNotSubject},
		{"owner reads their own private data", owner, authorization.PrivateScopeFor(other), true, authorization.ReasonAllowed},

		{"deactivated member cannot read shared data", deactivated, shared, false, authorization.ReasonDeactivated},
		{"deactivated member cannot read their own private data", deactivated, ownScope, false, authorization.ReasonDeactivated},
		{"deactivated member cannot read owner administration", deactivated, ownerScope, false, authorization.ReasonDeactivated},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := authorization.Authorize(test.principal, test.resource)
			if decision.Allowed != test.want || decision.Reason != test.reason {
				t.Fatalf("Authorize = %#v, want allowed=%v reason=%q", decision, test.want, test.reason)
			}
			err := authorization.Require(test.principal, test.resource)
			if test.want && err != nil {
				t.Fatalf("Require = %v, want nil", err)
			}
			if !test.want && !errors.Is(err, authorization.ErrDenied) {
				t.Fatalf("Require = %v, want ErrDenied", err)
			}
		})
	}
}

func TestAuthorizationFailsClosedForUnknownAndMalformedInput(t *testing.T) {
	principal := authorization.Principal{
		UserID: "10000000-0000-4000-8000-000000000001",
		Role:   authorization.RoleOwner, Authenticated: true,
	}
	// An unrecognised scope must be refused rather than treated as readable.
	if decision := authorization.Authorize(principal, authorization.Resource{Scope: "portfolio"}); decision.Allowed {
		t.Fatalf("unknown scope = %#v, want denied", decision)
	}
	// A private resource with no subject belongs to nobody and is readable by nobody.
	if decision := authorization.Authorize(principal, authorization.Resource{Scope: authorization.ScopeUser}); decision.Allowed {
		t.Fatalf("ownerless private resource = %#v, want denied", decision)
	}
	// An authenticated flag without a subject is not a principal.
	if decision := authorization.Authorize(authorization.Principal{Authenticated: true, Role: authorization.RoleOwner},
		authorization.Resource{Scope: authorization.ScopeShared}); decision.Allowed {
		t.Fatalf("subjectless principal = %#v, want denied", decision)
	}
	// A client cannot escalate by claiming a role it does not hold: the role comes from the
	// persisted record, so a member principal is refused owner scope even if it claims owner.
	forged := authorization.Principal{UserID: "10000000-0000-4000-8000-000000000009", Role: "Owner", Authenticated: true}
	if decision := authorization.Authorize(forged, authorization.Resource{Scope: authorization.ScopeOwner}); decision.Allowed {
		t.Fatalf("case-variant role = %#v, want denied", decision)
	}
}
