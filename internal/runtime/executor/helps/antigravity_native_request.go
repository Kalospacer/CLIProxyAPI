package helps

import (
	"bytes"
	"net/http"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
)

// ConfigureAntigravityRequest 只从出口配置生成请求头，管理员显式覆盖仍最后生效。
func ConfigureAntigravityRequest(req *http.Request, authorization, userAgent, contentType string, attrs map[string]string) {
	req.Header = make(http.Header)
	req.Close = false
	req.RequestURI = ""
	req.TransferEncoding = nil
	req.Trailer = nil
	if req.URL != nil {
		req.Host = req.URL.Host
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("User-Agent", userAgent)
	util.ApplyCustomHeadersFromAttrs(req, attrs)
}

// NormalizeAntigravityAgentRequestDoc 按原生外层结构组织指令和声明，不改写工具内容。
func NormalizeAntigravityAgentRequestDoc(doc *ojson.Doc) {
	if doc.Get("requestType").String() != "agent" {
		return
	}
	if system := doc.Get("request.systemInstruction"); system.IsObject() && !system.Get("role").Exists() {
		if parts := system.Get("parts"); parts.IsArray() && len(parts.Array()) > 0 {
			_ = doc.Set("request.systemInstruction.role", "user")
		}
	}

	tools := doc.Get("request.tools")
	if !tools.IsArray() {
		return
	}
	var organized [][]byte
	changed := false
	for _, tool := range tools.Array() {
		declarations := tool.Get("functionDeclarations")
		// 带其他配置的容器保留作用域；内置工具和未知字段不得复制到每个声明。
		if !tool.IsObject() || len(tool.Map()) != 1 || !declarations.IsArray() || len(declarations.Array()) < 2 {
			organized = append(organized, []byte(tool.Raw))
			continue
		}
		for _, declaration := range declarations.Array() {
			wrapper := ojson.NewDocString(`{"functionDeclarations":[]}`)
			_ = wrapper.SetRawBytes("functionDeclarations", []byte("["+declaration.Raw+"]"))
			organized = append(organized, wrapper.Bytes())
		}
		changed = true
	}
	if changed {
		array := append([]byte("["), bytes.Join(organized, []byte(","))...)
		array = append(array, ']')
		_ = doc.SetRawBytes("request.tools", array)
	}
}
