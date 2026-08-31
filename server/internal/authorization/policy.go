// Package authorization holds the explicit access decisions for Market Lens. Keeping them in
// one place makes the whole policy readable and testable as a table, rather than leaving it
// implied by scattered conditionals in handlers and queries.
package authorization

import "errors"

// Scope classifies what a resource is, which is what determines who may read it.
type Scope string

const (
	// ScopeShared is reference and market data every active user collaborates on.
	ScopeShared Scope = "shared"
	// ScopeUser is one person's private financial activity.
	ScopeUser Scope = "user"
	// ScopeOwner is account and security administration metadata.
	ScopeOwner Scope = "owner"
)

// Role is the caller's persisted role.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleMember Role = "member"
)

// Principal is the authenticated caller. It is built from the session and the persisted user
// record, never from anything the client supplies.
type Principal struct {
	UserID        string
	Role          Role
	Deactivated   bool
	Authenticated bool
}

// Resource is the thing being reached. OwnerUserID identifies whose private data it is and is
// meaningful only for ScopeUser.
type Resource struct {
	Scope       Scope
	OwnerUserID string
}

// Reason explains a decision. It is for logs and tests; it is never sent to a client, because
// telling an unauthorized caller why they were refused is itself a disclosure.
type Reason string

const (
	ReasonAnonymous   Reason = "anonymous"
	ReasonDeactivated Reason = "deactivated"
	ReasonNotOwner    Reason = "not_owner"
	ReasonNotSubject  Reason = "not_subject"
	ReasonUnknown     Reason = "unknown_scope"
	ReasonAllowed     Reason = "allowed"
)

// ErrDenied is returned by Require for any refused access.
var ErrDenied = errors.New("access denied")

// Decision is the outcome of one authorization question.
type Decision struct {
	Allowed bool
	Reason  Reason
}

// Authorize answers whether principal may reach resource. It fails closed: an unrecognised
// scope is refused rather than defaulting to readable.
func Authorize(principal Principal, resource Resource) Decision {
	if !principal.Authenticated || principal.UserID == "" {
		return Decision{Reason: ReasonAnonymous}
	}
	if principal.Deactivated {
		return Decision{Reason: ReasonDeactivated}
	}
	switch resource.Scope {
	case ScopeShared:
		return Decision{Allowed: true, Reason: ReasonAllowed}
	case ScopeUser:
		// The owner administers access, not other people's research. Ownership of the instance
		// deliberately does not grant a window into another member's private activity.
		if resource.OwnerUserID != "" && resource.OwnerUserID == principal.UserID {
			return Decision{Allowed: true, Reason: ReasonAllowed}
		}
		return Decision{Reason: ReasonNotSubject}
	case ScopeOwner:
		if principal.Role == RoleOwner {
			return Decision{Allowed: true, Reason: ReasonAllowed}
		}
		return Decision{Reason: ReasonNotOwner}
	default:
		return Decision{Reason: ReasonUnknown}
	}
}

// Require is the error-returning form of Authorize for use in services.
func Require(principal Principal, resource Resource) error {
	if Authorize(principal, resource).Allowed {
		return nil
	}
	return ErrDenied
}

// PrivateScopeFor builds the resource describing one user's private data.
func PrivateScopeFor(userID string) Resource {
	return Resource{Scope: ScopeUser, OwnerUserID: userID}
}
