package skip

import (
	"strings"
	"testing"

	"catalog/internal/store"
)

func TestMinorsRuleMatchesAgesUnder21AndNotEpisodeNumbers(t *testing.T) {
	cases := []struct {
		name  string
		title string
		tags  string
		desc  string
		skip  bool
	}{
		{"seventeen", "测试17岁", "", "SYNOPSIS_SHOULD_NOT_LEAK", true},
		{"eighteen", "演员18岁", "", "", true},
		{"twenty", "20 years old", "", "", true},
		{"twenty one", "演员21岁", "", "", false},
		{"episode number", "第18集 都市故事", "", "", false},
		{"year", "2024年上线", "", "", false},
		{"views", "播放 82.1w", "", "", false},
		{"loli", "普通标题", "loli", "", true},
		{"luoli", "萝莉题材样本", "", "", true},
		{"child word", "", "女童", "", true},
		{"grade", "高一同学", "", "", true},
		{"cn age", "十七岁", "", "", true},
		{"cn twenty one", "二十一岁", "", "", false},
		{"under 21 phrase", "under 21", "", "", true},
		{"plain", "虚构作品甲", "都市", "这是普通简介", false},
		{"college not listed", "大学生日常", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := Evaluate(nil, tc.title, tc.tags, tc.desc)
			if d.Skip != tc.skip {
				t.Fatalf("skip=%v want %v (%+v)", d.Skip, tc.skip, d)
			}
			if tc.skip && !d.Minors {
				t.Fatal("expected minors flag")
			}
			if strings.Contains(d.Reason, "SYNOPSIS_SHOULD_NOT_LEAK") || strings.Contains(d.Reason, "这是普通简介") {
				t.Fatalf("reason quoted synopsis: %s", d.Reason)
			}
		})
	}
}

func TestCustomRulesAndDisabledBuiltinRow(t *testing.T) {
	rules := []store.SkipRule{
		{Name: "广告", RuleType: store.RuleKeyword, Target: store.TargetTitle, Pattern: "广告", Reason: "标题含广告", Enabled: true, Priority: 10},
		{Name: "停用", RuleType: store.RuleKeyword, Target: store.TargetAll, Pattern: "虚构", Reason: "不应命中", Enabled: false, Priority: 1},
		{Name: "内置被改", RuleType: store.RuleBuiltin, Target: store.TargetAll, Pattern: "never", Enabled: false, Locked: true, Priority: 0},
	}
	d := Evaluate(rules, "周末广告", "", "简介原文不要出现在原因里")
	if !d.Skip || d.Minors {
		t.Fatalf("got %+v", d)
	}
	if !strings.Contains(d.Reason, "标题含广告") {
		t.Fatalf("reason: %s", d.Reason)
	}
	if strings.Contains(d.Reason, "简介原文") {
		t.Fatalf("leaked description: %s", d.Reason)
	}

	// Disabled builtin row must not weaken the code-level minors check.
	d = Evaluate(rules, "17岁", "", "SECRET_SYNOPSIS")
	if !d.Minors || !d.Skip {
		t.Fatal("minors rule was disabled")
	}
	if strings.Contains(d.Reason, "SECRET_SYNOPSIS") {
		t.Fatal("leaked synopsis")
	}
}

func TestRegexRule(t *testing.T) {
	rules := []store.SkipRule{{
		Name: "编号", RuleType: store.RuleRegex, Target: store.TargetTitle,
		Pattern: `测试-\d{3}`, Reason: "命中编号", Enabled: true,
	}}
	d := Evaluate(rules, "测试-123", "", "")
	if !d.Skip {
		t.Fatal("expected skip")
	}
	bad := []store.SkipRule{{
		Name: "坏", RuleType: store.RuleRegex, Target: store.TargetAll,
		Pattern: `(`, Enabled: true, Reason: "坏正则",
	}}
	if Evaluate(bad, "任意", "", "").Skip {
		t.Fatal("invalid regex should not skip")
	}
}
