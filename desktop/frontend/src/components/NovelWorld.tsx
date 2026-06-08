// NovelWorld is the world-building / character / knowledge graph browser page.

import { useState } from "react";
import { Globe, Users, Share2, ChevronRight, ChevronDown } from "lucide-react";

export interface WorldEntry {
  id: string;
  name: string;
  description: string;
  type: string;
}

export interface CharacterEntry {
  id: string;
  name: string;
  description: string;
  roleType: string;
}

export interface KGEdge {
  from: string;
  to: string;
  relation: string;
}

export interface NovelWorldProps {
  worlds: WorldEntry[];
  characters: CharacterEntry[];
  edges: KGEdge[];
}

type Tab = "worlds" | "characters" | "graph";

export function NovelWorld({ worlds, characters, edges }: NovelWorldProps) {
  const [tab, setTab] = useState<Tab>("worlds");
  const [expandedId, setExpandedId] = useState<string | null>(null);

  const tabs: { key: Tab; label: string; icon: typeof Globe }[] = [
    { key: "worlds", label: "世界观", icon: Globe },
    { key: "characters", label: "角色", icon: Users },
    { key: "graph", label: "知识图谱", icon: Share2 },
  ];

  return (
    <div className="novel-world">
      <div className="novel-world-tabs">
        {tabs.map(({ key, label, icon: Icon }) => (
          <button
            key={key}
            className={`novel-world-tab ${tab === key ? "novel-world-tab-active" : ""}`}
            onClick={() => setTab(key)}
          >
            <Icon size={16} />
            <span>{label}</span>
          </button>
        ))}
      </div>
      <div className="novel-world-content">
        {tab === "worlds" && (
          <div className="novel-world-list">
            {worlds.length === 0 && <p className="novel-world-empty">尚无世界观条目</p>}
            {worlds.map((w) => (
              <div key={w.id} className="novel-world-item">
                <button
                  className="novel-world-item-header"
                  onClick={() => setExpandedId(expandedId === w.id ? null : w.id)}
                >
                  {expandedId === w.id ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
                  <span className="novel-world-item-name">{w.name}</span>
                  <span className="novel-world-item-type">{w.type}</span>
                </button>
                {expandedId === w.id && (
                  <div className="novel-world-item-body">
                    <p>{w.description || "暂无描述"}</p>
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
        {tab === "characters" && (
          <div className="novel-world-list">
            {characters.length === 0 && <p className="novel-world-empty">尚无角色</p>}
            {characters.map((c) => (
              <div key={c.id} className="novel-world-item">
                <button
                  className="novel-world-item-header"
                  onClick={() => setExpandedId(expandedId === c.id ? null : c.id)}
                >
                  {expandedId === c.id ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
                  <span className="novel-world-item-name">{c.name}</span>
                  <span className="novel-world-item-type">{c.roleType}</span>
                </button>
                {expandedId === c.id && (
                  <div className="novel-world-item-body">
                    <p>{c.description || "暂无描述"}</p>
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
        {tab === "graph" && (
          <div className="novel-world-graph">
            {edges.length === 0 && <p className="novel-world-empty">尚无知识图谱边</p>}
            {edges.map((e, i) => (
              <div key={i} className="novel-world-edge">
                <span className="novel-world-edge-node">{e.from}</span>
                <span className="novel-world-edge-relation">{e.relation}</span>
                <span className="novel-world-edge-node">{e.to}</span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
