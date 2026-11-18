// Package skip decides whether a catalog item may be stored.
// The minors rule is applied in code and cannot be turned off by configuration.
package skip

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"catalog/internal/store"
)

const MinorsReason = "内置规则：疑似未成年或未满21岁相关内容，已跳过且不保存简介"

// Decision is the outcome of skip evaluation. Reason never quotes a synopsis.
type Decision struct {
	Skip   bool
	Minors bool
	Reason string
}

// Evaluate always applies the built-in minors rule first, then enabled custom rules.
// description is only used as a match input. It is never copied into Reason.
func Evaluate(rules []store.SkipRule, title, tags, description string) Decision {
	if d, ok := matchMinors(title, tags, description); ok {
		return d
	}
	ordered := append([]store.SkipRule(nil), rules...)
	sortRules(ordered)
	for _, rule := range ordered {
		if rule.RuleType == store.RuleBuiltin || rule.Locked && rule.RuleType == store.RuleBuiltin {
			continue
		}
		if !rule.Enabled || rule.RuleType == store.RuleBuiltin {
			continue
		}
		if d, ok := matchCustom(rule, title, tags, description); ok {
			return d
		}
	}
	return Decision{}
}

func sortRules(rules []store.SkipRule) {
	for i := 1; i < len(rules); i++ {
		j := i
		for j > 0 && rules[j].Priority < rules[j-1].Priority {
			rules[j], rules[j-1] = rules[j-1], rules[j]
			j--
		}
	}
}

func matchCustom(rule store.SkipRule, title, tags, description string) (Decision, bool) {
	hay, where := fieldText(rule.Target, title, tags, description)
	if hay == "" || strings.TrimSpace(rule.Pattern) == "" {
		return Decision{}, false
	}
	switch rule.RuleType {
	case store.RuleKeyword:
		if strings.Contains(strings.ToLower(hay), strings.ToLower(strings.TrimSpace(rule.Pattern))) {
			return Decision{Skip: true, Reason: customReason(rule, where)}, true
		}
	case store.RuleRegex:
		re, err := regexp.Compile(rule.Pattern)
		if err != nil {
			return Decision{}, false
		}
		if re.MatchString(hay) {
			return Decision{Skip: true, Reason: customReason(rule, where)}, true
		}
	}
	return Decision{}, false
}

func customReason(rule store.SkipRule, where string) string {
	reason := strings.TrimSpace(rule.Reason)
	if reason == "" {
		reason = "命中跳过规则「" + rule.Name + "」"
	}
	return trimReason(reason + where)
}

func fieldText(target, title, tags, description string) (string, string) {
	switch target {
	case store.TargetTitle:
		return title, "（命中标题）"
	case store.TargetTags:
		return tags, "（命中标签）"
	case store.TargetDescription:
		return description, "（命中简介，未保存简介）"
	default:
		return title + "\n" + tags + "\n" + description, ""
	}
}

var asciiKeywords = []string{
	"loli", "lolita", "shota", "shotacon", "underage", "jailbait", "preteen",
	"child porn", "csam", "under 21", "under21", "under-21",
}

var textKeywords = []string{
	"未成年", "未成年人", "幼女", "幼童", "女童", "男童", "萝莉", "罗莉", "正太",
	"小学生", "初中生", "高中生", "中学生", "儿童", "幼齿", "童颜",
	"女高中生", "男高中生", "女初中", "男初中", "女小学", "男小学",
	"未满18", "未满十八", "未满21", "未满二十一",
	"高一", "高二", "高三", "初一", "初二", "初三",
}

var (
	digitAgeRe = regexp.MustCompile(`(?:^|[^\d])(\d{1,2})\s*(?:岁|周岁|yo\b|y/o|years?\s*old)`)
	cnAgeRe    = regexp.MustCompile(`(二十一|二十|[十]?[一二三四五六七八九]|十|两)\s*(?:岁|周岁)`)
)

func matchMinors(title, tags, description string) (Decision, bool) {
	parts := []struct {
		text  string
		where string
	}{
		{title, "（命中标题）"},
		{tags, "（命中标签）"},
		{description, "（命中简介，未保存简介）"},
	}
	for _, part := range parts {
		if part.text == "" {
			continue
		}
		if hit, ok := minorHit(part.text); ok {
			reason := MinorsReason + part.where
			if part.where != "（命中简介，未保存简介）" && hit != "" {
				reason += "：关键词「" + trimKeyword(hit) + "」"
			}
			return Decision{Skip: true, Minors: true, Reason: trimReason(reason)}, true
		}
	}
	return Decision{}, false
}

func minorHit(text string) (string, bool) {
	lower := strings.ToLower(text)
	for _, kw := range textKeywords {
		if strings.Contains(text, kw) {
			return kw, true
		}
	}
	for _, kw := range asciiKeywords {
		if containsASCIIWord(lower, kw) {
			return kw, true
		}
	}
	if n, ok := digitAge(text); ok && n < 21 {
		return strconv.Itoa(n) + "岁", true
	}
	if n, ok := cnAge(text); ok && n < 21 {
		return strconv.Itoa(n) + "岁", true
	}
	return "", false
}

func digitAge(text string) (int, bool) {
	m := digitAgeRe.FindStringSubmatch(text)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return n, true
}

var cnDigit = map[rune]int{
	'一': 1, '二': 2, '三': 3, '四': 4, '五': 5,
	'六': 6, '七': 7, '八': 8, '九': 9, '两': 2,
}

func cnAge(text string) (int, bool) {
	m := cnAgeRe.FindStringSubmatch(text)
	if m == nil {
		return 0, false
	}
	token := m[1]
	switch token {
	case "十":
		return 10, true
	case "二十":
		return 20, true
	case "二十一":
		return 21, true
	}
	runes := []rune(token)
	if len(runes) == 1 {
		if n, ok := cnDigit[runes[0]]; ok {
			return n, true
		}
	}
	if len(runes) == 2 && runes[0] == '十' {
		if n, ok := cnDigit[runes[1]]; ok {
			return 10 + n, true
		}
	}
	return 0, false
}

func containsASCIIWord(hay, needle string) bool {
	if strings.ContainsAny(needle, " -") {
		return strings.Contains(hay, needle)
	}
	start := 0
	for {
		i := strings.Index(hay[start:], needle)
		if i < 0 {
			return false
		}
		i += start
		beforeOK := i == 0 || !isASCIIWord(rune(hay[i-1]))
		end := i + len(needle)
		afterOK := end == len(hay) || !isASCIIWord(rune(hay[end]))
		if beforeOK && afterOK {
			return true
		}
		start = i + 1
	}
}

func isASCIIWord(r rune) bool {
	return r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r))
}

func trimReason(s string) string {
	r := []rune(s)
	if len(r) <= 240 {
		return s
	}
	return string(r[:240])
}

func trimKeyword(s string) string {
	r := []rune(s)
	if len(r) <= 32 {
		return s
	}
	return string(r[:32])
}

// BuiltinRule is the row shown in the admin. Matching does not depend on this row staying enabled.
func BuiltinRule() store.SkipRule {
	return store.SkipRule{
		Name:     "未成年与未满21岁",
		RuleType: store.RuleBuiltin,
		Target:   store.TargetAll,
		Pattern:  "builtin:minors-under-21",
		Reason:   "疑似未成年或未满21岁相关内容",
		Enabled:  true,
		Locked:   true,
		Priority: 0,
	}
}
