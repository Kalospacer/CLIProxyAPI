package helps

import (
	"fmt"

	"github.com/MoeForge/moego/ojson"
)

type antigravityTextChange struct {
	path string
	text string
}

// ObfuscateSensitiveAntigravityPayloadDoc 改写系统指令与普通对话文本。
func ObfuscateSensitiveAntigravityPayloadDoc(doc *ojson.Doc, matcher *SensitiveWordMatcher) error {
	if doc == nil || doc.Len() == 0 || !matcher.hasRules() {
		return nil
	}

	var changes []antigravityTextChange
	for _, path := range []string{"request.systemInstruction", "request.system_instruction", "systemInstruction", "system_instruction"} {
		instruction := doc.Get(path)
		if instruction.Type == ojson.String {
			appendAntigravityTextChange(&changes, path, instruction, matcher)
			continue
		}
		for i, part := range instruction.Get("parts").Array() {
			if part.Get("functionCall").Exists() || part.Get("functionResponse").Exists() || part.Get("function_call").Exists() || part.Get("function_response").Exists() {
				continue
			}
			appendAntigravityTextChange(&changes, fmt.Sprintf("%s.parts.%d.text", path, i), part.Get("text"), matcher)
		}
	}
	for _, path := range []string{"request.contents", "contents"} {
		for i, content := range doc.Get(path).Array() {
			role := content.Get("role").String()
			if role != "user" && role != "model" && role != "assistant" {
				continue
			}
			for j, part := range content.Get("parts").Array() {
				if part.Get("functionCall").Exists() || part.Get("functionResponse").Exists() || part.Get("function_call").Exists() || part.Get("function_response").Exists() {
					continue
				}
				appendAntigravityTextChange(&changes, fmt.Sprintf("%s.%d.parts.%d.text", path, i, j), part.Get("text"), matcher)
			}
		}
	}
	for _, change := range changes {
		if errSet := doc.Set(change.path, change.text); errSet != nil {
			return fmt.Errorf("rewrite Antigravity message text: %w", errSet)
		}
	}
	return nil
}

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
