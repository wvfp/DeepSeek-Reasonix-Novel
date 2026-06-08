// Package progress provides terminal progress reporting for the novel
// pipeline.
package progress

import (
	"fmt"
	"strings"
)

// ErrorCategory classifies errors into high-level groups.
type ErrorCategory int

const (
	// CategoryUnknown indicates an unclassified error.
	CategoryUnknown ErrorCategory = iota
	// CategoryNetwork indicates network connectivity or timeout errors.
	CategoryNetwork
	// CategoryConfig indicates misconfiguration or missing settings.
	CategoryConfig
	// CategoryAPI indicates LLM/API provider errors (rate limits, invalid keys, etc.).
	CategoryAPI
	// CategoryContent indicates content-related errors (format violations, safety, etc.).
	CategoryContent
	// CategoryIO indicates file system or disk I/O errors.
	CategoryIO
	// CategoryInternal indicates internal logic or unexpected errors.
	CategoryInternal
)

// String returns the Chinese name of the error category.
func (c ErrorCategory) String() string {
	switch c {
	case CategoryNetwork:
		return "网络错误"
	case CategoryConfig:
		return "配置错误"
	case CategoryAPI:
		return "API 错误"
	case CategoryContent:
		return "内容错误"
	case CategoryIO:
		return "文件错误"
	case CategoryInternal:
		return "内部错误"
	default:
		return "未知错误"
	}
}

// CategorizedError wraps an error with its category and a suggested fix.
type CategorizedError struct {
	Category    ErrorCategory
	OriginalErr error
	Message     string
	Suggestion  string
}

// Error implements the error interface.
func (e *CategorizedError) Error() string {
	if e.OriginalErr == nil {
		return e.Message
	}
	return fmt.Sprintf("[%s] %s: %s", e.Category.String(), e.Message, e.OriginalErr.Error())
}

// friendlyErrors maps low-level / technical error substrings to
// actionable, user-facing messages in Chinese.
var friendlyErrors = map[string]string{
	"no JSON object found":         "LLM 返回格式错误，请检查 temperature 是否过低",
	"paragraph too long":           "段落过长，已自动分割",
	"forbidden word detected":      "检测到禁用词，已自动替换",
	"no LLM configured":            "未配置 LLM，请先运行 novel init 或设置环境变量",
	"chapter_write: llm call":      "LLM 调用失败，请检查网络或 API 密钥",
	"chapter_write: parse llm":     "LLM 输出无法解析，请尝试提高 temperature",
	"chapter_write: load arc":      "加载主线失败，请确认 arc_id 是否正确",
	"write markdown":               "Markdown 文件写入失败，请检查磁盘空间或权限",
	"safety prompt violation":      "提示词包含敏感内容，已跳过安全检查",
	"safety content violation":     "生成内容包含敏感信息，已自动过滤",
}

// categoryPatterns maps error substrings to their categories.
var categoryPatterns = map[string]ErrorCategory{
	"connection refused":      CategoryNetwork,
	"timeout":                 CategoryNetwork,
	"no such host":            CategoryNetwork,
	"i/o timeout":             CategoryNetwork,
	"tls handshake":           CategoryNetwork,
	"no LLM configured":       CategoryConfig,
	"missing config":          CategoryConfig,
	"invalid config":          CategoryConfig,
	"api key":                 CategoryConfig,
	"unauthorized":            CategoryAPI,
	"rate limit":              CategoryAPI,
	"quota exceeded":          CategoryAPI,
	"invalid api key":         CategoryAPI,
	"model not found":         CategoryAPI,
	"bad request":             CategoryAPI,
	"no JSON object found":    CategoryContent,
	"paragraph too long":      CategoryContent,
	"forbidden word detected": CategoryContent,
	"safety prompt violation": CategoryContent,
	"safety content violation": CategoryContent,
	"write markdown":          CategoryIO,
	"permission denied":       CategoryIO,
	"no space left":           CategoryIO,
	"file not found":          CategoryIO,
	"chapter_write: load arc": CategoryInternal,
}

// suggestionMap provides fix suggestions per category.
var suggestionMap = map[ErrorCategory]string{
	CategoryNetwork:  "请检查网络连接，确认目标服务可达，或稍后重试。",
	CategoryConfig:   "请检查配置文件或环境变量，运行 novel init 重新初始化。",
	CategoryAPI:      "请检查 API 密钥是否有效，确认账户余额或配额，或降低调用频率。",
	CategoryContent:  "请调整提示词或生成参数（如 temperature），避免触发内容限制。",
	CategoryIO:       "请检查磁盘空间、文件权限及路径是否正确。",
	CategoryInternal: "请确认输入参数正确，若问题持续请提交 issue。",
	CategoryUnknown:  "请查看详细错误信息，或尝试重新执行。",
}

// FriendlyError looks up a human-readable explanation for err. If no
// mapping exists it returns the original error string.
func FriendlyError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	for pattern, friendly := range friendlyErrors {
		if strings.Contains(msg, pattern) {
			return friendly
		}
	}
	return msg
}

// FriendlyErrorf is a convenience wrapper around FriendlyError for
// string values.
func FriendlyErrorf(format string, args ...any) string {
	return FriendlyError(fmt.Errorf(format, args...))
}

// CategorizeError determines the category of an error.
func CategorizeError(err error) ErrorCategory {
	if err == nil {
		return CategoryUnknown
	}
	msg := strings.ToLower(err.Error())
	for pattern, cat := range categoryPatterns {
		if strings.Contains(msg, strings.ToLower(pattern)) {
			return cat
		}
	}
	return CategoryUnknown
}

// SuggestFix returns a user-friendly suggestion for a given error.
func SuggestFix(err error) string {
	cat := CategorizeError(err)
	if suggestion, ok := suggestionMap[cat]; ok {
		return suggestion
	}
	return suggestionMap[CategoryUnknown]
}

// WrapError categorizes an error and returns a CategorizedError with
// a friendly message and fix suggestion.
func WrapError(err error) *CategorizedError {
	if err == nil {
		return nil
	}
	cat := CategorizeError(err)
	return &CategorizedError{
		Category:    cat,
		OriginalErr: err,
		Message:     FriendlyError(err),
		Suggestion:  SuggestFix(err),
	}
}
