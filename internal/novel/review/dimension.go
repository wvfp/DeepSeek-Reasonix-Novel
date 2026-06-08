// Package review provides configurable review dimensions for chapter
// quality assessment. Each dimension implements the ReviewDimension
// interface and can be selected via config.json.
package review

import (
	"fmt"
	"strings"

	"reasonix/internal/novel/domain"
)

// ReviewDimension is a single axis of chapter quality. The
// chapter_review tool loads the active set from config, calls
// Evaluate for each dimension, and persists the results.
type ReviewDimension interface {
	Name() string
	Evaluate(chapter *domain.Chapter) (score float64, issues []domain.Issue)
	Criteria() string
}

// ---------------------------------------------------------------------------
// Built-in dimensions
// ---------------------------------------------------------------------------

// PlotDimension evaluates narrative tension and arc progression.
type PlotDimension struct{}

func (PlotDimension) Name() string { return domain.ReviewDimPlot }

func (PlotDimension) Evaluate(ch *domain.Chapter) (float64, []domain.Issue) {
	return heuristicScore(ch, "plot",
		"0-3 分：情节平淡，缺乏冲突或转折；",
		"4-6 分：有起伏，但高潮不够突出；",
		"7-10 分：情节精彩，冲突层层递进，转折自然。")
}

func (PlotDimension) Criteria() string {
	return "情节：0-3 平淡，4-6 有起伏，7-10 精彩"
}

// CharacterDimension evaluates character depth and development.
type CharacterDimension struct{}

func (CharacterDimension) Name() string { return domain.ReviewDimCharacter }

func (CharacterDimension) Evaluate(ch *domain.Chapter) (float64, []domain.Issue) {
	return heuristicScore(ch, "character",
		"0-3 分：角色扁平，动机模糊；",
		"4-6 分：有一定层次，但成长线不明显；",
		"7-10 分：角色立体，动机清晰，成长自然。")
}

func (CharacterDimension) Criteria() string {
	return "角色：0-3 扁平，4-6 有层次，7-10 立体"
}

// StyleDimension evaluates prose quality and readability.
type StyleDimension struct{}

func (StyleDimension) Name() string { return domain.ReviewDimStyle }

func (StyleDimension) Evaluate(ch *domain.Chapter) (float64, []domain.Issue) {
	return heuristicScore(ch, "style",
		"0-3 分：文笔粗糙，病句多，阅读卡顿；",
		"4-6 分：文笔流畅，偶有冗余；",
		"7-10 分：文笔优美，画面感强，节奏舒适。")
}

func (StyleDimension) Criteria() string {
	return "文笔：0-3 粗糙，4-6 流畅，7-10 优美"
}

// ConsistencyDimension evaluates internal logic and continuity.
type ConsistencyDimension struct{}

func (ConsistencyDimension) Name() string { return domain.ReviewDimConsistency }

func (ConsistencyDimension) Evaluate(ch *domain.Chapter) (float64, []domain.Issue) {
	return heuristicScore(ch, "consistency",
		"0-3 分：多处矛盾，设定前后不一致；",
		"4-6 分：有小瑕疵，但不影响主线；",
		"7-10 分：逻辑自洽，设定严谨，完美衔接。")
}

func (ConsistencyDimension) Criteria() string {
	return "一致性：0-3 多处矛盾，4-6 小瑕疵，7-10 完美"
}

// PacingDimension evaluates narrative rhythm and tempo.
type PacingDimension struct{}

func (PacingDimension) Name() string { return domain.ReviewDimPacing }

func (PacingDimension) Evaluate(ch *domain.Chapter) (float64, []domain.Issue) {
	return heuristicScore(ch, "pacing",
		"0-3 分：节奏拖沓或过于急促，详略失当；",
		"4-6 分：有张有弛，但过渡稍显生硬；",
		"7-10 分：节奏恰到好处，快慢交替自然。")
}

func (PacingDimension) Criteria() string {
	return "节奏：0-3 拖沓/急促，4-6 有张有弛，7-10 恰到好处"
}

// ForeshadowDimension evaluates setup and payoff quality.
type ForeshadowDimension struct{}

func (ForeshadowDimension) Name() string { return domain.ReviewDimForeshadow }

func (ForeshadowDimension) Evaluate(ch *domain.Chapter) (float64, []domain.Issue) {
	return heuristicScore(ch, "foreshadow",
		"0-3 分：无伏笔或铺垫突兀；",
		"4-6 分：有铺垫，但回收不够有力；",
		"7-10 分：伏笔精妙，前后呼应，意料之外情理之中。")
}

func (ForeshadowDimension) Criteria() string {
	return "伏笔：0-3 无/突兀，4-6 有铺垫，7-10 精妙"
}

// HookDimension evaluates opening and cliffhanger appeal.
type HookDimension struct{}

func (HookDimension) Name() string { return domain.ReviewDimHook }

func (HookDimension) Evaluate(ch *domain.Chapter) (float64, []domain.Issue) {
	return heuristicScore(ch, "hook",
		"0-3 分：开头无吸引力，结尾平淡；",
		"4-6 分：有悬念，但张力不足；",
		"7-10 分：开头抓人，结尾欲罢不能，悬念强烈。")
}

func (HookDimension) Criteria() string {
	return "钩子：0-3 无吸引力，4-6 有悬念，7-10 欲罢不能"
}

// ValuesDimension evaluates thematic depth and moral stance.
type ValuesDimension struct{}

func (ValuesDimension) Name() string { return domain.ReviewDimValues }

func (ValuesDimension) Evaluate(ch *domain.Chapter) (float64, []domain.Issue) {
	return heuristicScore(ch, "values",
		"0-3 分：价值观偏差或传达混乱；",
		"4-6 分：价值观正常，但缺乏深度；",
		"7-10 分：价值观深刻，主题鲜明，引人思考。")
}

func (ValuesDimension) Criteria() string {
	return "价值观：0-3 偏差，4-6 正常，7-10 深刻"
}

// ---------------------------------------------------------------------------
// Registry
// ---------------------------------------------------------------------------

var dimensionRegistry = map[string]ReviewDimension{
	domain.ReviewDimPlot:        PlotDimension{},
	domain.ReviewDimCharacter:   CharacterDimension{},
	domain.ReviewDimStyle:       StyleDimension{},
	domain.ReviewDimConsistency: ConsistencyDimension{},
	domain.ReviewDimPacing:      PacingDimension{},
	domain.ReviewDimForeshadow:  ForeshadowDimension{},
	domain.ReviewDimHook:        HookDimension{},
	domain.ReviewDimValues:      ValuesDimension{},
}

// LoadDimensions returns the active dimensions filtered by names.
// Unknown names are silently ignored so a typo in config.json does
// not crash the pipeline.
func LoadDimensions(names []string) []ReviewDimension {
	if len(names) == 0 {
		// Default to all built-in dimensions in stable order.
		names = []string{
			domain.ReviewDimPlot,
			domain.ReviewDimCharacter,
			domain.ReviewDimStyle,
			domain.ReviewDimConsistency,
			domain.ReviewDimPacing,
			domain.ReviewDimForeshadow,
			domain.ReviewDimHook,
			domain.ReviewDimValues,
		}
	}
	out := make([]ReviewDimension, 0, len(names))
	for _, n := range names {
		if d, ok := dimensionRegistry[n]; ok {
			out = append(out, d)
		}
	}
	return out
}

// AllDimensionNames returns the names of every built-in dimension.
func AllDimensionNames() []string {
	return []string{
		domain.ReviewDimPlot,
		domain.ReviewDimCharacter,
		domain.ReviewDimStyle,
		domain.ReviewDimConsistency,
		domain.ReviewDimPacing,
		domain.ReviewDimForeshadow,
		domain.ReviewDimHook,
		domain.ReviewDimValues,
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// heuristicScore is a placeholder evaluator that returns a neutral
// score and an info-level issue describing the dimension's criteria.
// In Phase 7.2+ this will be replaced by LLM-based or rule-based
// scoring; for now it gives the pipeline a concrete (score, issues)
// pair so the config layer can be wired end-to-end.
func heuristicScore(ch *domain.Chapter, dim, low, mid, high string) (float64, []domain.Issue) {
	score := 5.0
	if ch.WordCount < 500 {
		score = 3.0
	}
	msg := fmt.Sprintf("%s 评分标准：%s %s %s", dim, low, mid, high)
	return score, []domain.Issue{
		{Severity: domain.SeverityInfo, Description: msg},
	}
}

// BuildPromptCriteria concatenates the Criteria() strings of the
// given dimensions into a multi-line block suitable for embedding
// in a LLM system prompt.
func BuildPromptCriteria(dims []ReviewDimension) string {
	var b strings.Builder
	for i, d := range dims {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("- ")
		b.WriteString(d.Criteria())
	}
	return b.String()
}
