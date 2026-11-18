package helps

import (
	"strings"

	"github.com/MoeForge/moego/regexp"
)

// 字面品牌词可能出现在可执行路径和变量里；完整固定句规则仍按显式配置生效。
var antigravityExecutableTokens = regexp.MustCompile("`(?:[A-Za-z]:[\\\\/]|~/|/)[^`\\r\\n]*`|\"(?:[A-Za-z]:[\\\\/]|~/|/)[^\"\\r\\n]*\"|(?:^|[\\s(\"'`=])(?:[A-Za-z]:[\\\\/]|~/|/)[^\\s`\"'<>]*|\\$(?:\\{[A-Z][A-Z0-9_]*\\}|[A-Z][A-Z0-9_]*)")

func (m *SensitiveWordMatcher) obfuscateAntigravityLiterals(text string) string {
	protected := antigravityExecutableTokens.FindAllStringIndex(text, -1)
	if len(protected) == 0 {
		return m.regex.ReplaceAllStringFunc(text, m.replaceFn)
	}
	var out strings.Builder
	last, protectedIndex := 0, 0
	for _, match := range m.regex.FindAllStringIndex(text, -1) {
		for protectedIndex < len(protected) && protected[protectedIndex][1] <= match[0] {
			protectedIndex++
		}
		if protectedIndex < len(protected) && protected[protectedIndex][0] <= match[0] && match[1] <= protected[protectedIndex][1] {
			continue
		}
		out.WriteString(text[last:match[0]])
		out.WriteString(m.replaceFn(text[match[0]:match[1]]))
		last = match[1]
	}
	out.WriteString(text[last:])
	return out.String()
}
