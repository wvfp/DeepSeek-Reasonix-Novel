// NovelReview is the 8-dimension quality review report page.

import { useMemo } from "react";
import { Star, AlertCircle, CheckCircle2 } from "lucide-react";

export interface ReviewDimension {
  name: string;
  score: number;    // 0-10
  issues: string[];
}

export interface NovelReviewProps {
  chapterTitle: string;
  dimensions: ReviewDimension[];
  overallScore: number;
  summary: string;
}

const DIMENSION_LABELS: Record<string, string> = {
  plot: "剧情",
  character: "人物",
  pacing: "节奏",
  dialogue: "对话",
  setting: "场景",
  consistency: "一致性",
  style: "文笔",
  hook: "钩子",
};

export function NovelReview({ chapterTitle, dimensions, overallScore, summary }: NovelReviewProps) {
  const totalIssues = useMemo(
    () => dimensions.reduce((sum, d) => sum + d.issues.length, 0),
    [dimensions]
  );

  const scoreColor = (score: number) => {
    if (score >= 8) return "novel-review-score-good";
    if (score >= 5) return "novel-review-score-ok";
    return "novel-review-score-bad";
  };

  return (
    <div className="novel-review">
      <div className="novel-review-header">
        <h3>{chapterTitle} — 审查报告</h3>
        <div className="novel-review-overall">
          <Star size={20} className={scoreColor(overallScore)} />
          <span className={`novel-review-score ${scoreColor(overallScore)}`}>
            {overallScore.toFixed(1)}
          </span>
          <span className="novel-review-total-issues">
            {totalIssues > 0 ? `${totalIssues} 个问题` : "无问题"}
          </span>
        </div>
      </div>
      {summary && <p className="novel-review-summary">{summary}</p>}
      <div className="novel-review-dimensions">
        {dimensions.map((dim) => (
          <div key={dim.name} className="novel-review-dimension">
            <div className="novel-review-dim-header">
              <span className="novel-review-dim-name">
                {DIMENSION_LABELS[dim.name] || dim.name}
              </span>
              <span className={`novel-review-dim-score ${scoreColor(dim.score)}`}>
                {dim.score.toFixed(1)}
              </span>
              <div className="novel-review-dim-bar">
                <div
                  className={`novel-review-dim-fill ${scoreColor(dim.score)}`}
                  style={{ width: `${dim.score * 10}%` }}
                />
              </div>
            </div>
            {dim.issues.length > 0 && (
              <ul className="novel-review-dim-issues">
                {dim.issues.map((issue, i) => (
                  <li key={i} className="novel-review-issue">
                    <AlertCircle size={12} />
                    <span>{issue}</span>
                  </li>
                ))}
              </ul>
            )}
            {dim.issues.length === 0 && (
              <div className="novel-review-dim-ok">
                <CheckCircle2 size={12} />
                <span>通过</span>
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
