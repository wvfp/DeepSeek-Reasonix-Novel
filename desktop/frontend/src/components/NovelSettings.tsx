// NovelSettings is the reasonix.toml editor page with novel-specific fields.

import { useState } from "react";
import { Settings, Save } from "lucide-react";

export interface NovelSettingsData {
  genre: string;
  author: string;
  defaultModel: string;
  reviewerModel: string;
  antiAiEnabled: boolean;
  antiAiLayers: string[];
  temperature: Record<string, number>;
}

export interface NovelSettingsProps {
  config: NovelSettingsData;
  onSave: (config: NovelSettingsData) => void;
}

const GENRE_OPTIONS = [
  { value: "fantasy", label: "通用奇幻" },
  { value: "xianxia", label: "玄幻仙侠" },
  { value: "urban", label: "都市言情" },
  { value: "infinite-flow", label: "无限流" },
  { value: "apocalypse", label: "末日求生" },
  { value: "sci-fi", label: "科幻未来" },
  { value: "horror", label: "恐怖悬疑" },
];

const ANTI_AI_LAYERS = [
  { value: "adverb_overuse", label: "副词滥用" },
  { value: "emotion_tagging", label: "情感标签" },
  { value: "dialog_formality", label: "对话格式化" },
  { value: "structure_closure", label: "结构闭合" },
  { value: "transition_formula", label: "过渡公式" },
  { value: "summary_tendency", label: "总结倾向" },
  { value: "info_exposition", label: "信息说明" },
];

export function NovelSettings({ config, onSave }: NovelSettingsProps) {
  const [data, setData] = useState<NovelSettingsData>({ ...config });

  const toggleLayer = (layer: string) => {
    setData((prev) => ({
      ...prev,
      antiAiLayers: prev.antiAiLayers.includes(layer)
        ? prev.antiAiLayers.filter((l) => l !== layer)
        : [...prev.antiAiLayers, layer],
    }));
  };

  return (
    <div className="novel-settings">
      <div className="novel-settings-header">
        <Settings size={20} />
        <h3>小说项目设置</h3>
      </div>
      <div className="novel-settings-body">
        <div className="novel-settings-section">
          <h4>基本信息</h4>
          <label className="novel-settings-field">
            <span>作者</span>
            <input
              type="text"
              value={data.author}
              onChange={(e) => setData((p) => ({ ...p, author: e.target.value }))}
            />
          </label>
          <label className="novel-settings-field">
            <span>题材</span>
            <select
              value={data.genre}
              onChange={(e) => setData((p) => ({ ...p, genre: e.target.value }))}
            >
              {GENRE_OPTIONS.map((opt) => (
                <option key={opt.value} value={opt.value}>
                  {opt.label}
                </option>
              ))}
            </select>
          </label>
        </div>

        <div className="novel-settings-section">
          <h4>模型配置</h4>
          <label className="novel-settings-field">
            <span>默认模型</span>
            <input
              type="text"
              value={data.defaultModel}
              onChange={(e) => setData((p) => ({ ...p, defaultModel: e.target.value }))}
              placeholder="deepseek-flash"
            />
          </label>
          <label className="novel-settings-field">
            <span>审查模型</span>
            <input
              type="text"
              value={data.reviewerModel}
              onChange={(e) => setData((p) => ({ ...p, reviewerModel: e.target.value }))}
              placeholder="deepseek-pro"
            />
          </label>
        </div>

        <div className="novel-settings-section">
          <h4>Anti-AI 规则</h4>
          <label className="novel-settings-toggle">
            <input
              type="checkbox"
              checked={data.antiAiEnabled}
              onChange={(e) => setData((p) => ({ ...p, antiAiEnabled: e.target.checked }))}
            />
            <span>启用 Anti-AI 检测</span>
          </label>
          {data.antiAiEnabled && (
            <div className="novel-settings-layers">
              {ANTI_AI_LAYERS.map((layer) => (
                <label key={layer.value} className="novel-settings-toggle">
                  <input
                    type="checkbox"
                    checked={data.antiAiLayers.includes(layer.value)}
                    onChange={() => toggleLayer(layer.value)}
                  />
                  <span>{layer.label}</span>
                </label>
              ))}
            </div>
          )}
        </div>

        <button className="novel-settings-save" onClick={() => onSave(data)}>
          <Save size={16} />
          <span>保存设置</span>
        </button>
      </div>
    </div>
  );
}
