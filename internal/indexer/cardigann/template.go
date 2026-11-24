package cardigann

import (
	"fmt"
	"strings"

	"cinearr/internal/rx"
)

// Vars holds template variables keyed like Prowlarr (".Config.sitelink",
// ".Query.Keywords", ".Result.title" ...). Values are string, nil or []string.
type Vars map[string]any

func (v Vars) clone() Vars {
	out := make(Vars, len(v))
	for k, x := range v {
		out[k] = x
	}
	return out
}

func (v Vars) str(key string) string {
	switch x := v[key].(type) {
	case string:
		return x
	case []string:
		return strings.Join(x, ",")
	case nil:
		return ""
	default:
		return fmt.Sprint(x)
	}
}

// Port of CardigannBase.ApplyGoTemplateText. Prowlarr does not run Go's
// text/template: it evaluates a fixed set of constructs with regular
// expressions, in this order: re_replace, join, logic functions
// (and/or/eq/ne), if/else/end, range, plain variables.

var (
	tplReReplace = rx.MustCompile(`{{\s*re_replace\s+(\..+?)\s+"(.*?)"\s+"(.*?)"\s*}}`, 0)
	tplJoin      = rx.MustCompile(`{{\s*join\s+(\..+?)\s+"(.*?)"\s*}}`, 0)
	tplLogic     = rx.MustCompile(`\b(and|or|eq|ne)(?:\s+(\(?\.[^\)\s]+\)?|"[^"]+")){2,}`, 0)
	tplIfElse    = rx.MustCompile(`{{\s*if\s*(.+?)\s*}}(.*?){{\s*else\s*}}(.*?){{\s*end\s*}}`, 0)
	tplRange     = rx.MustCompile(`{{\s*range\s*(((?<index>\$.+?),)((\s*(?<element>.+?)\s*(:=)\s*)))?(?<variable>.+?)\s*}}(?<prefix>.*?){{\.}}(?<postfix>.*?){{end}}`, 0)
	tplVariable  = rx.MustCompile(`{{\s*(\..+?)\s*}}`, 0)
)

// Modifier transforms expanded values (e.g. URL encoding of paths).
type Modifier func(string) string

// ApplyTemplate expands a Cardigann template.
func ApplyTemplate(template string, vars Vars, modifier Modifier) (string, error) {
	if strings.TrimSpace(template) == "" || !strings.Contains(template, "{{") {
		return template, nil
	}
	mod := func(s string) string {
		if modifier != nil {
			return modifier(s)
		}
		return s
	}

	for _, m := range tplReReplace.FindAllStringMatch(template) {
		all := m.String()
		re, err := rx.Cached(m.GroupNum(2).Value, 0)
		if err != nil {
			return "", err
		}
		expanded := mod(re.ReplaceAllString(vars.str(m.GroupNum(1).Value), m.GroupNum(3).Value))
		template = strings.ReplaceAll(template, all, expanded)
	}

	for _, m := range tplJoin.FindAllStringMatch(template) {
		all := m.String()
		var list []string
		switch x := vars[m.GroupNum(1).Value].(type) {
		case []string:
			list = x
		case string:
			if x != "" {
				list = []string{x}
			}
		}
		template = strings.ReplaceAll(template, all, mod(strings.Join(list, m.GroupNum(2).Value)))
	}

	for guard := 0; guard < 1000; guard++ {
		m := tplLogic.FindStringMatch(template)
		if m == nil {
			break
		}
		start := m.Index()
		length := m.Length()
		fn := m.GroupNum(1).Value
		caps := m.GroupNum(2)
		var params []string
		for _, c := range caps.Captures {
			params = append(params, strings.Trim(c, "()"))
		}
		result := ""
		switch fn {
		case "and", "or":
			isAnd := fn == "and"
			var filtered []string
			for _, p := range params {
				if !strings.HasPrefix(p, `"`) {
					filtered = append(filtered, p)
				}
			}
			for _, p := range filtered {
				result = p
				sv, _ := vars[p].(string)
				if (strings.TrimSpace(sv) == "") == isAnd {
					break
				}
			}
		case "eq", "ne":
			wantEqual := fn == "eq"
			if len(params) > 2 {
				length = caps.CaptureSpans[2][0] - start
			}
			val := func(p string) (string, bool) {
				if strings.HasPrefix(p, `"`) {
					return strings.Trim(p, `"`), true
				}
				x, ok := vars[p].(string)
				return x, ok
			}
			a, aok := val(params[0])
			b, bok := val(params[1])
			isEqual := aok == bok && a == b
			if isEqual == wantEqual {
				result = ".True"
			} else {
				result = ".False"
			}
		}
		r := []rune(template)
		template = string(r[:start]) + result + string(r[start+length:])
	}

	for _, m := range tplIfElse.FindAllStringMatch(template) {
		all := m.String()
		cond := m.GroupNum(1).Value
		onTrue, onFalse := m.GroupNum(2).Value, m.GroupNum(3).Value
		if !strings.HasPrefix(cond, ".") {
			return "", fmt.Errorf("condition %q not implemented", cond)
		}
		state := false
		switch x := vars[cond].(type) {
		case string:
			state = strings.TrimSpace(x) != ""
		case []string:
			state = len(x) > 0
		case bool:
			state = x
		}
		res := onFalse
		if state {
			res = onTrue
		}
		template = strings.ReplaceAll(template, all, res)
	}

	for _, m := range tplRange.FindAllStringMatch(template) {
		all := m.String()
		index := m.Value("index")
		variable := m.Value("variable")
		prefix, postfix := m.Value("prefix"), m.Value("postfix")
		var list []string
		switch x := vars[variable].(type) {
		case []string:
			list = x
		case string:
			if x != "" {
				list = []string{x}
			}
		}
		var b strings.Builder
		indexReplace := "{{" + index + "}}"
		for i, v := range list {
			nv := mod(v)
			if strings.TrimSpace(index) != "" {
				is := fmt.Sprint(i)
				b.WriteString(strings.ReplaceAll(prefix, indexReplace, is) + nv + strings.ReplaceAll(postfix, indexReplace, is))
			} else {
				b.WriteString(prefix + nv + postfix)
			}
		}
		template = strings.ReplaceAll(template, all, b.String())
	}

	for _, m := range tplVariable.FindAllStringMatch(template) {
		all := m.String()
		key := m.GroupNum(1).Value
		val := ""
		if vars[key] != nil {
			val = vars.str(key)
		}
		template = strings.ReplaceAll(template, all, mod(val))
	}
	return template, nil
}
