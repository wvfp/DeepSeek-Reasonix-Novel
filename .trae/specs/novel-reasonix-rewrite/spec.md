# Novel Reasonix Rewrite Spec

## Why

`g:\Code\Novel\DeepSeek-Reasonix-Novel` 当前是一个通用的 DeepSeek 优化 coding agent。我们要把它**深度改造**为**网文写作专用 agent**，完整迁移 `g:\Code\novel-plugin`（TypeScript / opencode plugin）里的所有领域能力——知识图谱、章纲引擎、5 个角色子 agent、anti-AI 规则、风格锚点、声纹、伏笔、6 个 genre pack——同时**保留** Reasonix 自身的核心优势（prefix 缓存、动态上下文压缩、storm breaker、sandbox、工具并行、Wails desktop、MCP plugin 机制）。

改造后用户能直接用一个 CLI：`novel chat / novel chapter / novel arc / novel world / novel character / novel review / novel setup`，后端连 DeepSeek / MiMo / OpenCode Zen 任意 provider，前端用 Wails 6 个 novel 专用 React 页面。

## What Changes

- **BREAKING** CLI 命令名 `reasonix` → `novel`（`novel chat` 替代 `reasonix chat`；原 `reasonix` 命令保留作为别名，输出 deprecation 提示）
- **BREAKING** 默认 `reasonix.toml` schema 改为 novel 专用（保留 [providers] / [plugins] / [permissions] / [sandbox] 段，新增 [novel] 段）
- **新增** `internal/novel/` 子树（domain / db / tools / roles / pipeline / style / genre / knowledge / chapter）
- **新增** SQLite 持久化（modernc.org/sqlite，纯 Go / CGO=0 / 13 张表）
- **新增** 5 个角色（World / Arc / Plot / Write / Review），通过**同 session 换 system prompt** 切换
- **新增** 12 个领域 tool（`novel_init / world_create / character_create / chapter_write / chapter_review / consistency_check / arc_generate / state_snapshot / crosscheck / query / progress / stats`）
- **新增** 4 阶段 pipeline（setting → planning → writing → reviewing）
- **新增** 6 个 genre pack（玄幻 / 都市 / 无限流 / 末日 / 科幻 / 恐怖）内嵌 JSON
- **新增** anti-AI 规则引擎、风格锚点提取、角色声纹追踪、伏笔系统
- **新增** Wails 内部 6 个 React 页面（chat / editor / pacing / review / world / settings）
- **保留** Reasonix 所有优势（compact.go / cache / storm breaker / tool parallel / 32KB 截断 / sandbox / MCP / extra_headers）
- **保留** Desktop 编译链路（Wails 2.11 / pnpm / gcc 已验证）

## Impact

- Affected specs: 配置、Agent、Provider、Tool、CLI、Desktop Web UI、Pipeline
- Affected code:
  - `cmd/reasonix/main.go` 内新增 `novel` 子命令（`reasonix` 保留为 alias）
  - `go.mod` 加 `modernc.org/sqlite` 依赖
  - `internal/` 完全保留，新增 `internal/novel/` 子树
  - `desktop/` 保留，React 页面换为 novel 专用

## ADDED Requirements

### Requirement: Novel 专用 CLI

The system SHALL expose a `novel` command with subcommands: `chat`, `chapter`, `arc`, `world`, `character`, `review`, `setup`, `stats`, `doctor`, `version`, `help`.

#### Scenario: 启动交互式写作
- **WHEN** user runs `novel chat --model deepseek-flash`
- **THEN** system starts a TUI session, loads `.novel-weaver/` project, and accepts novel-writing prompts

#### Scenario: 单章节写作
- **WHEN** user runs `novel chapter --arc "主线冲突 1" --world id:wu-xia`
- **THEN** system loads world/character context, runs PlotPlanner role, calls DeepSeek, writes chapter to `.novel-weaver/content/chapters/vol-1/ch{N}-{slug}.md`, and extracts facts/character states/knowledge graph edges in the same LLM call

### Requirement: SQLite 持久化（modernc.org/sqlite）

The system SHALL use modernc.org/sqlite (pure Go, CGO=0) to persist project state in `.novel-weaver/novel.db` with 13 tables:

- `projects`, `worlds`, `characters`, `chapters`, `reviews`
- `knowledge_graph_nodes`, `knowledge_graph_edges`, `entity_links`
- `chapter_facts`, `character_states`, `foreshadows`
- `outlines`, `aliases`, `schema_version`

#### Scenario: 数据库初始化
- **WHEN** user runs `novel setup` first time
- **THEN** system creates `.novel-weaver/` directory and `novel.db` with all 13 tables

#### Scenario: 跨会话恢复
- **WHEN** user restarts `novel chat` after previous session
- **THEN** system loads last chapter, character states, knowledge graph into context prefix

### Requirement: 5 角色同 Session 切换

The system SHALL implement 5 agent roles (WorldBuilder / ArcMaster / PlotPlanner / PlotWriter / Reviewer) that share the same session's messages array. Role switch is achieved by **replacing system prompt suffix** (not opening a new session), preserving Reasonix prefix cache.

#### Scenario: PlotWriter 写完一章后切换到 Reviewer
- **WHEN** PlotWriter role returns a chapter with completion marker
- **THEN** system replaces system prompt suffix with Reviewer instructions and calls DeepSeek again with the same messages history
- **AND** prefix cache hit rate SHALL stay > 70% across role switches

#### Scenario: WorldBuilder 在 setup 阶段独立运行
- **WHEN** user runs `novel world --create "九州大陆"`
- **THEN** system creates a fresh session with WorldBuilder system prompt only

### Requirement: 嵌入式事实提取

The system SHALL require PlotWriter role to return structured JSON in addition to chapter text: `{"chapter_text": "...", "facts": [...], "character_states": [...], "kg_edges": [...]}`. Go parser SHALL extract and batch-write to SQLite in the same call (no additional LLM round-trip).

#### Scenario: 写完第 3 章
- **WHEN** PlotWriter returns chapter 3 content
- **THEN** system parses 5-20 facts, 3-10 character state updates, 2-8 knowledge graph edges and writes them to SQLite
- **AND** SHALL NOT make any additional LLM calls for fact extraction

### Requirement: 12 个领域 Tool

The system SHALL register 12 tools in the `tool()` registry:

| Tool | 用途 |
|---|---|
| `novel_init` | 创建 `.novel-weaver/` 项目（必须第一个调用） |
| `world_create` / `world_query` / `world_link` | 世界观管理 |
| `character_create` / `character_query` / `character_update` | 角色管理 |
| `arc_generate` / `arc_update` | 章纲生成与更新 |
| `chapter_write` / `chapter_edit` / `chapter_continue` | 章节写作 |
| `chapter_review` / `review_fix` | 章节质量审查 |
| `consistency_check` | 5 维度一致性检查 |
| `crosscheck` | 跨章事实核查 |
| `state_snapshot` / `state_restore` | 状态快照 |

### Requirement: 200万字长篇小说支持（RAG + 分层上下文）

The system SHALL support 2M+ word novels (≈1000 chapters) through:

1. **Embedding + Rerank RAG**: text-embedding-v4 + qwen3-rerank via DashScope
2. **Three-layer context**: L1 (immediate) + L2 (recent) + L3 (retrieved)
3. **Chapter summaries**: Auto-generated 200-word structured summaries indexed for retrieval
4. **Character timeline**: Long-term character state tracking with key events
5. **World lore knowledge base**: Structured world-building facts with reference tracking

#### Scenario: 写到第 500 章时检索第 50 章的伏笔
- **WHEN** writing chapter 500 with foreshadow recovery
- **THEN** system retrieves chapter 50 summary via embedding search + rerank
- **AND** injects relevant context into L3 layer of system prompt

### Requirement: 与 Reasonix 上下文管理协调

The system SHALL ensure Novel RAG context layers coexist with Reasonix dynamic compaction without conflict:

1. **分层职责**：
   - Reasonix `compact.go` 管理**对话历史消息**的压缩（session.Messages）
   - Novel `SmartContextLoader` 管理**小说内容检索**（L1/L2/L3 上下文注入 system prompt）
   - 两者操作不同层面，互不干扰

2. **上下文容量自适应**：
   - L1（即时）：~1K tokens（当前 arc + 上一章摘要）
   - L2（近期）：~2K tokens（最近 N 章 + 角色状态）
   - L3（检索）：~3K tokens（Top-K 相关章节摘要）
   - **总计**：~6K tokens
   - 对于 ≤8K 窗口模型：自动缩减 L3 Top-K=1, L2 N=2
   - 对于 ≥32K 窗口模型：正常使用 L3 Top-K=3, L2 N=5

3. **Prefix Cache 保护**：
   - 角色切换时**只替换 system prompt 后缀**，保留前缀
   - L1/L2/L3 上下文作为 system prompt 的一部分，不参与角色切换
   - 确保 prefix cache 命中率 > 70%

4. **摘要生成时机**：
   - 章节写作完成后**同步生成摘要**（写入 chapter_summaries 表）
   - **异步生成 embedding** 并写入 vec_chapters 表
   - 确保下一章写作时摘要已可用

5. **压缩触发协调**：
   - Novel 上下文注入后，总 tokens = Reasonix 历史 + Novel 上下文
   - 如果总 tokens > soft limit，Reasonix 自动压缩历史消息
   - Novel L3 检索结果**不被压缩**（属于 system prompt）

#### Scenario: 8K 窗口模型写第 100 章
- **WHEN** 使用 8K 窗口模型（如 deepseek-chat）
- **THEN** SmartContextLoader 自动调整：L3 Top-K=1, L2 N=2
- **AND** 总上下文控制在 4K 以内，避免触发压缩
- **AND** 角色切换时 prefix cache 仍然命中

#### Scenario: 128K 窗口模型写第 500 章
- **WHEN** 使用 128K 窗口模型（如 deepseek-v3）
- **THEN** SmartContextLoader 使用完整配置：L3 Top-K=3, L2 N=5
- **AND** 总上下文 6K，仅占窗口 5%，不会触发压缩
- **AND** L3 检索成功注入第 50 章的伏笔摘要

### Requirement: 配置化改造

The system SHALL support runtime configuration via `.novel-weaver/config.json`:

- Writing parameters (paragraph limit, word count targets, context window sizes)
- Anti-AI rules source (embedded / file / database)
- Review dimensions and score thresholds
- Consistency check dimensions
- Genre-specific overrides

#### Scenario: 用户自定义禁用词
- **WHEN** user creates `.novel-weaver/config.json` with custom forbidden words
- **THEN** system loads custom rules instead of embedded defaults
- **AND** applies them during chapter_write post-processing

### Requirement: 按 Genre 定制规则

The system SHALL support genre-specific:
- Anti-AI rules (`xianxia_anti_ai.json`, `urban_anti_ai.json`, etc.)
- Style guidelines (combat scenes, dialogue patterns, pacing rules)
- Forbidden patterns (genre-specific clichés)
- Recommended patterns (genre-specific tropes)

#### Scenario: 仙侠小说禁用"霸道总裁"
- **WHEN** project genre is "xianxia"
- **THEN** anti-AI engine loads xianxia-specific rules
- **AND** does NOT apply urban-genre rules like "霸道总裁"

## MODIFIED Requirements

### Requirement: 现有 Anti-AI 规则引擎

**Before**: Rules compiled into binary via `//go:embed`, 60+ static patterns
**After**: Rules loaded from configurable source with genre-specific overrides

- SHALL support `embedded` (default), `file`, `database` rule sources
- SHALL merge genre-specific rules with base rules
- SHALL support runtime rule hot-reload

### Requirement: 现有风格锚点

**Before**: Fixed 5 dimensions (sentence/paragraph length, dialogue ratio, bigrams, punctuation)
**After**: Extensible dimension system with manual override support

- SHALL support adding new dimensions via config
- SHALL support `manual-anchor.md` YAML override per dimension
- SHALL auto-extract from last N chapters (configurable)

### Requirement: 现有审查维度

**Before**: Fixed 8 dimensions (plot/character/style/consistency/pacing/foreshadow/hook/values)
**After**: Configurable dimensions with quantifiable scoring criteria

- SHALL load dimension definitions from config
- SHALL support per-dimension score thresholds
- SHALL provide actionable fix suggestions (not just scores)

## ADDED Requirements (补充)

### Requirement: 容错与降级

The system SHALL handle API failures gracefully:

- **Embedding API 失败** → 回退到 FTS5 全文检索
- **Rerank API 失败** → 使用 Embedding 相似度直接排序
- **LLM 调用失败** → 重试 3 次，每次指数退避（1s → 2s → 4s）
- **DashScope QPS 限流** → 自动降速，避免触发限流

#### Scenario: Embedding API 超时
- **WHEN** DashScope text-embedding-v4 返回 429/503
- **THEN** 系统自动切换到 FTS5 关键词检索
- **AND** 记录日志，下次优先尝试 Embedding

### Requirement: 内容安全（可选）

The system SHALL provide optional content safety checks:

- 默认关闭，用户可在 `config.json` 中启用
- 启用后，生成前审核 prompt，生成后审核内容
- 支持自定义敏感词列表

#### Scenario: 用户启用内容安全
- **WHEN** `config.json` 中 `content_safety.enabled = true`
- **THEN** 系统过滤敏感内容
- **AND** 将违规内容替换为 `[内容已过滤]`

### Requirement: 性能监控

The system SHALL expose performance metrics:

- 检索耗时（Embedding / Rerank / 总耗时）
- Embedding 生成耗时
- 内存使用（缓存大小、向量存储大小）
- 缓存命中率
- CLI 性能报告（`novel doctor --performance`）

#### Scenario: 性能瓶颈定位
- **WHEN** 用户运行 `novel doctor --performance`
- **THEN** 输出各组件耗时和内存使用
- **AND** 提示优化建议（如"缓存命中率低，建议增大缓存"）

### Requirement: 数据备份

The system SHALL support automatic and manual backup:

- 每 10 章自动备份 SQLite 到 `.novel-weaver/backups/`
- 支持手动导出 ZIP（含 Markdown + DB + config）
- 支持从 ZIP 恢复
- 保留最近 10 个自动备份

#### Scenario: 自动备份
- **WHEN** 写完第 10/20/30... 章
- **THEN** 自动创建 `backup-20240101-001000.zip`
- **AND** 删除超过 10 个的旧备份

### Requirement: 用户反馈循环

The system SHALL learn from user edits:

- 追踪用户对生成内容的修改（diff 分析）
- 识别用户偏好的修改模式（如总是把"缓缓说道"改为"沉声道"）
- 自动调整规则权重（用户频繁修改的模式提升优先级）
- 可选：匿名上报修改统计（用于改进默认规则）

#### Scenario: 用户反馈学习
- **WHEN** 用户连续 3 次将"缓缓说道"改为不同表达
- **THEN** 系统记录该偏好
- **AND** 在后续生成中优先使用用户偏好的表达

### Requirement: 导出格式

The system SHALL support multiple export formats:

- Markdown（默认，保留完整元数据）
- EPUB（电子书格式）
- TXT（纯文本，无元数据）
- 分卷导出（按 volume 分割）

#### Scenario: 导出 EPUB
- **WHEN** 用户运行 `novel export --format epub --output 我的小说.epub`
- **THEN** 生成 EPUB 文件，含目录、章节标题、作者信息

## REMOVED Requirements

### Requirement: 静态工具 Schema

**Reason**: Hardcoded JSON schemas in `novelbridge/bridge.go` require recompilation for changes
**Migration**: Schemas auto-generated from tool registry metadata

### Requirement: 固定字数限制

**Reason**: 2000-4000 word target is too rigid for different genres
**Migration**: Configurable via `.novel-weaver/config.json` with genre defaults
