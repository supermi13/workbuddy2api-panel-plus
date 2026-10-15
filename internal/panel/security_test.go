package panel

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestPanel() *Panel {
	// 启用鉴权：未带 key 的请求一律 401，不进入依赖 Pool/Upstream 的 handler。
	return New(Config{Version: "test", APIKey: "test-key"})
}

// 面板安全响应头必须覆盖：页面、静态脚本、鉴权失败响应。
func TestSecurityHeadersOnAllPanelResponses(t *testing.T) {
	p := newTestPanel()
	paths := []struct{ method, path string }{
		{"GET", "/panel/"},
		{"GET", "/panel/app.js"},
		{"GET", "/panel/api/overview"}, // 401（未提供 key）
		{"POST", "/panel/api/config"},  // 401
		{"GET", "/panel/api/nonexistent"},
	}
	for _, c := range paths {
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		h := rec.Header()
		if got := h.Get("Content-Security-Policy"); got == "" {
			t.Errorf("%s %s: missing CSP", c.method, c.path)
		}
		if h.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s %s: X-Content-Type-Options=%q", c.method, c.path, h.Get("X-Content-Type-Options"))
		}
		if h.Get("X-Frame-Options") != "DENY" {
			t.Errorf("%s %s: X-Frame-Options=%q", c.method, c.path, h.Get("X-Frame-Options"))
		}
		if h.Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("%s %s: Referrer-Policy=%q", c.method, c.path, h.Get("Referrer-Policy"))
		}
	}
}

// CSP 必须禁止内联脚本与 iframe 嵌套（严格策略的核心约束）。
func TestCSPDisallowsInlineScriptAndFraming(t *testing.T) {
	p := newTestPanel()
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest("GET", "/panel/", nil))
	csp := rec.Header().Get("Content-Security-Policy")

	for _, must := range []string{
		"script-src 'self'",
		"frame-ancestors 'none'",
		"base-uri 'none'",
		"default-src 'none'",
	} {
		if !strings.Contains(csp, must) {
			t.Errorf("CSP missing %q; got: %s", must, csp)
		}
	}
	if strings.Contains(csp, "script-src 'self' 'unsafe-inline'") || strings.Contains(csp, "script-src 'unsafe-inline'") {
		t.Errorf("CSP must not allow unsafe-inline scripts; got: %s", csp)
	}
}

// 页面必须引用外部脚本（内联脚本会被上面的 CSP 拦掉，页面将完全不可用）。
// 注意：script 标签带 ?v= 缓存戳（发版时手改），这里只断言 src 前缀。
func TestIndexReferencesExternalScript(t *testing.T) {
	p := newTestPanel()
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest("GET", "/panel/", nil))
	body := rec.Body.String()

	if !strings.Contains(body, `<script src="app.js`) {
		t.Error("index.html must load app.js externally (inline script is blocked by CSP)")
	}
	// 反例保护：出现内联 <script>...</script> 内容块即为回归
	if strings.Contains(body, "<script>\n") || strings.Contains(body, "<script> ") {
		t.Error("index.html still contains an inline <script> block; CSP would block it")
	}
}

// app.js 必须能作为同源脚本取到且类型正确（否则页面白屏）。
func TestAppScriptServed(t *testing.T) {
	p := newTestPanel()
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest("GET", "/panel/app.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("Content-Type=%q want javascript", ct)
	}
	if !strings.Contains(rec.Body.String(), "'use strict'") {
		t.Error("app.js body looks wrong")
	}
}

// UID 白名单：拒绝路径穿越与异常字符，放行真实 UUID 形态。
func TestValidUID(t *testing.T) {
	ok := []string{
		"248890d9-bb26-4131-87a7-4ec74d472344",
		"abc_123-XYZ",
		"a",
	}
	bad := []string{
		"",
		"../../evil",
		"x/../../y",
		`..\..\evil`,
		"a/b",
		"a\\b",
		"uid with space",
		"uid\nnewline",
		"uid\x00null",
		"café",
		strings.Repeat("a", 65), // 超长
	}
	for _, u := range ok {
		if !validUID(u) {
			t.Errorf("validUID(%q) = false, want true", u)
		}
	}
	for _, u := range bad {
		if validUID(u) {
			t.Errorf("validUID(%q) = true, want false", u)
		}
	}
}

// 未带密钥的 API 请求必须 401；携带正确密钥则通过鉴权层（不再是 401）。
func TestAuthLayerBehavior(t *testing.T) {
	p := newTestPanel()
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest("GET", "/panel/api/overview", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no key: code=%d want 401", rec.Code)
	}
	// 用不存在的路由验证"带正确 key 已过鉴权"（避免触碰依赖 nil 的 handler）。
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/panel/api/nonexistent", nil)
	req2.Header.Set("Authorization", "Bearer test-key")
	p.ServeHTTP(rec2, req2)
	if rec2.Code == http.StatusUnauthorized {
		t.Error("valid key must pass the auth layer")
	}
}

// ---------------------------------------------------------------------------
// iframe 嵌套策略（frame_ancestors）
// ---------------------------------------------------------------------------

// newFramePanel 构造指定嵌套策略的面板（其余同 newTestPanel）。
func newFramePanel(mode string) *Panel {
	return New(Config{Version: "test", APIKey: "test-key", FrameAncestors: mode})
}

// frameReq 构造带 Host 与帧相关请求头的面板页面请求。
func frameReq(host string, hdr map[string]string) *http.Request {
	req := httptest.NewRequest("GET", "/panel/", nil)
	req.Host = host
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	return req
}

// same-site 策略：同站父页面（NAS 桌面入口跨端口 iframe）放行；
// 跨站与无信号场景一律拒绝（fail closed）。
func TestFrameAncestorsSameSite(t *testing.T) {
	p := newFramePanel("same-site")
	cases := []struct {
		name    string
		hdr     map[string]string
		allowed bool
	}{
		{"Sec-Fetch-Site: same-site（NAS 桌面入口）", map[string]string{"Sec-Fetch-Site": "same-site"}, true},
		{"Sec-Fetch-Site: same-origin", map[string]string{"Sec-Fetch-Site": "same-origin"}, true},
		{"Sec-Fetch-Site: cross-site（外部站点）", map[string]string{"Sec-Fetch-Site": "cross-site"}, false},
		{"无 Sec-Fetch-Site，Referer 同主机（老浏览器回落）", map[string]string{"Referer": "http://192.168.31.210/desktop"}, true},
		{"无 Sec-Fetch-Site，Referer 异主机", map[string]string{"Referer": "http://evil.example/"}, false},
		{"无任何信号 fail closed", nil, false},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, frameReq("192.168.31.210:7863", c.hdr))
		csp := rec.Header().Get("Content-Security-Policy")
		xfo := rec.Header().Get("X-Frame-Options")
		if c.allowed {
			if !strings.Contains(csp, "frame-ancestors http://192.168.31.210:*") {
				t.Errorf("%s: 应放行同站嵌套，CSP=%q", c.name, csp)
			}
			if xfo != "" {
				t.Errorf("%s: 放行时不应发 X-Frame-Options，got %q", c.name, xfo)
			}
		} else {
			if !strings.Contains(csp, "frame-ancestors 'none'") || xfo != "DENY" {
				t.Errorf("%s: 应拒绝嵌套，CSP=%q XFO=%q", c.name, csp, xfo)
			}
		}
	}
}

// 放行时除端口通配外，还应带上 Referer 精确 origin（非默认端口父页面兜底）。
func TestFrameAncestorsSameSiteIncludesRefererOrigin(t *testing.T) {
	p := newFramePanel("same-site")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, frameReq("192.168.31.210:7863", map[string]string{
		"Sec-Fetch-Site": "same-site",
		"Referer":        "http://192.168.31.210/desktop",
	}))
	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{"frame-ancestors http://192.168.31.210:*", "http://192.168.31.210"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP 缺 %q: %s", want, csp)
		}
	}
}

// IPv6 主机：同站放行表达式必须加回方括号（否则 CSP 语法错误）。
func TestFrameAncestorsIPv6Host(t *testing.T) {
	p := newFramePanel("same-site")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, frameReq("[::1]:7863", map[string]string{"Sec-Fetch-Site": "same-site"}))
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-ancestors http://[::1]:*") {
		t.Errorf("IPv6 表达式错误: %s", csp)
	}
}

// 显式来源列表：CSP 逐字承载、不发 XFO；脏值必须 fail closed 回 none。
func TestFrameAncestorsExplicitList(t *testing.T) {
	p := newFramePanel("http://192.168.31.210, https://nas.example.com")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, frameReq("nas.example.com:7863", nil))
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-ancestors http://192.168.31.210 https://nas.example.com") {
		t.Errorf("显式列表未进 CSP: %s", csp)
	}
	if rec.Header().Get("X-Frame-Options") != "" {
		t.Error("显式列表模式不应发 X-Frame-Options")
	}
	// 脏值（含注入 token）一律剔除；列表空了回退 none。
	// 为什么重要：无效源表达式会让浏览器忽略整条 frame-ancestors 指令（等于不设防）。
	for _, bad := range []string{"garbage", "javascript:alert(1)", ";;;", "http://"} {
		p2 := newFramePanel(bad)
		rec2 := httptest.NewRecorder()
		p2.ServeHTTP(rec2, frameReq("192.168.31.210:7863", map[string]string{"Sec-Fetch-Site": "same-site"}))
		if !strings.Contains(rec2.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") ||
			rec2.Header().Get("X-Frame-Options") != "DENY" {
			t.Errorf("脏值 %q 未 fail closed 回 none", bad)
		}
	}
	// 混合输入：合法 token 保留、非法 token 剔除（不是整体回退 none）。
	p3 := newFramePanel("http://a javascript:x")
	rec3 := httptest.NewRecorder()
	p3.ServeHTTP(rec3, frameReq("192.168.31.210:7863", nil))
	csp3 := rec3.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp3, "frame-ancestors http://a") || strings.Contains(csp3, "javascript") {
		t.Errorf("混合输入处理错误: %s", csp3)
	}
}

// 默认（未配置）必须维持严格禁止——即使请求带着 same-site 信号。
func TestFrameAncestorsDefaultStrict(t *testing.T) {
	p := newFramePanel("")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, frameReq("192.168.31.210:7863", map[string]string{"Sec-Fetch-Site": "same-site"}))
	if rec.Header().Get("X-Frame-Options") != "DENY" ||
		!strings.Contains(rec.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Error("默认策略必须禁止任何嵌套")
	}
}

// normalizeFrameMode 规范化表。
func TestNormalizeFrameMode(t *testing.T) {
	cases := map[string]string{
		"":                            "none",
		"none":                        "none",
		"DENY":                        "none",
		"SameSite":                    "same-site",
		"same_site":                   "same-site",
		"http://a,http://b":           "http://a http://b",
		" 'self' ; https://c ":        "'self' https://c",
		"http://a, garbage, http://b": "http://a http://b",
		"???":                         "none",
	}
	for in, want := range cases {
		if got := normalizeFrameMode(in); got != want {
			t.Errorf("normalizeFrameMode(%q) = %q, want %q", in, got, want)
		}
	}
}
