# Tasks

> 基于已完成 Phase 0-3 的增量改造计划。Phase 4-8 为新增/改造内容。

## Phase 4: 配置化与 Anti-AI 改造

* [ ] Task 4.1: 配置系统 (`config.json`)

  * [ ] SubTask 4.1.1: `internal/novel/config/config.go` — 配置结构定义（Writing/Review/AntiAI/Genre/Context）

  * [ ] SubTask 4.1.2: 加载逻辑：`.novel-weaver/config.json` → 默认值回退

  * [ ] SubTask 4.1.3: 热重载支持（文件变更自动刷新）

  * [ ] SubTask 4.1.4: 测试：配置加载/回退/热重载

* [ ] Task 4.2: Anti-AI 规则引擎改造

  * [ ] SubTask 4.2.1: 规则接口化：`RuleEngine` interface（`Load`/`Check`/`Fix`）

  * [ ] SubTask 4.2.2: 多源加载：`embedded` → `file` → `database` 优先级

  * [ ] SubTask 4.2.3: Genre 规则合并：base rules + genre-specific rules

  * [ ] SubTask 4.2.4: 正则表达式匹配（替代简单 `strings.Contains`）

  * [ ] SubTask 4.2.5: 上下文感知（避免误伤合理用法）

  * [ ] SubTask 4.2.6: 测试：多源加载 / genre 合并 / 正则匹配 / 上下文感知

* [ ] Task 4.3: 风格锚点增强

  * [ ] SubTask 4.3.1: 维度接口化：`AnchorDimension` interface

  * [ ] SubTask 4.3.2: 新增维度：修辞手法（比喻/排比/夸张）、情感分布、叙事视角

  * [ ] SubTask 4.3.3: 配置化提取章节数（默认 3-5，可配置）

  * [ ] SubTask 4.3.4: 手动 override 增强（支持 per-dimension override）

  * [ ] SubTask 4.3.5: 测试：新维度提取 / 配置化 / override

## Phase 5: RAG 与 200万字支持

* [ ] Task 5.1: Schema 升级（新增表）

  * [ ] SubTask 5.1.1: `chapter_summaries` 表（摘要 + 关键信息 + embedding）

  * [ ] SubTask 5.1.2: `vec_chapters` 虚拟表（SQLite vec 扩展）

  * [ ] SubTask 5.1.3: `character_timeline` 表（角色关键事件）

  * [ ] SubTask 5.1.4: `world_lore` 表（世界观知识库）

  * [ ] SubTask 5.1.5: schema\_version 迁移到 v3

  * [ ] SubTask 5.1.6: 测试：新表创建 / 索引 / FK

* [ ] Task 5.2: Embedding 客户端

  * [ ] SubTask 5.2.1: `internal/novel/rag/embed.go` — DashScope text-embedding-v4 客户端

  * [ ] SubTask 5.2.2: 批量编码接口（支持 \[]string → \[]\[]float32）

  * [ ] SubTask 5.2.3: 缓存层（避免重复编码）

  * [ ] SubTask 5.2.4: 测试：mock API / 缓存命中

* [ ] Task 5.3: Rerank 客户端

  * [ ] SubTask 5.3.1: `internal/novel/rag/rerank.go` — DashScope qwen3-rerank 客户端

  * [ ] SubTask 5.3.2: 批量打分接口（query + \[]documents → \[]scores）

  * [ ] SubTask 5.3.3: 测试：mock API / 分数排序

* [ ] Task 5.4: 向量检索层

  * [ ] SubTask 5.4.1: `internal/novel/rag/vector.go` — SQLite vec 封装

  * [ ] SubTask 5.4.2: `Insert(chapterID, embedding)` 接口

  * [ ] SubTask 5.4.3: `Search(queryVec, topK)` 接口（余弦相似度）

  * [ ] SubTask 5.4.4: 测试：插入 / 检索 / 相似度排序

* [ ] Task 5.5: 智能上下文加载器

  * [ ] SubTask 5.5.1: `internal/novel/context/loader.go` — `SmartContextLoader`

  * [ ] SubTask 5.5.2: L1 即时上下文（当前 arc + 上一章摘要）

  * [ ] SubTask 5.5.3: L2 近期上下文（最近 N 章 + 活跃角色状态）

  * [ ] SubTask 5.5.4: L3 检索上下文（Embedding 粗排 + Rerank 精排）

  * [ ] SubTask 5.5.5: 上下文组装（L1 + L2 + L3 → system prompt）

  * [ ] SubTask 5.5.6: **上下文容量自适应**（根据模型窗口大小调整 L2/L3 容量）

  * [ ] SubTask 5.5.7: **与 Reasonix compact 协调**（确保 Novel 上下文不被压缩）

  * [ ] SubTask 5.5.8: **Prefix Cache 保护**（角色切换时保留 L1/L2/L3）

  * [ ] SubTask 5.5.9: 测试：三层加载 / 检索质量 / 窗口自适应 / 角色切换

* [ ] Task 5.6: 章节摘要生成

  * [ ] SubTask 5.6.1: `internal/novel/rag/summary.go` — LLM 生成 200 字结构化摘要

  * [ ] SubTask 5.6.2: 关键信息提取（events / characters / locations / items）

  * [ ] SubTask 5.6.3: 异步索引（写作完成后后台生成）

  * [ ] SubTask 5.6.4: 测试：摘要质量 / 信息完整性

* [ ] Task 5.7: 角色长期追踪

  * [ ] SubTask 5.7.1: `internal/novel/context/timeline.go` — 角色时间线管理

  * [ ] SubTask 5.7.2: 关键事件提取（突破 / 受伤 / 关系变化）

  * [ ] SubTask 5.7.3: 状态压缩（只保留重要事件）

  * [ ] SubTask 5.7.4: 测试：时间线生成 / 压缩

* [ ] Task 5.8: 世界知识库

  * [ ] SubTask 5.8.1: `internal/novel/context/lore.go` — 世界观知识管理

  * [ ] SubTask 5.8.2: 知识提取（修炼体系 / 地理 / 历史 / 组织）

  * [ ] SubTask 5.8.3: 引用追踪（哪章引用了哪条知识）

  * [ ] SubTask 5.8.4: 测试：知识提取 / 引用追踪

## Phase 6: Genre Pack 增强

* [ ] Task 6.1: Genre 特定 Anti-AI 规则

  * [ ] SubTask 6.1.1: `internal/novel/genre/xianxia/anti_ai.json` — 仙侠专用规则

  * [ ] SubTask 6.1.2: `internal/novel/genre/urban/anti_ai.json` — 都市专用规则

  * [ ] SubTask 6.1.3: `internal/novel/genre/horror/anti_ai.json` — 恐怖专用规则

  * [ ] SubTask 6.1.4: 加载逻辑：genre pack + anti\_ai 规则合并

  * [ ] SubTask 6.1.5: 测试：各 genre 规则加载 / 合并

* [ ] Task 6.2: Genre 场景模板

  * [ ] SubTask 6.2.1: 战斗场景模板（分镜 / 节奏 / 视觉描写）

  * [ ] SubTask 6.2.2: 修炼场景模板（境界突破 / 资源获取）

  * [ ] SubTask 6.2.3: 对话场景模板（身份差异 / 语气特征）

  * [ ] SubTask 6.2.4: 情感场景模板（克制 / 张力 / 留白）

  * [ ] SubTask 6.2.5: 注入 system prompt

  * [ ] SubTask 6.2.6: 测试：模板加载 / 注入

## Phase 7: 审查系统增强

* [ ] Task 7.1: 审查维度配置化

  * [ ] SubTask 7.1.1: `internal/novel/review/dimension.go` — `ReviewDimension` interface

  * [ ] SubTask 7.1.2: 维度定义文件（`dimensions.json`）

  * [ ] SubTask 7.1.3: 量化评分标准（每维度 0-10 分标准）

  * [ ] SubTask 7.1.4: 测试：维度加载 / 评分

* [ ] Task 7.2: 可执行修改建议

  * [ ] SubTask 7.2.1: `internal/novel/review/suggestion.go` — 修改建议生成

  * [ ] SubTask 7.2.2: 建议格式：`{type: "add_conflict", location: "paragraph_3", reason: "..."}`

  * [ ] SubTask 7.2.3: 与 chapter\_write 联动（自动注入下一章 prompt）

  * [ ] SubTask 7.2.4: 测试：建议生成 / 联动

## Phase 8: 性能与体验优化

* [ ] Task 8.1: 异步索引

  * [ ] SubTask 8.1.1: `internal/novel/async/indexer.go` — 后台索引服务

  * [ ] SubTask 8.1.2: 章节写作完成后触发异步索引

  * [ ] SubTask 8.1.3: 索引队列（避免并发冲突）

  * [ ] SubTask 8.1.4: 测试：异步触发 / 队列

* [ ] Task 8.2: 缓存层

  * [ ] SubTask 8.2.1: Embedding 缓存（chapter\_id → embedding）

  * [ ] SubTask 8.2.2: 检索结果缓存（query\_hash → results）

  * [ ] SubTask 8.2.3: 摘要缓存（常驻内存）

  * [ ] SubTask 8.2.4: 测试：缓存命中 / 失效

* [ ] Task 8.3: 进度反馈

  * [ ] SubTask 8.3.1: `internal/novel/progress/feedback.go` — 写作进度回调

  * [ ] SubTask 8.3.2: CLI 进度条（Indexing → Generating → Scrubbing → Saving）

  * [ ] SubTask 8.3.3: 错误友好提示（常见错误 + 修复建议）

  * [ ] SubTask 8.3.4: 测试：进度回调 / 错误提示

## Phase 9: 容错、监控与导出

* [ ] Task 9.1: 容错与降级

  * [ ] SubTask 9.1.1: `internal/novel/fallback/fallback.go` — API 失败降级策略

  * [ ] SubTask 9.1.2: Embedding 失败 → FTS5 回退

  * [ ] SubTask 9.1.3: Rerank 失败 → Embedding 直接排序

  * [ ] SubTask 9.1.4: LLM 失败 → 指数退避重试（1s → 2s → 4s）

  * [ ] SubTask 9.1.5: DashScope QPS 限流 → 自动降速

  * [ ] SubTask 9.1.6: 测试：模拟各种失败场景

* [ ] Task 9.2: 内容安全（可选）

  * [ ] SubTask 9.2.1: `internal/novel/safety/safety.go` — 内容安全检查器

  * [ ] SubTask 9.2.2: 默认关闭，config 中启用

  * [ ] SubTask 9.2.3: 生成前 prompt 审核

  * [ ] SubTask 9.2.4: 生成后内容审核

  * [ ] SubTask 9.2.5: 自定义敏感词列表

  * [ ] SubTask 9.2.6: 测试：敏感内容过滤

* [ ] Task 9.3: 性能监控

  * [ ] SubTask 9.3.1: `internal/novel/metrics/metrics.go` — 性能指标收集

  * [ ] SubTask 9.3.2: 检索耗时统计（Embedding / Rerank / 总耗时）

  * [ ] SubTask 9.3.3: Embedding 生成耗时统计

  * [ ] SubTask 9.3.4: 内存使用监控（缓存 / 向量存储）

  * [ ] SubTask 9.3.5: 缓存命中率统计

  * [ ] SubTask 9.3.6: `novel doctor --performance` 输出报告

  * [ ] SubTask 9.3.7: 测试：指标收集 / 报告生成

* [ ] Task 9.4: 数据备份

  * [ ] SubTask 9.4.1: `internal/novel/backup/backup.go` — 备份管理器

  * [ ] SubTask 9.4.2: 每 10 章自动备份 SQLite

  * [ ] SubTask 9.4.3: 手动导出 ZIP（Markdown + DB + config）

  * [ ] SubTask 9.4.4: 从 ZIP 恢复

  * [ ] SubTask 9.4.5: 保留最近 10 个自动备份

  * [ ] SubTask 9.4.6: 测试：自动备份 / 导出 / 恢复

* [ ] Task 9.5: 用户反馈循环

  * [ ] SubTask 9.5.1: `internal/novel/feedback/feedback.go` — 反馈收集器

  * [ ] SubTask 9.5.2: 追踪用户修改（diff 分析）

  * [ ] SubTask 9.5.3: 识别用户偏好模式

  * [ ] SubTask 9.5.4: 自动调整规则权重

  * [ ] SubTask 9.5.5: 可选匿名上报

  * [ ] SubTask 9.5.6: 测试：反馈学习 / 权重调整

* [ ] Task 9.6: 导出格式

  * [ ] SubTask 9.6.1: `internal/novel/export/export.go` — 导出管理器

  * [ ] SubTask 9.6.2: Markdown 导出（默认）

  * [ ] SubTask 9.6.3: EPUB 导出

  * [ ] SubTask 9.6.4: TXT 导出

  * [ ] SubTask 9.6.5: 分卷导出

  * [ ] SubTask 9.6.6: 测试：各格式导出

## Phase 10: 集成验证

* [ ] Task 10.1: 端到端测试

  * [ ] SubTask 10.1.1: `novel setup` → 创建项目

  * [ ] SubTask 10.1.2: `novel world --create` → 创建世界观

  * [ ] SubTask 10.1.3: `novel character --create` → 创建 3 个角色

  * [ ] SubTask 10.1.4: `novel chapter` → 写第 1 章（验证 RAG / 配置化 / Genre 规则）

  * [ ] SubTask 10.1.5: `novel chapter --continue` → 写第 2、3 章

  * [ ] SubTask 10.1.6: `novel review` → 审查第 1 章（验证配置化维度）

  * [ ] SubTask 10.1.7: `novel consistency` → 5 维检查

  * [ ] SubTask 10.1.8: `novel stats` → 统计

  * [ ] SubTask 10.1.9: `novel export --format epub` → 导出验证

  * [ ] SubTask 10.1.10: `novel doctor --performance` → 性能报告

* [ ] Task 10.2: 200万字压力测试

  * [ ] SubTask 10.2.1: 生成 1000 章 mock 数据

  * [ ] SubTask 10.2.2: 验证检索性能（< 500ms）

  * [ ] SubTask 10.2.3: 验证内存使用（< 2GB）

  * [ ] SubTask 10.2.4: 验证一致性（角色 / 世界观 / 时间线）

  * [ ] SubTask 10.2.5: 验证容错（模拟 API 失败）

  * [ ] SubTask 10.2.6: 验证备份（自动备份触发）

# Task Dependencies

* Task 5.2 (Embedding) depends on Task 4.1 (Config)

* Task 5.3 (Rerank) depends on Task 5.2 (Embedding)

* Task 5.4 (Vector) depends on Task 5.1 (Schema) + Task 5.2 (Embedding)

* Task 5.5 (Context Loader) depends on Task 5.3 (Rerank) + Task 5.4 (Vector)

* Task 5.6 (Summary) depends on Task 5.2 (Embedding)

* Task 5.7 (Timeline) depends on Task 5.1 (Schema)

* Task 5.8 (Lore) depends on Task 5.1 (Schema)

* Task 6.1 (Genre Anti-AI) depends on Task 4.2 (Anti-AI Engine)

* Task 6.2 (Genre Templates) depends on Task 4.1 (Config)

* Task 7.1 (Review Dimensions) depends on Task 4.1 (Config)

* Task 7.2 (Suggestions) depends on Task 7.1 (Review Dimensions)

* Task 8.1 (Async Indexer) depends on Task 5.6 (Summary)

* Task 8.2 (Cache) depends on Task 5.4 (Vector)

* Task 8.3 (Feedback) depends on Task 5.5 (Context Loader)

* Task 9.1 (Fallback) depends on Task 5.2 (Embedding) + Task 5.3 (Rerank)

* Task 9.2 (Safety) depends on Task 4.1 (Config)

* Task 9.3 (Metrics) depends on Task 5.4 (Vector) + Task 5.5 (Context Loader)

* Task 9.4 (Backup) depends on Task 4.1 (Config)

* Task 9.5 (Feedback Loop) depends on Task 4.2 (Anti-AI Engine)

* Task 9.6 (Export) depends on Task 4.1 (Config)

* Task 10.1 (E2E) depends on all above

* Task 10.2 (Stress Test) depends on Task 10.1 (E2E)

# Parallelizable Work

* Task 4.1 (Config) + Task 4.3 (Anchor) + Task 5.1 (Schema) can run in parallel

* Task 5.2 (Embedding) + Task 5.7 (Timeline) + Task 5.8 (Lore) can run in parallel after Schema

* Task 6.1 (Genre Anti-AI) + Task 6.2 (Genre Templates) can run in parallel after Config

* Task 7.1 (Review Dimensions) + Task 8.2 (Cache) + Task 9.2 (Safety) + Task 9.4 (Backup) + Task 9.6 (Export) can run in parallel after Config

* Task 9.1 (Fallback) + Task 9.3 (Metrics) + Task 9.5 (Feedback Loop) can run in parallel after Embedding/Rerank

