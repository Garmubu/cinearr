package rx

import "testing"

func TestBasics(t *testing.T) {
	cases := []struct {
		pat  string
		opts Options
		in   string
		want string // whole match, "" with ok=false means no match
		ok   bool
	}{
		{`abc`, 0, "xxabcxx", "abc", true},
		{`a.c`, 0, "a\nc abc", "abc", true},
		{`(?<=\d{4})x`, 0, "1999x", "x", true},
		{`(?<!\d)x`, 0, "1x ax", "x", true},
		{`foo(?!bar)`, 0, "foobar foobaz", "foo", true},
		{`\bweb\b`, IgnoreCase, "Movie.WEB.1080p", "WEB", true},
		{`(?-i:WEB)$`, IgnoreCase, "movie web", "", false},
		{`(?-i:WEB)$`, IgnoreCase, "movie WEB", "WEB", true},
		{`[^])]+`, 0, "ab)c", "ab", true},
		{`(\w+)-\1`, 0, "ab-ab", "ab-ab", true},
		{`a{2,3}?`, 0, "aaaa", "aa", true},
		{`{{\s*if\s*(.+?)\s*}}`, 0, "x{{ if .Keywords }}y", "{{ if .Keywords }}", true},
		{`[-_. ]`, 0, "a_b", "_", true},
		{`(?<x>a)|(?<x>b)`, 0, "b", "b", true},
		{"a # comment\n b", IgnorePatternWhitespace, "ab", "ab", true},
		{`(?>a+)b`, 0, "aaab", "aaab", true},
		{`(?>a+)a`, 0, "aaaa", "", false},
		{`é`, IgnoreCase, "É", "É", true},
		{`^$`, 0, "", "", true},
		{`x*`, 0, "", "", true},
	}
	for _, c := range cases {
		re, err := Compile(c.pat, c.opts)
		if err != nil {
			t.Fatalf("%q: %v", c.pat, err)
		}
		m := re.FindStringMatch(c.in)
		if (m != nil) != c.ok {
			t.Errorf("%q on %q: ok=%v want %v", c.pat, c.in, m != nil, c.ok)
			continue
		}
		if m != nil && m.String() != c.want {
			t.Errorf("%q on %q: got %q want %q", c.pat, c.in, m.String(), c.want)
		}
	}
}

func TestGroupsAndReplace(t *testing.T) {
	re := MustCompile(`(?<title>.+?)\.(?<year>\d{4})`, 0)
	m := re.FindStringMatch("The.Movie.2010.1080p")
	if m.Value("title") != "The.Movie" || m.Value("year") != "2010" {
		t.Fatalf("got %q %q", m.Value("title"), m.Value("year"))
	}
	if g := m.Group("year"); g.Index != 10 || g.Length != 4 {
		t.Fatalf("index %d len %d", g.Index, g.Length)
	}
	r := MustCompile(`(\w+)@(\w+)`, 0).ReplaceAllString("a@b c@d", "$2@$1 ${1}")
	if r != "b@a a d@c c" {
		t.Fatalf("replace got %q", r)
	}
	parts := MustCompile(`[ ]+(?:AKA|\/)[ ]+`, IgnoreCase).Split("Foo AKA Bar / Baz")
	if len(parts) != 3 || parts[2] != "Baz" {
		t.Fatalf("split %q", parts)
	}
	if got := MustCompile(`\s*`, 0).ReplaceAllString("a b", "-"); got != "-a--b-" {
		t.Fatalf("empty replace %q", got)
	}
	// numbering: unnamed first, then named
	m = MustCompile(`(?<n>a)(b)`, 0).FindStringMatch("ab")
	if m.GroupNum(1).Value != "b" || m.GroupNum(2).Value != "a" {
		t.Fatalf("numbering wrong")
	}
	// repeated captures
	m = MustCompile(`(?:(?<d>\d),?)+`, 0).FindStringMatch("1,2,3")
	if g := m.Group("d"); g.Value != "3" || len(g.Captures) != 3 {
		t.Fatalf("captures %v", g.Captures)
	}
}

func TestNetBlocks(t *testing.T) {
	re := MustCompile(`([\p{IsCJKUnifiedIdeographs}\W]+)`, 0)
	if got := re.ReplaceAllString("盗梦空间 Inception", "."); got != ".Inception" {
		t.Fatalf("CJK block: %q", got)
	}
	if !MustCompile(`^\p{IsCyrillic}+$`, 0).MatchString("Начало") || MustCompile(`\p{IsCyrillic}`, 0).MatchString("abc") {
		t.Fatal("Cyrillic block")
	}
	if !MustCompile(`\p{IsBasicLatin}`, 0).MatchString("a") {
		t.Fatal("BasicLatin")
	}
}
