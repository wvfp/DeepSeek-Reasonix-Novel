package style

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// AnchorDimension — 统一维度接口
// ---------------------------------------------------------------------------

// AnchorDimension 定义风格锚点的一个可提取、可格式化维度。
// 所有维度（内置或新增）均实现此接口，以便 anchor.go 统一调度。
type AnchorDimension interface {
	// Name 返回维度的唯一标识名，如 "sentence_length"。
	Name() string
	// Extract 从章节文本切片中提取该维度的原始值。
	Extract(chapters []string) (value interface{}, err error)
	// Format 将 Extract 返回的原始值渲染为可读字符串，供 prompt 使用。
	Format(value interface{}) string
}

// ---------------------------------------------------------------------------
// 内置 5 个维度
// ---------------------------------------------------------------------------

// SentenceLengthDimension 统计句长分布（5 个桶）。
type SentenceLengthDimension struct{}

func (SentenceLengthDimension) Name() string { return "sentence_length" }

func (SentenceLengthDimension) Extract(chapters []string) (interface{}, error) {
	body := strings.Join(chapters, "\n")
	stats := extractLengthStats(body)
	return buildDistribution(stats.sentenceLengths, sentenceBuckets), nil
}

func (SentenceLengthDimension) Format(value interface{}) string {
	dist, ok := value.([]int)
	if !ok || len(dist) != 5 {
		return ""
	}
	return fmt.Sprintf("sentence_length_dist (chars: <10, 10-20, 20-30, 30-50, >50): %v", dist)
}

// ParagraphLengthDimension 统计段长分布（5 个桶）。
type ParagraphLengthDimension struct{}

func (ParagraphLengthDimension) Name() string { return "paragraph_length" }

func (ParagraphLengthDimension) Extract(chapters []string) (interface{}, error) {
	body := strings.Join(chapters, "\n")
	stats := extractLengthStats(body)
	return buildDistribution(stats.paragraphLengths, paragraphBuckets), nil
}

func (ParagraphLengthDimension) Format(value interface{}) string {
	dist, ok := value.([]int)
	if !ok || len(dist) != 5 {
		return ""
	}
	return fmt.Sprintf("paragraph_length_dist (chars: <50, 50-100, 100-200, 200-500, >500): %v", dist)
}

// DialogueRatioDimension 统计对话占全文比例。
type DialogueRatioDimension struct{}

func (DialogueRatioDimension) Name() string { return "dialogue_ratio" }

func (DialogueRatioDimension) Extract(chapters []string) (interface{}, error) {
	body := strings.Join(chapters, "\n")
	stats := extractLengthStats(body)
	if stats.totalChars == 0 {
		return 0.0, nil
	}
	return float64(stats.dialogueCount) / float64(stats.totalChars), nil
}

func (DialogueRatioDimension) Format(value interface{}) string {
	ratio, ok := value.(float64)
	if !ok {
		return ""
	}
	return fmt.Sprintf("dialogue_ratio: %.2f", ratio)
}

// BigramFrequencyDimension 提取 Top-50 CJK 二元组。
type BigramFrequencyDimension struct{}

func (BigramFrequencyDimension) Name() string { return "bigram_frequency" }

func (BigramFrequencyDimension) Extract(chapters []string) (interface{}, error) {
	body := strings.Join(chapters, "\n")
	return extractBigrams(body), nil
}

func (BigramFrequencyDimension) Format(value interface{}) string {
	bigrams, ok := value.([][2]any)
	if !ok || len(bigrams) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "top_bigrams: %d entries (top 10 shown)\n", len(bigrams))
	for i, bg := range bigrams {
		if i >= 10 {
			break
		}
		word, _ := bg[0].(string)
		count, _ := bg[1].(float64)
		fmt.Fprintf(&b, "  - %s × %d\n", word, int(count))
	}
	return strings.TrimRight(b.String(), "\n")
}

// PunctuationFrequencyDimension 统计标点符号出现频次。
type PunctuationFrequencyDimension struct{}

func (PunctuationFrequencyDimension) Name() string { return "punctuation_frequency" }

func (PunctuationFrequencyDimension) Extract(chapters []string) (interface{}, error) {
	body := strings.Join(chapters, "\n")
	return extractPunctuation(body), nil
}

func (PunctuationFrequencyDimension) Format(value interface{}) string {
	freq, ok := value.(map[string]int)
	if !ok || len(freq) == 0 {
		return ""
	}
	keys := make([]string, 0, len(freq))
	for k := range freq {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("punctuation_freq: ")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s=%d", k, freq[k])
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// 新增 3 个维度
// ---------------------------------------------------------------------------

// RhetoricDimension 检测常见修辞手法：比喻、排比、夸张。
type RhetoricDimension struct{}

func (RhetoricDimension) Name() string { return "rhetoric" }

// RhetoricProfile 保存三种修辞的计数。
type RhetoricProfile struct {
	Metaphor  int `json:"metaphor"`
	Parallel  int `json:"parallel"`
	Hyperbole int `json:"hyperbole"`
}

var (
	metaphorRe  = regexp.MustCompile(`像|如同|仿佛|似|犹如`)
	parallelRe  = regexp.MustCompile(`(([^，。！？\n]+，){2,}[^，。！？\n]+[。！？])`)
	hyperboleRe = regexp.MustCompile(`千万|亿万|无尽|破天|惊天|泣鬼神|海枯石烂`)
)

func (RhetoricDimension) Extract(chapters []string) (interface{}, error) {
	body := strings.Join(chapters, "\n")
	return RhetoricProfile{
		Metaphor:  len(metaphorRe.FindAllString(body, -1)),
		Parallel:  len(parallelRe.FindAllString(body, -1)),
		Hyperbole: len(hyperboleRe.FindAllString(body, -1)),
	}, nil
}

func (RhetoricDimension) Format(value interface{}) string {
	prof, ok := value.(RhetoricProfile)
	if !ok {
		return ""
	}
	return fmt.Sprintf("rhetoric (metaphor=%d, parallel=%d, hyperbole=%d)", prof.Metaphor, prof.Parallel, prof.Hyperbole)
}

// EmotionDimension 统计情感倾向词频：积极 / 消极 / 中性。
type EmotionDimension struct{}

func (EmotionDimension) Name() string { return "emotion" }

// EmotionProfile 保存三类情感词的出现次数。
type EmotionProfile struct {
	Positive int `json:"positive"`
	Negative int `json:"negative"`
	Neutral  int `json:"neutral"`
}

var (
	positiveEmotionRe = regexp.MustCompile(`高兴|开心|快乐|喜悦|兴奋|满足|幸福|自豪|欣慰|感动`)
	negativeEmotionRe = regexp.MustCompile(`悲伤|痛苦|愤怒|绝望|恐惧|焦虑|沮丧|失落|孤独|怨恨`)
	neutralEmotionRe  = regexp.MustCompile(`平静|淡然|沉默|无语|发呆|思索|观望|等待|行走|坐下`)
)

func (EmotionDimension) Extract(chapters []string) (interface{}, error) {
	body := strings.Join(chapters, "\n")
	return EmotionProfile{
		Positive: len(positiveEmotionRe.FindAllString(body, -1)),
		Negative: len(negativeEmotionRe.FindAllString(body, -1)),
		Neutral:  len(neutralEmotionRe.FindAllString(body, -1)),
	}, nil
}

func (EmotionDimension) Format(value interface{}) string {
	prof, ok := value.(EmotionProfile)
	if !ok {
		return ""
	}
	total := prof.Positive + prof.Negative + prof.Neutral
	if total == 0 {
		return "emotion: none detected"
	}
	return fmt.Sprintf("emotion (positive=%d, negative=%d, neutral=%d, total=%d)", prof.Positive, prof.Negative, prof.Neutral, total)
}

// PerspectiveDimension 判断叙事视角稳定性（第一人称 / 第三人称）。
type PerspectiveDimension struct{}

func (PerspectiveDimension) Name() string { return "perspective" }

// PerspectiveProfile 保存视角统计结果。
type PerspectiveProfile struct {
	FirstPerson  int     `json:"first_person"`
	ThirdPerson  int     `json:"third_person"`
	Dominant     string  `json:"dominant"`
	Stability    float64 `json:"stability"`
}

var (
	firstPersonRe = regexp.MustCompile(`(我|我们|咱|咱们)`)
	thirdPersonRe = regexp.MustCompile(`(他|她|它|他们|她们|它们|祂)`)
)

func (PerspectiveDimension) Extract(chapters []string) (interface{}, error) {
	body := strings.Join(chapters, "\n")
	fp := len(firstPersonRe.FindAllString(body, -1))
	tp := len(thirdPersonRe.FindAllString(body, -1))
	total := fp + tp
	if total == 0 {
		return PerspectiveProfile{Dominant: "unknown", Stability: 0}, nil
	}
	dominant := "third"
	if fp > tp {
		dominant = "first"
	}
	stability := float64(max(fp, tp)) / float64(total)
	return PerspectiveProfile{
		FirstPerson: fp,
		ThirdPerson: tp,
		Dominant:    dominant,
		Stability:   stability,
	}, nil
}

func (PerspectiveDimension) Format(value interface{}) string {
	prof, ok := value.(PerspectiveProfile)
	if !ok {
		return ""
	}
	return fmt.Sprintf("perspective (dominant=%s, stability=%.2f, first=%d, third=%d)", prof.Dominant, prof.Stability, prof.FirstPerson, prof.ThirdPerson)
}

// ---------------------------------------------------------------------------
// 维度注册表
// ---------------------------------------------------------------------------

// DefaultDimensions 返回全部 8 个内置维度，顺序即输出顺序。
func DefaultDimensions() []AnchorDimension {
	return []AnchorDimension{
		SentenceLengthDimension{},
		ParagraphLengthDimension{},
		DialogueRatioDimension{},
		BigramFrequencyDimension{},
		PunctuationFrequencyDimension{},
		RhetoricDimension{},
		EmotionDimension{},
		PerspectiveDimension{},
	}
}

// DimensionByName 按名称查找维度，未找到返回 nil。
func DimensionByName(name string) AnchorDimension {
	for _, d := range DefaultDimensions() {
		if d.Name() == name {
			return d
		}
	}
	return nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
