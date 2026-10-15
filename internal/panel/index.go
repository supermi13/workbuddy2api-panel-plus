// index.go 面板静态资源与安全响应头。
//
// 资源经 go:embed 打进二进制（随服务部署，无外部构建步骤）：
//   - index.html  页面骨架
//   - app.js      全部前端逻辑（独立文件而非内联，为了启用无需 unsafe-inline 的严格 CSP）
//
// 安全头对"面板页面与全部 /panel/api/* 响应"统一生效：CSP 限制脚本只能来自本服务，
// 禁 MIME 嗅探，并声明不泄露 Referer 出去。iframe 嵌套（frame-ancestors /
// X-Frame-Options）按配置动态判定，默认禁止任何嵌套——见 setSecurityHeaders
// 与 normalizeFrameMode。
package panel

import (
	_ "embed"
	"net"
	"net/http"
	"net/url"
	"strings"
)

//go:embed index.html
var indexHTML []byte

//go:embed app.js
var appJS []byte

// cspBase 基础策略：frame-ancestors 之外的指令恒定不变，
// frame-ancestors 按配置与请求特征动态拼接（见 setSecurityHeaders）。
//   - default-src 'none'        默认全禁，逐个开口
//   - script-src 'self'         只跑同源脚本（app.js）；页面无内联事件处理器/内联脚本
//   - style-src 'self' 'unsafe-inline'
//     style 的内联是设计取舍：页面有少量 style="..." 属性（进度条宽度、表格列宽），
//     允许内联样式不会导致脚本执行；仍禁止外部样式域与 @import 外链。
//   - connect-src 'self'        前端 fetch 只能打本服务
//   - img-src 'self' data:      图标/内联图
//   - form-action 'none'        页面无表单提交目标（配置页是 JS 提交）
//   - base-uri 'none'           禁止注入 <base> 改写相对路径
const cspBase = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"connect-src 'self'; img-src 'self' data:; form-action 'none'; base-uri 'none'"

// cspNone 默认完整策略：禁止被任何站点 iframe 嵌套（防点击劫持）。
const cspNone = cspBase + "; frame-ancestors 'none'"

// setSecurityHeaders 写入面板统一安全响应头（页面与 API 都要，API 也含 JSON 数据）。
//
// iframe 嵌套策略由 cfg.FrameAncestors 决定（panel.New 时经 normalizeFrameMode 规范化）：
//
//	"none"（默认）  禁止任何站点嵌套：frame-ancestors 'none' + X-Frame-Options: DENY
//	"same-site"     仅放行同站父页面——同一主机/IP、任意端口。NAS 桌面入口（飞牛等）
//	                以 iframe 嵌面板正是跨端口同站场景；外部站点一律拒绝
//	                （Sec-Fetch-Site 判定，缺失时回落 Referer 主机比对，都拿不到则拒绝）
//	来源列表        只放行列表内 origin，如 "http://192.168.31.210,https://nas.example.com"
//
// 放行时不发 X-Frame-Options：它没有 same-site 语义（ALLOW-FROM 已被浏览器放弃），
// 由 CSP frame-ancestors 把关（CSP3 起主流浏览器全支持）。
func (p *Panel) setSecurityHeaders(w http.ResponseWriter, r *http.Request) {
	if p.frameAllowed(r) {
		w.Header().Set("Content-Security-Policy", cspBase+"; frame-ancestors "+p.frameAncestorsExpr(r))
	} else {
		w.Header().Set("Content-Security-Policy", cspNone)
		w.Header().Set("X-Frame-Options", "DENY") // 老浏览器兜底（CSP frame-ancestors 的等价项）
	}
	w.Header().Set("X-Content-Type-Options", "nosniff") // 禁 MIME 嗅探
	w.Header().Set("Referrer-Policy", "no-referrer")    // 不外泄面板地址给外部站点
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
}

// frameAllowed 本次响应是否允许被 iframe 嵌套。
func (p *Panel) frameAllowed(r *http.Request) bool {
	switch p.cfg.FrameAncestors {
	case "same-site":
		return sameSiteFramer(r)
	case "none":
		return false
	default:
		return true // 显式来源列表：能否嵌套由 CSP 表达式逐个判定
	}
}

// frameAncestorsExpr 放行时的 frame-ancestors 源表达式（CSP 源列表，空格分隔）。
func (p *Panel) frameAncestorsExpr(r *http.Request) string {
	if p.cfg.FrameAncestors == "same-site" {
		return sameSiteExprs(r)
	}
	return p.cfg.FrameAncestors
}

// normalizeFrameMode 规范化 frame_ancestors 配置值（panel.New 时调用一次）：
//
//	"" / none / deny / off        → "none"       禁止嵌套（默认）
//	same-site / samesite          → "same-site"  仅同站嵌套
//	http(s)://… 逗号/分号分隔列表  → 规范化后的源列表（空格分隔）
//	其他无法识别的值               → "none"       fail closed
//
// 为什么要校验显式列表：无效源表达式会让浏览器忽略整条 frame-ancestors 指令
// （等于不设防），所以识别不了的 token 一律丢弃，列表空了就回退 none。
func normalizeFrameMode(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "none", "deny", "off", "false":
		return "none"
	case "same-site", "samesite", "same_site":
		return "same-site"
	}
	var ok []string
	for _, tok := range strings.FieldsFunc(v, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	}) {
		if strings.EqualFold(tok, "'self'") || isHTTPOrigin(tok) {
			ok = append(ok, tok)
		}
	}
	if len(ok) == 0 {
		return "none"
	}
	return strings.Join(ok, " ")
}

// isHTTPOrigin 粗校验显式来源是否形如 http(s)://host[:port][/path]。
func isHTTPOrigin(tok string) bool {
	lower := strings.ToLower(tok)
	for _, p := range []string{"http://", "https://"} {
		if strings.HasPrefix(lower, p) {
			rest := tok[len(p):]
			return rest != "" && rest != "/" && !strings.ContainsAny(rest, " \t\r\n'\"`;()<>")
		}
	}
	return false
}

// sameSiteFramer 判断请求是否来自同站父页面（同一主机/IP、跨端口——NAS 桌面入口
// iframe 嵌面板正是此场景）。判定信号按可靠度排序：
//
//  1. Sec-Fetch-Site: same-origin / same-site（现代浏览器必发；cross-site 明确拒绝）
//  2. 缺失时回落 Referer 主机比对（老浏览器）
//
// 任何信号都拿不到时 fail closed（拒绝嵌套）。
func sameSiteFramer(r *http.Request) bool {
	host := hostOnly(r.Host)
	if host == "" {
		return false
	}
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "same-site":
		return true
	case "cross-site":
		return false
	}
	if ref := r.Header.Get("Referer"); ref != "" {
		if u, err := url.Parse(ref); err == nil {
			return strings.EqualFold(hostOnly(u.Host), host)
		}
	}
	return false
}

// sameSiteExprs 同站放行的 frame-ancestors 表达式：本机主机 + 端口通配
// （父页面可能是桌面入口的 80/443 或任意反代端口；CSP3 语法 port-part 允许 "*"）。
// 能解析出 Referer 的精确 origin 时一并放行，减少对端口通配的依赖。
func sameSiteExprs(r *http.Request) string {
	host := hostOnly(r.Host)
	if host == "" {
		return "'none'"
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	exprs := []string{scheme + "://" + hostForURL(host) + ":*"}
	if ref := r.Header.Get("Referer"); ref != "" {
		if u, err := url.Parse(ref); err == nil && u.Scheme != "" && u.Host != "" {
			exprs = append(exprs, u.Scheme+"://"+u.Host)
		}
	}
	return strings.Join(exprs, " ")
}

// hostOnly 取主机名：去端口（IPv6 去方括号）；无端口时原样返回。
func hostOnly(hostport string) string {
	if hostport == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		hostport = h
	}
	return strings.Trim(hostport, "[]")
}

// hostForURL 主机名转 URL 字面量：IPv6 加回方括号。
func hostForURL(host string) string {
	if strings.Contains(host, ":") {
		return "[" + host + "]"
	}
	return host
}

// index 输出面板页面（静态无秘密；数据接口 /panel/api/* 才走鉴权）。
func (p *Panel) index(w http.ResponseWriter, r *http.Request) {
	p.setSecurityHeaders(w, r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(indexHTML)
}

// appScript 输出前端逻辑（同源脚本，供 CSP script-src 'self' 加载）。
func (p *Panel) appScript(w http.ResponseWriter, r *http.Request) {
	p.setSecurityHeaders(w, r)
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(appJS)
}
