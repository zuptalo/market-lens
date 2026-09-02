package api

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// contractPaths are the reviewed API contracts. Reconciling against the files the
// specifications own keeps the two from drifting silently in either direction. Each feature
// keeps its own contract, so this is a list rather than one path.
var contractPaths = []string{
	"../../../specs/004-owner-access/contracts/openapi.yaml",
	"../../../specs/005-instrument-exploration/contracts/openapi.yaml",
	"../../../specs/013-feature-engine/contracts/openapi.yaml",
	"../../../specs/014-market-data-navigation/contracts/openapi.yaml",
	"../../../specs/015-strategies-and-signals/contracts/openapi.yaml",
	"../../../specs/016-rolling-reobservation/contracts/openapi.yaml",
}

// boundaryContractPath is the contract that declares the deny-by-default access boundary for
// the whole API. It is feature 004's, because that is the feature that established it.
const boundaryContractPath = "../../../specs/004-owner-access/contracts/openapi.yaml"

// routerSource is parsed rather than probed because Go's ServeMux does not enumerate its own
// routes, and a route registered but undocumented is exactly the drift worth catching.
const routerSource = "router.go"

var (
	contractPathLine  = regexp.MustCompile(`^  (/\S*?):\s*$`)
	contractMethod    = regexp.MustCompile(`^    (get|post|put|patch|delete):\s*$`)
	registeredPattern = regexp.MustCompile(`"(GET|POST|PUT|PATCH|DELETE) (/api/v1/[^"]*)"`)
	pathParameter     = regexp.MustCompile(`\{[^}]+\}`)
)

// normalize reduces both sources to one comparable shape: METHOD /path with every path
// parameter written as {}, because the contract and the router name them differently.
func normalize(method, path string) string {
	path = strings.TrimPrefix(path, "/api/v1")
	if path == "" {
		path = "/"
	}
	return strings.ToUpper(method) + " " + pathParameter.ReplaceAllString(path, "{}")
}

// contractOperations merges every reviewed contract into one operation set. The sanity guard
// below applies to that union rather than to each file, because a per-file guard would make
// any small contract fail as a harness error — and a harness error is not evidence of
// anything. A feature is entitled to document two operations.
func contractOperations(t *testing.T) map[string]bool {
	t.Helper()
	operations := map[string]bool{}
	for _, path := range contractPaths {
		contents, err := os.ReadFile(filepath.FromSlash(path))
		if err != nil {
			t.Fatalf("read the API contract %s: %v", path, err)
		}
		current := ""
		inPaths := false
		for _, line := range strings.Split(string(contents), "\n") {
			if line == "paths:" {
				inPaths = true
				continue
			}
			if !inPaths {
				continue
			}
			// A top-level key ends the paths block; without this a later section's keys
			// would be read as operations once more than one contract is registered.
			if line != "" && line[0] != ' ' && line[0] != '#' {
				inPaths = false
				current = ""
				continue
			}
			if match := contractPathLine.FindStringSubmatch(line); match != nil {
				current = match[1]
				continue
			}
			if match := contractMethod.FindStringSubmatch(line); match != nil && current != "" {
				operations[normalize(match[1], current)] = true
			}
		}
	}
	if len(operations) < 10 {
		t.Fatalf("parsed only %d operations from %d contracts, so this comparison proves nothing",
			len(operations), len(contractPaths))
	}
	return operations
}

func registeredOperations(t *testing.T) map[string]bool {
	t.Helper()
	contents, err := os.ReadFile(routerSource)
	if err != nil {
		t.Fatal(err)
	}
	operations := map[string]bool{}
	for _, match := range registeredPattern.FindAllStringSubmatch(string(contents), -1) {
		operations[normalize(match[1], match[2])] = true
	}
	if len(operations) < 10 {
		t.Fatalf("parsed only %d routes from the router, so this comparison proves nothing",
			len(operations))
	}
	return operations
}

func TestEveryImplementedAPIRouteIsDocumentedAndEveryDocumentedRouteExists(t *testing.T) {
	contract := contractOperations(t)
	registered := registeredOperations(t)

	// Liveness and readiness are named in the access boundary rather than as operations, and
	// the retired recovery endpoints exist only to answer 404. Nothing else is exempt.
	exempt := map[string]bool{
		"GET /health": true, "GET /ready": true,
		"POST /auth/owner/recovery/request": true, "POST /auth/owner/recovery/complete": true,
	}
	// Routes belonging to earlier features keep their own contracts.
	// GET /instruments is no longer inherited: feature 005's contract documents it, so
	// exempting it would keep the test silent about the very endpoint that feature changed.
	inherited := map[string]bool{
		"GET /instruments/{}": true, "GET /instruments/{}/prices": true,
		"GET /market-data/imports": true, "GET /market-data/imports/{}": true,
		"GET /market-data/quality-findings": true,
	}

	var undocumented, unimplemented []string
	for operation := range registered {
		if !contract[operation] && !exempt[operation] && !inherited[operation] {
			undocumented = append(undocumented, operation)
		}
	}
	for operation := range contract {
		if !registered[operation] && !exempt[operation] {
			unimplemented = append(unimplemented, operation)
		}
	}
	sort.Strings(undocumented)
	sort.Strings(unimplemented)
	if len(undocumented) > 0 {
		t.Errorf("routes exist but the contract does not describe them: %v", undocumented)
	}
	if len(unimplemented) > 0 {
		t.Errorf("the contract describes routes that do not exist: %v", unimplemented)
	}
}

func TestTheContractStillDeclaresTheAccessBoundaryItPromises(t *testing.T) {
	contents, err := os.ReadFile(filepath.FromSlash(boundaryContractPath))
	if err != nil {
		t.Fatal(err)
	}
	document := string(contents)
	// The boundary is the security claim the whole feature rests on; losing it from the
	// contract would quietly turn deny-by-default into a convention.
	for _, required := range []string{
		"x-market-lens-access-boundary",
		"protected by default",
	} {
		if !strings.Contains(document, required) {
			t.Errorf("the contract no longer declares %q", required)
		}
	}
	// A retired endpoint must not reappear as a documented operation.
	for _, retired := range []string{"/auth/owner/recovery/request", "/auth/owner/recovery/complete"} {
		if strings.Contains(document, "  "+retired+":") {
			t.Errorf("the contract documents retired endpoint %s", retired)
		}
	}
}
