# Checklist

> 验收检查清单。每个 Phase 结束后勾选对应项。

## Phase 4: 配置化与 Anti-AI 改造

- [ ] `config.json` 加载成功（含默认值回退）
- [ ] 热重载生效（文件变更后 5 秒内刷新）
- [ ] Anti-AI 规则接口化（`RuleEngine` interface）
- [ ] 多源加载生效（embedded → file → database）
- [ ] Genre 规则合并正确（base + genre-specific）
- [ ] 正则表达式匹配（替代 `strings.Contains`）
- [ ] 上下文感知（避免误伤合理用法）
- [ ] 风格锚点维度接口化（`AnchorDimension` interface）
- [ ] 新增维度生效（修辞 / 情感 / 视角）
- [ ] 手动 override 支持 per-dimension

## Phase 5: RAG 与 200万字支持

- [ ] Schema v3 迁移成功（4 张新表）
- [ ] `chapter_summaries` 表工作正常
- [ ] `vec_chapters` 虚拟表支持向量检索
- [ ] `character_timeline` 表记录关键事件
- [ ] `world_lore` 表记录世界观知识
- [ ] Embedding 客户端连接 DashScope 成功
- [ ] 批量编码接口工作正常
- [ ] Embedding 缓存命中 > 80%
- [ ] Rerank 客户端连接 DashScope 成功
- [ ] 批量打分接口工作正常
- [ ] 向量检索层 `Search` 返回 Top-K 结果
- [ ] 相似度排序正确
- [ ] L1 即时上下文加载（arc + 上一章）
- [ ] L2 近期上下文加载（最近 N 章 + 角色状态）
- [ ] L3 检索上下文加载（Embedding + Rerank）
- [ ] 上下文组装注入 system prompt
- [ ] 上下文容量自适应（8K 窗口自动缩减 L2/L3）
- [ ] 与 Reasonix compact 协调（Novel 上下文不被压缩）
- [ ] Prefix Cache 保护（角色切换保留 L1/L2/L3）
- [ ] 章节摘要生成（200 字结构化）
- [ ] 关键信息提取（events / characters / locations / items）
- [ ] 异步索引触发（写作完成后自动索引）
- [ ] 角色时间线生成（突破 / 受伤 / 关系变化）
- [ ] 状态压缩（只保留重要事件）
- [ ] 世界知识提取（修炼体系 / 地理 / 历史 / 组织）
- [ ] 引用追踪（哪章引用了哪条知识）

## Phase 6: Genre Pack 增强

- [ ] 仙侠 Anti-AI 规则加载（`xianxia/anti_ai.json`）
- [ ] 都市 Anti-AI 规则加载（`urban/anti_ai.json`）
- [ ] 恐怖 Anti-AI 规则加载（`horror/anti_ai.json`）
- [ ] Genre 规则合并逻辑正确
- [ ] 战斗场景模板注入
- [ ] 修炼场景模板注入
- [ ] 对话场景模板注入
- [ ] 情感场景模板注入

## Phase 7: 审查系统增强

- [ ] 审查维度配置化（`dimensions.json`）
- [ ] 维度接口化（`ReviewDimension` interface）
- [ ] 量化评分标准（每维度 0-10 分）
- [ ] 可执行修改建议生成
- [ ] 建议格式标准化
- [ ] 与 chapter_write 联动（注入下一章 prompt）

## Phase 8: 性能与体验优化

- [ ] 异步索引服务启动
- [ ] 章节写作后自动触发索引
- [ ] 索引队列避免并发冲突
- [ ] Embedding 缓存生效
- [ ] 检索结果缓存生效
- [ ] 摘要缓存常驻内存
- [ ] CLI 进度条显示（Indexing → Generating → Scrubbing → Saving）
- [ ] 错误友好提示（常见错误 + 修复建议）

## Phase 9: 容错、监控与导出

- [ ] Embedding 失败 → FTS5 回退生效
- [ ] Rerank 失败 → Embedding 直接排序生效
- [ ] LLM 失败 → 指数退避重试生效
- [ ] DashScope QPS 限流 → 自动降速生效
- [ ] 内容安全默认关闭
- [ ] 内容安全启用后过滤生效
- [ ] 性能指标收集（检索耗时 / Embedding 耗时 / 内存 / 缓存命中率）
- [ ] `novel doctor --performance` 输出报告
- [ ] 每 10 章自动备份触发
- [ ] 手动导出 ZIP 成功
- [ ] 从 ZIP 恢复成功
- [ ] 用户修改追踪（diff 分析）
- [ ] 用户偏好模式识别
- [ ] 规则权重自动调整
- [ ] Markdown 导出成功
- [ ] EPUB 导出成功
- [ ] TXT 导出成功
- [ ] 分卷导出成功

## Phase 10: 集成验证

- [ ] `novel setup` → 创建项目（含 config.json 模板）
- [ ] `novel world --create` → 创建世界观
- [ ] `novel character --create` → 创建 3 个角色
- [ ] `novel chapter` → 写第 1 章（验证 RAG / 配置化 / Genre 规则）
- [ ] `novel chapter --continue` → 写第 2、3 章
- [ ] `novel review` → 审查第 1 章（验证配置化维度）
- [ ] `novel consistency` → 5 维检查
- [ ] `novel stats` → 统计
- [ ] `novel export --format epub` → 导出验证
- [ ] `novel doctor --performance` → 性能报告
- [ ] 1000 章 mock 数据生成
- [ ] 检索性能 < 500ms
- [ ] 内存使用 < 2GB
- [ ] 角色一致性（500 章后无矛盾）
- [ ] 世界观一致性（500 章后无矛盾）
- [ ] 时间线一致性（500 章后无矛盾）
- [ ] 容错验证（模拟 API 失败）
- [ ] 备份验证（自动备份触发）

## 质量门禁

每个 Phase 结束必须满足：
- [ ] `go build ./...` 0 错误
- [ ] `go test ./...` 全部通过
- [ ] 无新增 lint warning
- [ ] 对应 Phase 的 checklist 项全部勾选
