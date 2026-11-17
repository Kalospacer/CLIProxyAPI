package helps

import (
	"fmt"

	"github.com/MoeForge/moego/ojson"
)

type antigravityTextChange struct {
	path string
	text string
}

// isAntigravityToolPart 判断对话部分是否承载工具调用数据，这类字段不参与文本改写。
func isAntigravityToolPart(part ojson.Result) bool {
	return part.Get("functionCall").Exists() || part.Get("functionResponse").Exists() ||
		part.Get("function_call").Exists() || part.Get("function_response").Exists()
}

// ObfuscateSensitiveAntigravityPayloadDoc 改写系统指令与普通对话文本中的敏感内容。
func ObfuscateSensitiveAntigravityPayloadDoc(doc *ojson.Doc, matcher *SensitiveWordMatcher) error {
	if doc == nil || doc.Len() == 0 || !matcher.hasRules() {
		return nil
	}

	var changes []antigravityTextChange
	instruction := doc.Get("request.systemInstruction")
	if instruction.Type == ojson.String {
		appendAntigravityTextChange(&changes, "request.systemInstruction", instruction, matcher)
	} else {
		for i, part := range instruction.Get("parts").Array() {
			if isAntigravityToolPart(part) {
				continue
			}
			appendAntigravityTextChange(&changes, fmt.Sprintf("request.systemInstruction.parts.%d.text", i), part.Get("text"), matcher)
		}
	}

	for i, content := range doc.Get("request.contents").Array() {
		role := content.Get("role").String()
		if role != "user" && role != "model" && role != "assistant" {
			continue
		}
		for j, part := range content.Get("parts").Array() {
			if isAntigravityToolPart(part) {
				continue
			}
			appendAntigravityTextChange(&changes, fmt.Sprintf("request.contents.%d.parts.%d.text", i, j), part.Get("text"), matcher)
		}
	}

	for _, change := range changes {
		if errSet := doc.Set(change.path, change.text); errSet != nil {
			return fmt.Errorf("rewrite Antigravity message text: %w", errSet)
		}
	}
	return nil
}

// appendAntigravityTextChange 记录发生改写的文本，未命中规则时保持原值。
func appendAntigravityTextChange(changes *[]antigravityTextChange, path string, value ojson.Result, matcher *SensitiveWordMatcher) {
	if value.Type != ojson.String {
		return
	}
	original := value.String()
	rewritten := matcher.ObfuscateText(original)
	if rewritten != original {
		*changes = append(*changes, antigravityTextChange{path: path, text: rewritten})
	}
}
