// NovelChat is the novel-specific chat page. It wraps the standard
// Transcript + Composer pair with a role indicator and novel-specific
// slash commands (/arc, /chapter, /character, /world, /review,
// /snapshot, /consistency).

import { useCallback, useState } from "react";
import { BookOpen, Pencil, RefreshCw } from "lucide-react";

export interface NovelChatProps {
  projectName: string;
  genre: string;
  phase: string;
  role: string;
}

export function NovelChat({ projectName, genre, phase, role }: NovelChatProps) {
  const [roleLabel, setRoleLabel] = useState(role);

  const roleLabels: Record<string, string> = {
    world_builder: "世界观构建师",
    arc_master: "大纲规划师",
    plot_planner: "剧情策划",
    plot_writer: "写手",
    reviewer: "审查员",
  };

  const phaseLabels: Record<string, string> = {
    setting: "设定",
    planning: "规划",
    writing: "写作",
    reviewing: "审查",
    completed: "已完成",
  };

  const cycleRole = useCallback(() => {
    const roles = ["world_builder", "arc_master", "plot_planner", "plot_writer", "reviewer"];
    const idx = roles.indexOf(roleLabel);
    setRoleLabel(roles[(idx + 1) % roles.length]);
  }, [roleLabel]);

  return (
    <div className="novel-chat">
      <div className="novel-chat-header">
        <div className="novel-chat-project">
          <BookOpen size={16} />
          <span className="novel-chat-name">{projectName}</span>
          <span className="novel-chat-genre">{genre}</span>
        </div>
        <div className="novel-chat-meta">
          <span className="novel-chat-phase">{phaseLabels[phase] || phase}</span>
          <button className="novel-chat-role-btn" onClick={cycleRole} title="切换角色">
            <Pencil size={14} />
            <span>{roleLabels[roleLabel] || roleLabel}</span>
            <RefreshCw size={12} />
          </button>
        </div>
      </div>
      <div className="novel-chat-body">
        <p className="novel-chat-placeholder">
          在此输入写作指令，或使用 /chapter /world /character 等斜杠命令
        </p>
      </div>
    </div>
  );
}
