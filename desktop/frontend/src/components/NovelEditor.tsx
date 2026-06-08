// NovelEditor is the chapter editing page with inline anti-AI warnings,
// character voice diff, and foreshadow resolution status.

import { useState } from "react";
import { FileText, AlertTriangle, CheckCircle, Eye } from "lucide-react";

export interface ChapterData {
  id: string;
  title: string;
  content: string;
  wordCount: number;
  status: string;
  antiAiWarnings: AntiAiWarning[];
  voiceDrifts: VoiceDrift[];
  foreshadows: ForeshadowStatus[];
}

export interface AntiAiWarning {
  pattern: string;
  position: number;
  replacement: string;
}

export interface VoiceDrift {
  characterName: string;
  deviation: string;
}

export interface ForeshadowStatus {
  description: string;
  status: "planted" | "developing" | "resolved" | "abandoned";
}

export interface NovelEditorProps {
  chapter?: ChapterData;
  onSave: (content: string) => void;
}

export function NovelEditor({ chapter, onSave }: NovelEditorProps) {
  const [content, setContent] = useState(chapter?.content || "");
  const [showWarnings, setShowWarnings] = useState(true);

  if (!chapter) {
    return (
      <div className="novel-editor novel-editor-empty">
        <FileText size={48} strokeWidth={1} />
        <p>选择一个章节开始编辑</p>
      </div>
    );
  }

  const warningCount = chapter.antiAiWarnings.length;
  const driftCount = chapter.voiceDrifts.length;
  const unresolvedForeshadows = chapter.foreshadows.filter(
    (f) => f.status === "planted" || f.status === "developing"
  ).length;

  return (
    <div className="novel-editor">
      <div className="novel-editor-toolbar">
        <h3 className="novel-editor-title">{chapter.title}</h3>
        <div className="novel-editor-stats">
          <span>{chapter.wordCount} 字</span>
          <span className={`novel-editor-status novel-editor-status-${chapter.status}`}>
            {chapter.status === "draft" ? "草稿" : chapter.status === "reviewed" ? "已审" : chapter.status}
          </span>
        </div>
        <div className="novel-editor-indicators">
          {warningCount > 0 && (
            <button className="novel-editor-indicator novel-editor-warning" onClick={() => setShowWarnings(!showWarnings)} title="Anti-AI 警告">
              <AlertTriangle size={14} />
              <span>{warningCount}</span>
            </button>
          )}
          {driftCount > 0 && (
            <span className="novel-editor-indicator novel-editor-drift" title="声纹偏移">
              <Eye size={14} />
              <span>{driftCount}</span>
            </span>
          )}
          {unresolvedForeshadows > 0 && (
            <span className="novel-editor-indicator novel-editor-foreshadow" title="未解决伏笔">
              <CheckCircle size={14} />
              <span>{unresolvedForeshadows}</span>
            </span>
          )}
        </div>
        <button className="novel-editor-save" onClick={() => onSave(content)}>
          保存
        </button>
      </div>
      <div className="novel-editor-content">
        <textarea
          className="novel-editor-textarea"
          value={content}
          onChange={(e) => setContent(e.target.value)}
          placeholder="章节内容..."
        />
        {showWarnings && warningCount > 0 && (
          <div className="novel-editor-warnings-panel">
            <h4>Anti-AI 警告</h4>
            {chapter.antiAiWarnings.map((w, i) => (
              <div key={i} className="novel-editor-warning-item">
                <AlertTriangle size={12} />
                <span>「{w.pattern}」→ 建议替换为「{w.replacement}」</span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
