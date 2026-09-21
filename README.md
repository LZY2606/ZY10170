# 声纹校形台（Voiceprint Formant Correction Bench）

用于语音实验的本地共振峰轨迹校正工具：导入固定帧率的频谱候选、自动轨迹、
说话人与发音版本；研究者在声谱图上修正 F1–F4 轨迹时，**原算法的每个候选
始终可见且永不被改写**。修订以稀疏操作保存；重新跑追踪后，旧操作会被映射
到新候选集合——能唯一映射的自动恢复，候选不唯一的区段进入**冲突列表**，
系统不会按距离强行选一个。

## 技术栈

- Go（标准库 `net/http`，Go 1.22+ 路由）
- SQLite（纯 Go 驱动 `modernc.org/sqlite`，无 CGO）
- 前端为单页 SVG（`web/index.html` + `web/app.js`，通过 `go:embed` 打包）
- 全部数据为仓库内置的**固定确定性 fixture**，无随机源、无外部文件

## 安装与演示

```bash
go mod download
go test ./... -count=1 && go run ./cmd/server --listen 127.0.0.1:5510
```

浏览器访问 <http://127.0.0.1:5510>，页面标题为「声纹校形台」。
首次启动会自动把固定 fixture 写入 `voicebench.db`（可用 `--db` 指定路径）。

## 数据口径（重要）

- **固定帧率**：100 fps；帧序号从 0 开始。
- **分析窗半开 `[from, to)`**：帧号恰好等于右端 `to` 的帧**不属于**当前窗，
  不产生候选/轨迹/单元格（fixture 中 `s1/ai-b` 的帧 18、以及 `s1/ai-a` 的
  帧 20 演示该规则）。
- **缺帧不插值**：缺帧（present=false）单元格保持空缺，频率为 0 仅表示
  “无值”，折线在缺帧处断开，**绝不做零频率插值或跨缺帧连线**。
- **同帧严格递增**：同一可见帧内最终值必须满足 `F1 < F2 < F3 < F4`。
  违反会在页面与 `/api/state` 的 `issues` 中标为 `not_strictly_increasing`。
- **禁止跨不可见帧交换身份**：对每个缺帧区间，用左右两侧可见帧做
  最小总跳变指派（4×4 置换穷举）；非恒等置换即标
  `identity_swap_across_invisible`。锁定边界帧（lock 操作）为重映射提供
  身份锚点，但不放宽该规则。
- **候选**：每个 (帧, 带) 可有多个候选峰（弱能量区竞争峰、假峰），按频率
  升序排列；自动轨迹为每个槽选择一个候选，原始选择与候选行永不被修改。
- **可信度**：自动点的置信度来自 fixture（弱能量竞争区 ~0.41–0.55，
  正常 0.9），页面以橙色虚线晕圈标出低可信点。

## 固定 fixture 内容

| 说话人 | 发音版本 | 窗 | 设计的现象 |
| --- | --- | --- | --- |
| s1 Lin | `ai-a` / `pron-v1` | `[0,20)` | 帧 9–11 弱能量区 F1/F2 竞争峰交叉跳轨；帧 13–14 缺帧；帧 15 之后 F2/F3 标签跨缺帧交换；帧 20 在右端外 |
| s2 Aoi | `ai-a` / `pron-v2` | `[0,20)` | 另一说话人的弱能量竞争峰（无缺帧） |
| s1 Lin | `ai-b` / `pron-v3-fast` | `[2,18)` | 帧 0–1 在窗外、帧 18 恰好落在右端（半开窗） |

候选集合：

- `v1`：原始频谱候选（含弱能量竞争峰、假峰）。
- `v2`：一次确定性候选重算（`internal/fixture` 中的纯函数扰动）。绝大多数
  峰在 ±40Hz 容差内唯一移动；**s1/ai-a 帧 10 的 F2 有两个候选（908/910）
  落在旧峰 900Hz 的 ±40Hz 窗口内**，用于验收“歧义保留两项”；s1/ai-a 帧 9
  的竞争峰在 v2 消失，用于验收“无法映射进 unresolved”。

## 操作（稀疏修订）

所有修订都是只追加的 `operations` 行，按时间顺序折叠到自动轨迹上（后写
覆盖），三类：

- `move`：拖动一个点到同帧同带的另一个候选（恰好 1 个 item）。
- `path`：同一带上一段连续可见帧重选候选路径（帧号必须连续、同带、
  均在窗内且 present）。
- `lock`：锁定边界帧，把该点固定为重映射时的身份锚。

操作校验：候选必须存在于当前激活集合且归属一致的帧/带；落在窗外或缺帧
的操作拒绝（HTTP 422）。校正已损坏的帧允许分步提交，页面持续显示未消除
的违规。

## 重跑追踪与冲突（不按距离强行选）

`POST /api/rerun` 切到 `v2` 并把 `v1` 操作逐个 item 重映射：

- 同帧、同带、`|新频率 - 旧频率| <= 40Hz` 收集候选；
- 恰好 1 个 → `unique`，自动生成对应的 `v2` 操作恢复；
- ≥2 个 → `conflict`，**两个选项全部保留**（`options`），该操作不会被
  半自动恢复，必须在页面冲突列表中由研究者显式选择
  （`POST /api/conflicts/resolve`）；
- 0 个（竞争峰消失等）→ `unresolved`，列入“无法映射”。

参考行持久化在 `remap_refs` 表，导出包含原始点、操作序列与最终值，可
独立重放。

## HTTP 接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/` | SVG 操作页面（标题“声纹校形台”） |
| GET | `/api/state?speaker=s1&utter=ai-a` | 同步视图：帧、全部候选、自动点、元音、声谱、操作、最终单元格、issues、冲突、日志 |
| POST | `/api/operations` | 追加 move/path/lock（422 = 校验失败） |
| POST | `/api/undo` | 撤销该发音最近一条操作 |
| POST | `/api/rerun` | 候选重算并重映射旧操作 |
| POST | `/api/conflicts/resolve` | 研究者显式选择歧义候选 |
| GET | `/api/export` | 下载自包含 JSON 导出包 |
| POST | `/api/import` | 清空后导入导出包并复核 |
| POST | `/api/reset` | 清空并重新导入固定 fixture |
| GET | `/api/runlog?limit=n` | 运行记录（导出包内也含 `run_log`） |

## 独立重放与“清空后复核”

导出包（schema `voicebench/v1`）包含：说话人、发音版本、帧元数据、
**两套候选全集（保留原始主键）**、原始自动轨迹、元音区间、声谱、稀疏
操作序列、各发音激活集合标记与运行记录。候选与自动点使用导出时的主键
重新入库，因此“导出的原始点从未被改写”可以直接逐行核对（测试
`TestExportImportRoundTrip` 即如此验收）。

清空数据库复核：

```bash
# 页面上点击「清空并重导 fixture」，或：
curl -XPOST http://127.0.0.1:5510/api/reset
# 也可以把之前的导出包重新导入：
curl -XPOST http://127.0.0.1:5510/api/import \
  -H 'Content-Type: application/json' --data-binary @voicebench-export-*.json
```

重跑同一修订：对任意导入包执行一次 `/api/rerun`，唯一映射自动恢复、
歧义映射在冲突列表中保留两项——验收脚本见
`internal/service/remap_test.go`（`TestRerunUniqueAndConflict`、
`TestRerunUnresolved`）与 `internal/track/track_test.go`。

## 目录结构

```
cmd/server/            服务入口
internal/model/        领域类型与导出包结构
internal/fixture/      固定确定性 fixture（v1/v2 候选、缺帧、弱能量交叉）
internal/track/        操作应用、严格递增/跨缺帧身份检测、重映射
internal/store/        SQLite 模式、持久化、导入导出
internal/service/      用例编排（校验、重跑、冲突消解）
internal/server/       HTTP 路由 + go:embed 的 SVG 页面
web/                   页面源文件（构建时复制进 internal/server/web_assets）
```
