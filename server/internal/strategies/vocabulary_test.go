package strategies_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// SC-006: a signal is a strategy's stated view, never advice. The vocabulary is where that
// position is kept or lost, and it is lost gradually — one helpful-sounding phrase at a time,
// each defensible on its own, until the product is telling people what to buy.
//
// So the words are forbidden on the surfaces a person reads, and the one legitimate use is
// denying them: "this is not advice" must remain sayable, because the rule has to be written
// down somewhere and the interface has to state it.
var (
	adviceVocabulary = regexp.MustCompile(`(?i)\b(recommend(s|ed|ation|ations)?|advice|advise[sd]?|profit(s|able)?|you should (buy|sell|hold)|will (rise|fall|go up|go down)|sure thing|can'?t lose|best (stock|pick)s?)\b|(?i)\bguarantee(s|d)?\s+(a|an|the)?\s*(return|profit|gain|outcome|result)`)
	// A denial anywhere in the fragment makes it a statement about what the product is not.
	// "Guarantee" on its own is left out deliberately: an ordering guarantee and a delivery
	// guarantee are engineering terms, and forbidding the word would push this test toward
	// being switched off rather than toward better copy.
	adviceDenial = regexp.MustCompile(`(?i)\b(never|nothing|not|neither|nor|no|without|is not|are not|isn'?t|aren'?t|does not|doesn'?t|cannot|can'?t)\b|(?i)\brather\s+than\b|(?i)\binstead\s+of\b`)
)

func offersAdvice(fragment string) bool {
	if !adviceVocabulary.MatchString(fragment) {
		return false
	}
	return !adviceDenial.MatchString(fragment)
}

func TestNoSurfaceCallsASignalAdvice(t *testing.T) {
	t.Run("the check knows the difference", func(t *testing.T) {
		wrong := []string{
			"We recommend buying this instrument",
			"Our recommendation for the session",
			"This score guarantees a profit next quarter",
			"the strategy's best picks for today",
			"you should buy the top-ranked instrument",
		}
		right := []string{
			"A signal is not advice, and this screen says so",
			"The product never recommends a trade",
			"This is a stated view, not a recommendation",
			"Market Lens does not offer investment advice",
			"the strategy scored this instrument, rather than recommending it",
			"no signal guarantees a return of any kind",
			"the ordering guarantee is what makes the pass reproducible",
		}
		for _, fragment := range wrong {
			if !offersAdvice(fragment) {
				t.Errorf("%q was accepted", fragment)
			}
		}
		for _, fragment := range right {
			if offersAdvice(fragment) {
				t.Errorf("%q was rejected", fragment)
			}
		}
	})

	root := filepath.Join("..", "..", "..")
	surfaces := []string{
		filepath.Join("server", "internal", "strategies"),
		filepath.Join("server", "internal", "db", "migrations", "0021_strategies_and_signals.sql"),
		"src",
		filepath.Join("specs", "015-strategies-and-signals"),
	}
	scanned := 0
	for _, surface := range surfaces {
		err := filepath.WalkDir(filepath.Join(root, surface), func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			// This file quotes the phrases it forbids; it is the description of the rule, not
			// a surface a person reads.
			if filepath.Base(path) == "vocabulary_test.go" {
				return nil
			}
			fragments, ok := readableText(t, path)
			if !ok {
				return nil
			}
			scanned++
			for number, fragment := range fragments {
				if offersAdvice(fragment) {
					t.Errorf("%s:%d speaks as an adviser: %q", path, number+1, strings.TrimSpace(fragment))
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
	goStringLiteral = regexp.MustCompile(`"(?:[^"\\\n]|\\.)*"`)
	sqlLiteral      = regexp.MustCompile(`'(?:[^'\n]|'')*'`)
	sqlLineComment  = regexp.MustCompile(`--.*$`)
	frontLiteral    = regexp.MustCompile("\"[^\"\\n]* [^\"\\n]*\"|'[^'\\n]* [^'\\n]*'|`[^`\\n]* [^`\\n]*`")
	templateHole    = regexp.MustCompile(`\$\{[^}]*\}`)
)

// readableText returns the parts of a file a person can read: prose, the strings a Go or SQL
// surface can emit, and the copy the front end renders. Identifiers are left out — a function
// named advisePosition would be a naming problem, not a claim to a reader.
func readableText(t *testing.T, path string) (map[int]string, bool) {
	t.Helper()
	extension := filepath.Ext(path)
	switch extension {
	case ".go", ".sql", ".md", ".ts", ".vue", ".yaml":
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
		case ".md", ".yaml":
			// Prose is read a paragraph at a time: a denial and the word it denies are one
			// thought, and a line break between them does not make them two.
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "| ") {
				paragraph, paragraphStart = "", number
			}
			paragraph += " " + line
			fragments[paragraphStart] = paragraph
			delete(fragments, number)
		case ".go":
			fragments[number] = strings.Join(goStringLiteral.FindAllString(line, -1), " ")
		case ".sql":
			fragments[number] = strings.Join(append(sqlLiteral.FindAllString(line, -1),
				sqlLineComment.FindAllString(line, -1)...), " ")
		case ".ts":
			fragments[number] = templateHole.ReplaceAllString(strings.Join(frontLiteral.FindAllString(line, -1), " "), "")
		case ".vue":
			if strings.HasPrefix(strings.TrimSpace(line), "<script") {
				inScript = true
			}
			if strings.HasPrefix(strings.TrimSpace(line), "</script") {
				inScript = false
			}
			if inScript {
				fragments[number] = templateHole.ReplaceAllString(strings.Join(frontLiteral.FindAllString(line, -1), " "), "")
			} else {
				fragments[number] = line
			}
		}
	}
	return fragments, true
}
