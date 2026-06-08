// NovelPacing is the pacing visualization page. It shows chapter length,
// conflict density, and narrative rhythm as a bar chart.

import { useMemo } from "react";
import { BarChart3 } from "lucide-react";

export interface PacingChapter {
  number: number;
  title: string;
  wordCount: number;
  conflictDensity: number; // 0-1
  dialogueRatio: number;   // 0-1
}

export interface NovelPacingProps {
  chapters: PacingChapter[];
}

export function NovelPacing({ chapters }: NovelPacingProps) {
  const maxWords = useMemo(
    () => Math.max(1, ...chapters.map((c) => c.wordCount)),
    [chapters]
  );

  if (chapters.length === 0) {
    return (
      <div className="novel-pacing novel-pacing-empty">
        <BarChart3 size={48} strokeWidth={1} />
        <p>尚无章节数据</p>
      </div>
    );
  }

  return (
    <div className="novel-pacing">
      <h3 className="novel-pacing-title">节奏图</h3>
      <div className="novel-pacing-chart">
        {chapters.map((ch) => (
          <div key={ch.number} className="novel-pacing-bar-group">
            <div className="novel-pacing-bar-container">
              <div
                className="novel-pacing-bar novel-pacing-bar-words"
                style={{ height: `${(ch.wordCount / maxWords) * 100}%` }}
                title={`字数: ${ch.wordCount}`}
              />
              <div
                className="novel-pacing-bar novel-pacing-bar-conflict"
                style={{ height: `${ch.conflictDensity * 100}%` }}
                title={`冲突密度: ${(ch.conflictDensity * 100).toFixed(0)}%`}
              />
              <div
                className="novel-pacing-bar novel-pacing-bar-dialogue"
                style={{ height: `${ch.dialogueRatio * 100}%` }}
                title={`对话占比: ${(ch.dialogueRatio * 100).toFixed(0)}%`}
              />
            </div>
            <span className="novel-pacing-label">{ch.number}</span>
          </div>
        ))}
      </div>
      <div className="novel-pacing-legend">
        <span className="novel-pacing-legend-item">
          <span className="novel-pacing-legend-swatch novel-pacing-words-swatch" />
          字数
        </span>
        <span className="novel-pacing-legend-item">
          <span className="novel-pacing-legend-swatch novel-pacing-conflict-swatch" />
          冲突密度
        </span>
        <span className="novel-pacing-legend-item">
          <span className="novel-pacing-legend-swatch novel-pacing-dialogue-swatch" />
          对话占比
        </span>
      </div>
    </div>
  );
}
