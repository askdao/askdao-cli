package deploy

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_Deploy_HappyPath(t *testing.T) {
	skillZip := makeSkillZip(t, "my-skill", "---\nname: my-skill\n---\ndo a thing\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != DefaultDeployPath {
			t.Errorf("path = %q, want %q", r.URL.Path, DefaultDeployPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("auth header = %q, want %q", got, "Bearer tok")
		}
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
			return
		}
		if got := r.FormValue("agent_yaml"); !strings.Contains(got, "apiVersion") {
			t.Errorf("agent_yaml field = %q", got)
		}
		if got := r.FormValue("harness_id"); got != "anthropic_managed_agents" {
			t.Errorf("harness_id field = %q", got)
		}
		if got := r.FormValue("force"); got != "true" {
			t.Errorf("force field = %q, want true", got)
		}
		fhs := r.MultipartForm.File["my-skill"]
		if len(fhs) != 1 {
			t.Errorf("skill file part 'my-skill': got %d parts, want 1; file fields present: %v", len(fhs), fileFieldNames(r))
			return
		}
		f, err := fhs[0].Open()
		if err != nil {
			t.Errorf("open skill part: %v", err)
			return
		}
		zb, _ := io.ReadAll(f)
		_ = f.Close()
		zr, err := zip.NewReader(bytes.NewReader(zb), int64(len(zb)))
		if err != nil {
			t.Errorf("skill part is not a valid zip: %v", err)
			return
		}
		hasSkillMd := false
		for _, zf := range zr.File {
			if zf.Name == "my-skill/SKILL.md" {
				hasSkillMd = true
			}
		}
		if !hasSkillMd {
			t.Errorf("skill zip missing my-skill/SKILL.md; entries: %v", zipNames(zr))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(DeployResponse{
			AgentID:                "agt_x",
			AnthropicAgentID:       "agent_x",
			AnthropicEnvironmentID: "env_x",
			AgentURL:               "https://askdao.ai/k/usr_1/a/agt_x",
			Skills: []map[string]interface{}{{
				"type": "custom_local", "path": "my-skill",
				"anthropic_skill_id": "skill_x", "anthropic_skill_version": "1",
				"ov_content_uri": "skill-store://private/usr_1/skill_x/v0.1.0/",
			}},
			TranslationReport: TranslationReport{Harness: "anthropic_managed_agents"},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	c.AuthToken = "tok"
	resp, err := c.Deploy(context.Background(), DeployInput{
		AgentYAML: []byte("apiVersion: askdao.ai/v1\nkind: AgentSpec\n"),
		HarnessID: "anthropic_managed_agents",
		Force:     true,
		SkillZips: map[string][]byte{"my-skill": skillZip},
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if resp.AgentID != "agt_x" || resp.AgentURL != "https://askdao.ai/k/usr_1/a/agt_x" {
		t.Errorf("unexpected response: %+v", resp)
	}
	if len(resp.Skills) != 1 || resp.Skills[0]["anthropic_skill_id"] != "skill_x" {
		t.Errorf("unexpected skills: %+v", resp.Skills)
	}
}

func TestClient_Deploy_KolProfileRequired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"detail":{"reason":"kol_profile_required","fields":["kol_join_mode","kol_bio"],"hint":"PATCH /api/v1/users/me/kol-profile with kol_join_mode='free'"}}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Deploy(context.Background(), DeployInput{AgentYAML: []byte("x")})
	var kpr *ErrKolProfileRequired
	if !errors.As(err, &kpr) {
		t.Fatalf("err = %v, want *ErrKolProfileRequired", err)
	}
	if kpr.Detail.Reason != "kol_profile_required" || len(kpr.Detail.Fields) != 2 || kpr.Detail.Hint == "" {
		t.Errorf("parsed detail = %+v", kpr.Detail)
	}
}

func TestClient_Deploy_BlockingWarnings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"detail":{"reason":"translation_report contains REJECTED (deploy-fatal) warnings; set force=true to deploy anyway","translation_report":{"harness":"anthropic_managed_agents","translation_warnings":[{"field":"custom_tools[0].name","severity":"high","action":"rejected","reason":"custom tool name is invalid"}]}}}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Deploy(context.Background(), DeployInput{AgentYAML: []byte("x")})
	var bw *ErrBlockingWarnings
	if !errors.As(err, &bw) {
		t.Fatalf("err = %v, want *ErrBlockingWarnings", err)
	}
	if len(bw.Report.TranslationWarnings) != 1 {
		t.Fatalf("warnings = %+v", bw.Report.TranslationWarnings)
	}
	wn := bw.Report.TranslationWarnings[0]
	if wn.Field != "custom_tools[0].name" || wn.Severity != "high" || wn.Action != "rejected" {
		t.Errorf("parsed warning = %+v", wn)
	}
}

func TestClient_Deploy_VisibilityDowngradeConfirm(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"detail":{"reason":"visibility_downgrade_requires_confirm","current_visibility":"public","requested_visibility":"private","agent_name":"童话讲书人"}}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Deploy(context.Background(), DeployInput{AgentYAML: []byte("x")})
	var vdc *ErrVisibilityDowngradeConfirm
	if !errors.As(err, &vdc) {
		t.Fatalf("err = %v, want *ErrVisibilityDowngradeConfirm", err)
	}
	d := vdc.Detail
	if d.CurrentVisibility != "public" || d.RequestedVisibility != "private" || d.AgentName != "童话讲书人" {
		t.Errorf("parsed detail = %+v", d)
	}
}

func TestClient_Deploy_SendsConfirmVisibilityDowngradeField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
			return
		}
		if got := r.FormValue("confirm_visibility_downgrade"); got != "true" {
			t.Errorf("confirm_visibility_downgrade field = %q, want true", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(DeployResponse{AgentID: "agt_x"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	if _, err := c.Deploy(context.Background(), DeployInput{
		AgentYAML:                  []byte("x"),
		ConfirmVisibilityDowngrade: true,
	}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
}

func TestClient_Deploy_GenericNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"detail":"Anthropic API call failed: boom"}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Deploy(context.Background(), DeployInput{AgentYAML: []byte("x")})
	if err == nil || !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want 502 + body", err)
	}
}

func TestClient_Deploy_409StringDetailFallsThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"detail":"some other 409 reason"}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Deploy(context.Background(), DeployInput{AgentYAML: []byte("x")})
	var kpr *ErrKolProfileRequired
	var bw *ErrBlockingWarnings
	if errors.As(err, &kpr) || errors.As(err, &bw) {
		t.Fatalf("string-detail 409 should not be a typed error: %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "409") || !strings.Contains(err.Error(), "some other 409") {
		t.Fatalf("err = %v, want generic 409 + body", err)
	}
}

func TestClient_Deploy_EmptyBaseURL(t *testing.T) {
	c := &Client{}
	if _, err := c.Deploy(context.Background(), DeployInput{AgentYAML: []byte("x")}); err == nil {
		t.Error("Deploy with empty BaseURL should error")
	}
}

func TestClient_SetupKol(t *testing.T) {
	var gotBody KolProfilePatch
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		if r.URL.Path != DefaultKolProfilePath {
			t.Errorf("path = %q, want %q", r.URL.Path, DefaultKolProfilePath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("auth header = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"user_id":"usr_1","name":"n","email":"e","is_kol":true,"kol_join_mode":"free"}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	c.AuthToken = "tok"
	if err := c.SetupKol(context.Background(), KolProfilePatch{KolJoinMode: "free", KolBio: "hi there"}); err != nil {
		t.Fatalf("SetupKol: %v", err)
	}
	if gotBody.KolJoinMode != "free" || gotBody.KolBio != "hi there" {
		t.Errorf("server received body %+v", gotBody)
	}
}

func TestClient_SetupKol_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"detail":"invalid kol_join_mode 'bogus'"}`)
	}))
	defer srv.Close()
	c := NewClient(srv.URL)
	if err := c.SetupKol(context.Background(), KolProfilePatch{KolJoinMode: "bogus"}); err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("err = %v, want 400 + body", err)
	}
}

// --- test helpers ---

func makeSkillZip(t *testing.T, rootName, skillMd string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(rootName + "/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(skillMd)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func fileFieldNames(r *http.Request) []string {
	if r.MultipartForm == nil {
		return nil
	}
	out := make([]string, 0, len(r.MultipartForm.File))
	for k := range r.MultipartForm.File {
		out = append(out, k)
	}
	return out
}

func zipNames(zr *zip.Reader) []string {
	out := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		out = append(out, f.Name)
	}
	return out
}
