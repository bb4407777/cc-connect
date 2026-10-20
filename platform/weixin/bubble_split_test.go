package weixin

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// 2026-10-20 20:18 高律师令「现在不是有个分割线吗？分割线上方是后台做完的播报。
// 这个播报可以每隔 50~100 字左右，做个消息气泡，然后直接一次性回复过来。」——
// 以下覆盖：分割线分块、播报 50~100 字聚合（含超长单行整行成泡）、正文空行/@@@/围栏/表格与 3800 回落。

func TestSplitBubbles_DividerSplitsBroadcastFromBody(t *testing.T) {
	in := "【后台待报】\n- 案件一：已立案\n- 案件二：等开庭\n\n—————\n\nQ：进展如何\n\nA：都推完了。"
	got := splitBubbles(in)
	if len(got) != 4 {
		t.Fatalf("parts=%#v, want 4 bubbles", got)
	}
	if want := "【后台待报】\n- 案件一：已立案\n- 案件二：等开庭"; got[0] != want {
		t.Fatalf("broadcast=%q, want %q", got[0], want)
	}
	if got[1] != "—————" {
		t.Fatalf("divider=%q, want standalone divider bubble", got[1])
	}
	if got[2] != "Q：进展如何" || got[3] != "A：都推完了。" {
		t.Fatalf("body=%q,%q, want blank-line split", got[2], got[3])
	}
}

func TestSplitBubbles_BroadcastCutsAt50OnSentenceBoundary(t *testing.T) {
	l1 := strings.Repeat("甲", 28) + "。"
	l2 := strings.Repeat("乙", 28) + "。"
	l3 := strings.Repeat("丙", 28) + "。"
	got := splitBubbles(l1 + "\n" + l2 + "\n" + l3 + "\n—————\n正文")
	if len(got) != 4 {
		t.Fatalf("parts=%#v, want 4 bubbles", got)
	}
	if n := utf8.RuneCountInString(got[0]); n != 59 {
		t.Fatalf("first bubble has %d runes, want 59 (l1+l2, cut at ≥50)", n)
	}
	if got[1] != l3 || got[2] != "—————" || got[3] != "正文" {
		t.Fatalf("tail=%#v", got[1:])
	}
}

func TestSplitBubbles_BroadcastNoPunctCapsAt100(t *testing.T) {
	var lines []string
	for i := 0; i < 8; i++ {
		lines = append(lines, strings.Repeat("事", 30)) // 30 字一行，无句末标点
	}
	got := splitBubbles(strings.Join(lines, "\n") + "\n—————\n正文")
	// 无标点顺延：30→61→92 断，92→61 断，末泡 61；再加分割线与正文
	if len(got) != 5 {
		t.Fatalf("parts=%#v, want 5 bubbles", got)
	}
	for i, want := range []int{92, 92, 61} {
		if n := utf8.RuneCountInString(got[i]); n != want {
			t.Fatalf("bubble %d has %d runes, want %d", i, n, want)
		}
	}
	if got[3] != "—————" || got[4] != "正文" {
		t.Fatalf("tail=%#v", got[3:])
	}
}

func TestSplitBubbles_BroadcastOverlongLineStandsAlone(t *testing.T) {
	long := strings.Repeat("长", 150)
	got := splitBubbles("短条甲\n" + long + "\n短条乙。\n—————\n正文")
	if len(got) != 5 {
		t.Fatalf("parts=%#v, want 5 bubbles", got)
	}
	if got[0] != "短条甲" || got[1] != long || got[2] != "短条乙。" {
		t.Fatalf("overlong line must stand alone: %#v", got[:3])
	}
}

func TestSplitBubbles_BodyKeepsFenceTableAndForcedBreak(t *testing.T) {
	in := "播报甲。播报乙。\n—————\n前段\n@@@\n```go\na := 1\n\nb := 2\n```\n\n| 一 | 二 |\n|---|---|\n尾"
	got := splitBubbles(in)
	if len(got) != 5 {
		t.Fatalf("parts=%#v, want 5 bubbles", got)
	}
	if got[0] != "播报甲。播报乙。" || got[1] != "—————" || got[2] != "前段" {
		t.Fatalf("head=%#v", got[:3])
	}
	if want := "```go\na := 1\n\nb := 2\n```"; got[3] != want {
		t.Fatalf("fence split: got %q, want %q", got[3], want)
	}
	if want := "| 一 | 二 |\n|---|---|\n尾"; got[4] != want {
		t.Fatalf("table split: got %q, want %q", got[4], want)
	}
}

func TestSplitBubbles_BodyOverlongFallbackAfterDivider(t *testing.T) {
	got := splitBubbles("播报一。\n—————\n" + strings.Repeat("字", 4000))
	if len(got) != 4 {
		t.Fatalf("parts=%#v, want 4 bubbles", got)
	}
	if got[1] != "—————" {
		t.Fatalf("divider=%q", got[1])
	}
	if !strings.HasPrefix(got[3], contMark) {
		t.Fatalf("continuation missing %q prefix", contMark)
	}
	for i, p := range got {
		if n := utf8.RuneCountInString(p); n > maxWeixinChunk {
			t.Fatalf("bubble %d has %d runes > %d", i, n, maxWeixinChunk)
		}
	}
}

func TestIsBubbleDivider(t *testing.T) {
	yes := []string{"—————", "———", " ——— ", "——————————"}
	no := []string{"——", "—— 分割 ——", "-——-", "正文", "", "---"}
	for _, s := range yes {
		if !isBubbleDivider(s) {
			t.Fatalf("isBubbleDivider(%q)=false, want true", s)
		}
	}
	for _, s := range no {
		if isBubbleDivider(s) {
			t.Fatalf("isBubbleDivider(%q)=true, want false", s)
		}
	}
}
