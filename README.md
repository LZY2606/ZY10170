# 声纹校形台 (Voiceprint Formant Bench)

本地服务 + SVG 操作页面, 用于在看得见原算法**每一个频谱候选**的前提下修正
F1–F4 共振峰轨迹。修订以**稀疏操作**保存; 重跑追踪(候选集合重算)后把旧操作
**映射**到新候选: 唯一映射自动恢复, 无法唯一映射的区段进入**冲突列表**,
**绝不按距离强行选一个**。

技术栈: Go (标准库 `net/http`) + SQLite (`modernc.org/sqlite`, 纯 Go, 无需 CGO)
+ 原生 SVG/JavaScript 页面。

## 安装与演示

```bash
go mod download
go test ./... -count=1
go run ./cmd/server --listen 127.0.0.1:5510
```

浏览器访问 <http://127.0.0.1:5510> , 页面标题为 **“声纹校形台”**。

可选参数:

- `--listen` 监听地址, 默认 `127.0.0.1:5510`
- `--db` SQLite 文件, 默认 `voiceprintbench.db`(文件已纳入 `.gitignore`)

## 数据口径(不变量)

| 口径 | 含义 |
| --- | --- |
| 固定帧率 | 主发音 `u01` 为 100Hz, 每帧 10ms, 共 48 帧 (0..47) |
| 半开区间 | 所有区间均为 `[start,end)`; **刚好落在分析窗右端的帧不属于当前窗** |
| 缺帧 | 帧 42..46 在两代候选集合中都没有候选; 轨迹在缺口处**断开**, **不做零频率插值** |
| 严格递增 | 同一帧最终值必须满足 `F1 < F2 < F3 < F4`; 两个 rank 选同一候选也判违规 |
| 身份不交换 | 轨迹按 rank(F1..F4) 连续成段, **不跨缺帧/不可见帧连线或交换身份** |
| 原始点只读 | 自动轨迹 `auto_tracks` 是算法原值, 任何修订/重放/导出都不会改写它 |

跨缺帧身份检测: 对每个缺帧缺口, 用缺口前最后两个可见帧线性外推到缺口后第一帧,
按 rank 逐一比较, 偏差超过 `60Hz` 即报 `cross_gap_swap`(见
`internal/fixture/fixture.go` 中 `GapContinuityTolHz`)。

## 固定 fixture

数据由 `internal/fixture` 确定性生成(无随机种子):

- 说话人 **甲** 的发音 `u01`(/a/): 两代候选集合 `gen1`、`gen2`。
  - 弱能量区为半开 `[18,29)`, 上升峰流 `a` 与下凹峰流 `b` 在帧 23/26 附近交叉;
  - gen1 原算法在帧 **22** 把 F2/F3 候选选反(同帧不再严格递增);
  - gen1 在缺帧缺口(42..46)后的帧 **47** 仍按旧身份接续, 发生 F2/F3 交换;
  - 重算 gen2 后追踪修复, 但帧 22 额外保留杂峰 **1318Hz**, 使旧 F2 目标
    **1288Hz** 在映射容差 `45Hz` 内同时命中 `1288` 与 `1318` 两个候选 => 歧义。
- 说话人 **乙** 的发音 `u02`(/i/): 30 帧单代候选, 无交叉/缺帧, 供多说话人与
  清空重导入复核。

页面“候选代”可切换 gen1/gen2; 灰色小点即该帧**全部**候选, 彩线为 F1–F4
最终轨迹; 元音区间为半开高亮, 缺帧为黑色竖带。

## 三种稀疏操作

1. **拖点** `drag`: 把某帧某 rank 的选择改到该帧另一个候选。
2. **重选路径** `reselect_path`: 先在“重选路径”工具下拖选半开帧区间
   `[start,end)`, 再逐帧点击候选; 提交后只保存受影响帧的
   `frame -> 候选ID` 映射。缺帧无法重选。
3. **锁定边界** `lock_boundary`: 点击元音区间的 `start` 或 `end` 帧锁定锚点候选;
   重放时同样要求**唯一**映射, 否则进入冲突。

操作可逐条撤销; 页面侧栏显示可信度、口径校验、稀疏操作列表与冲突列表。

## 重跑追踪与映射(不按距离强选)

点击 **“重跑追踪并映射”** (或 `POST /api/replay`):

- 对每个旧触点, 在**同一帧**的新候选中找频率差 `<= 45Hz` 的候选:
  - 恰好 1 个 => `unique`, **自动恢复**;
  - 多于 1 个 => `ambiguous`, **全部保留**进冲突列表, 最终轨迹回退到新代自动轨迹;
  - 0 个 => `unmatched`, 同样进入冲突。
- 冲突在侧栏列出, 研究者可从候选下拉中人工指定, 保存为**新的 gen2 稀疏操作**,
  原始 gen1 操作保持不变。

验收脚本对应 `internal/ops/ops_test.go` 与 `internal/httpapi/httpapi_test.go`:
同一组 5 条修订(2 拖点 + 2 段重选 + 1 边界锁定)重放后, **3 个触点 + 1 个锁定
唯一映射自动恢复**, **f22/F2 一项歧义保留两个候选**; 导出的原始点
`gen1 f22 F2 = 1382Hz` 从未被改写。

## 导出与独立重放

`GET /api/export` (页面“导出修订包”) 返回一个 JSON `Bundle`:

- `autoTracks`: **原轨迹**(只读原值);
- `operations`: 完整稀疏**操作序列**(含所基于的 `gen`);
- `finals`: 每代候选上叠加操作后的**最终值**(缺帧为 `{"missing": true}`);
- 另含 `candidates`、`utterances`、`vowels`、`conflicts`、`issues`。

仅凭导出包即可在全新库中重放复核, 见
`internal/export/export_test.go` 的 `TestBundleAllowsIndependentReplay`。

## 运行记录与清空重导入

- `GET /api/runs` 列出导入、重放、冲突解决、导出、重置等运行记录(页面侧栏可见)。
- `POST /api/admin/reimport` (页面“清空重导入”) 清空全部表后重新导入内置
  fixture, 用于复核; 重导入后原算法的 f22/f47 问题会原样恢复。

## HTTP API 摘要

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/state` | 首屏全量状态(候选/自动轨迹/元音/操作/冲突/每代最终值) |
| POST | `/api/operations` | 新增稀疏操作(服务端按口径校验) |
| DELETE | `/api/operations/{id}` | 撤销一条操作 |
| POST | `/api/replay` | 把 gen1 操作重放到 gen2, 落冲突列表 |
| POST | `/api/conflicts/resolve` | 人工指定冲突候选, 生成 gen2 操作 |
| GET | `/api/export` | 导出可独立重放修订包 |
| GET | `/api/runs` | 运行记录 |
| POST | `/api/admin/reimport` | 清空数据库并重新导入 fixture |

## 目录结构

```
cmd/server/         服务入口
internal/model/     领域类型与数据口径注释
internal/fixture/   固定 fixture(交叉/缺帧/候选重算) 与单元测试
internal/store/     SQLite 表结构、导入、操作/冲突/运行记录持久化
internal/ops/       操作校验、触点合并、映射、严格递增与跨缺口身份校验
internal/export/    导出包与独立重放测试
internal/httpapi/   HTTP API 与嵌入的 SVG 页面 (web/)
```
