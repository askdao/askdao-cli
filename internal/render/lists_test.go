package render

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderItemList_TruncatesPastMaxShown(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlain(&buf)

	items := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	RenderItemList(r, items, TruncateOpts{MaxShown: 8})

	out := buf.String()
	if !strings.Contains(out, "... and 2 more") {
		t.Errorf("expected '... and 2 more' trailer, got:\n%s", out)
	}
	if strings.Contains(out, "i") || strings.Contains(out, "j") {
		t.Errorf("hidden items should not appear: %s", out)
	}
}

func TestRenderItemList_TwoColumnLayout(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlain(&buf)
	RenderItemList(r, []string{"fastapi==0.135.1", "alembic==1.18.4"}, TruncateOpts{})
	// One line should contain both items.
	out := buf.String()
	if !strings.Contains(out, "fastapi==0.135.1") || !strings.Contains(out, "alembic==1.18.4") {
		t.Errorf("missing expected entries: %s", out)
	}
	if strings.Count(out, "\n") != 1 {
		t.Errorf("two items should pack into one line, got %d newlines:\n%s",
			strings.Count(out, "\n"), out)
	}
}

func TestRenderItemList_EmptyIsNoop(t *testing.T) {
	var buf bytes.Buffer
	RenderItemList(NewPlain(&buf), nil, TruncateOpts{})
	if buf.Len() != 0 {
		t.Errorf("empty input should produce no output, got %q", buf.String())
	}
}

func TestJoinTruncated(t *testing.T) {
	cases := []struct {
		in   []string
		max  int
		want string
	}{
		{[]string{"a", "b", "c"}, 5, "a, b, c"},
		{[]string{"a", "b", "c", "d", "e"}, 3, "a, b, c, … and 2 more"},
		{nil, 5, ""},
	}
	for _, tc := range cases {
		if got := JoinTruncated(tc.in, tc.max); got != tc.want {
			t.Errorf("JoinTruncated(%v, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
		}
	}
}
