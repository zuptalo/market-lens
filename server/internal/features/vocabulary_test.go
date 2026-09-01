package features_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// SC-003a: the universe composite is a mean of one curated list. Calling it an index or a
// benchmark would claim a relationship to something it has none with, so neither word may
// name it on any surface a person reads: the API and engine strings, the schema's own
// documentation, the front end's copy, and this feature's specification.
//
// Three uses of the words are legitimate and stay legitimate, so the check names them
// exactly rather than ignoring whole files: the relative strength index, which is the
// published name of a definition; a database index; and a sentence that denies the label,
// which is how the rule itself is written down.
var (
	forbiddenVocabulary = regexp.MustCompile(`(?i)\b(index|indexes|indices|benchmark|benchmarks|benchmarked)\b`)
	relativeStrength    = regexp.MustCompile(`(?i)relative strength index`)
	// A sentence is about a database index when it also talks about the database: a table, a
	// column, a migration, a query plan.
	databaseIndex = regexp.MustCompile(`(?i)create index|drop index|_idx|\b(feature_values|feature_runs|daily_price_bars|definition_id|session_date|instrument_id|migration|schema|postgres(ql)?|query|queries|listing|column|table|\.sql|jsonb|long format|wide (row|table))\b|\.sql`)
	denial        = regexp.MustCompile(`(?i)\b(never|not|neither|nor|no|without)\b`)
)

// namesTheCompositeWrongly reports whether one fragment of user-facing text uses one of the
// words to name something, rather than to deny the name or to talk about a database index.
func namesTheCompositeWrongly(fragment string) bool {
	if !forbiddenVocabulary.MatchString(fragment) {
		return false
	}
	cleaned := relativeStrength.ReplaceAllString(fragment, "")
	if !forbiddenVocabulary.MatchString(cleaned) {
		return false
	}
	// The whole fragment is the context, not the sentence: prose puts the denial and the
	// storage it is talking about in neighbouring sentences, and a fragment is one thought —
	// a paragraph of documentation, a single string, one line of copy.
	return !databaseIndex.MatchString(cleaned) && !denial.MatchString(cleaned)
}

func TestNoSurfaceCallsTheCompositeAnIndexOrABenchmark(t *testing.T) {
	t.Run("the check knows the difference", func(t *testing.T) {
		wrong := []string{
			"The universe index rose 0.4% today",
			"Compared against the Nordic benchmark",
			"benchmark return for the session",
			"shown next to the sector index",
		}
		right := []string{
			"the long format indexes for perfectly well on (instrument_id, session_date)",
			"add the index to 0019_markets_adopt_engine_statistics.sql",
			"RSI is Wilder's relative strength index over a fixed window of closes",
			"CREATE INDEX feature_values_definition_session_idx ON feature_values (definition_id, session_date)",
			"reserved for the index the Markets listing needs once it reads its three statistics",
			"It is a composite of one curated list, never an index or a benchmark",
			"the composite is not an index",
			"the equal-weighted mean return of the universe",
		}
		for _, fragment := range wrong {
			if !namesTheCompositeWrongly(fragment) {
				t.Errorf("%q was accepted", fragment)
			}
		}
		for _, fragment := range right {
			if namesTheCompositeWrongly(fragment) {
				t.Errorf("%q was rejected", fragment)
			}
		}
	})

	root := filepath.Join("..", "..", "..")
	surfaces := []string{
		filepath.Join("server", "internal", "features"),
		filepath.Join("server", "internal", "api", "features.go"),
		filepath.Join("server", "internal", "db", "migrations", "0017_feature_definitions.sql"),
		filepath.Join("server", "internal", "db", "migrations", "0018_feature_values.sql"),
		filepath.Join("server", "internal", "db", "migrations", "0019_markets_adopt_engine_statistics.sql"),
		"src",
		filepath.Join("specs", "013-feature-engine"),
	}
	scanned := 0
	for _, surface := range surfaces {
		err := filepath.WalkDir(filepath.Join(root, surface), func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			// This test quotes the very phrases it forbids, which is the one file that must
			// be read as a description of the rule rather than as a surface.
			if filepath.Base(path) == "vocabulary_test.go" {
				return nil
			}
			fragments, ok := userFacingText(t, path)
			if !ok {
				return nil
			}
			scanned++
			for number, fragment := range fragments {
				if namesTheCompositeWrongly(fragment) {
					t.Errorf("%s:%d calls something an index or a benchmark: %q", path, number+1, strings.TrimSpace(fragment))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", surface, err)
		}
	}
	if scanned < 30 {
		t.Fatalf("scanned only %d files; the surfaces are not being read", scanned)
	}
}

var (
	goString    = regexp.MustCompile(`"(?:[^"\\\n]|\\.)*"`)
	sqlString   = regexp.MustCompile(`'(?:[^'\n]|'')*'`)
	sqlComment  = regexp.MustCompile(`--.*$`)
	frontString = regexp.MustCompile("\"[^\"\\n]* [^\"\\n]*\"|'[^'\\n]* [^'\\n]*'|`[^`\\n]* [^`\\n]*`")
	// ${...} inside a template literal is code, not copy.
	interpolation = regexp.MustCompile(`\$\{[^}]*\}`)
)

// userFacingText returns the parts of a file a person can read: prose in documentation, the
// text a Go or SQL surface can emit, and the copy the front end renders. Identifiers are
// deliberately left out — a loop variable named index is not a claim about the composite.
func userFacingText(t *testing.T, path string) (map[int]string, bool) {
	t.Helper()
	extension := filepath.Ext(path)
	switch extension {
	case ".go", ".sql", ".md", ".ts", ".vue":
	default:
		return nil, false
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	fragments := map[int]string{}
	inScript := false
	paragraph, paragraphStart := "", 0
	for number, line := range strings.Split(string(body), "\n") {
		switch extension {
		case ".md":
			// Markdown is read a paragraph at a time: a denial and the word it denies are
			// one thought, and a line break between them does not make them two.
			if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "- ") ||
				strings.HasPrefix(strings.TrimSpace(line), "| ") {
				paragraph, paragraphStart = "", number
			}
			paragraph += " " + line
			fragments[paragraphStart] = paragraph
			delete(fragments, number)
		case ".go":
			fragments[number] = strings.Join(goString.FindAllString(line, -1), " ")
		case ".sql":
			fragments[number] = strings.Join(append(sqlString.FindAllString(line, -1), sqlComment.FindAllString(line, -1)...), " ")
		case ".ts":
			fragments[number] = interpolation.ReplaceAllString(strings.Join(frontString.FindAllString(line, -1), " "), "")
		case ".vue":
			// A .vue file is copy outside its script block and code inside it.
			if strings.HasPrefix(strings.TrimSpace(line), "<script") {
				inScript = true
			}
			if strings.HasPrefix(strings.TrimSpace(line), "</script") {
				inScript = false
			}
			if inScript {
				fragments[number] = interpolation.ReplaceAllString(strings.Join(frontString.FindAllString(line, -1), " "), "")
			} else {
				fragments[number] = line
			}
		}
	}
	return fragments, true
}
