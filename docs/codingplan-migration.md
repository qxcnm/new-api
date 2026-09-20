# CodingPlan 功能移植方案

状态：已实现（GLM 国内真实上游冒烟验证完成；其余厂商仍以 fixture/mock 验证）

## 1. 目标

在当前项目现有架构和渠道管理页面的基础上，移植 CodingPlan 相关能力：

- 使用各厂商对应的 CodingPlan 模型地址和协议路径转发请求；
- 让请求头、请求体、模型路径和响应处理更接近厂商官方客户端；
- 在渠道管理页查看计划额度、风控状态和重置卡；
- 支持 Kimi、MiniMax 的额度查询；
- 支持 GLM 的风控查询和重置卡查询/使用；
- 补齐后端、前端和 i18n 测试。

页面布局、渠道表格、已有弹窗和管理后台导航保持当前项目样式，不引入 NookMux 的整套页面或目录结构。

## 2. 明确不迁移的内容

- 不迁移 NookMux 的 `internal/domain`、`internal/store`、`internal/httpapi` 架构；
- 不迁移站点订阅、支付、套餐销售和独立订阅计费系统；
- 不新增 CodingPlan 专用数据库表；
- 不重做渠道管理页的布局、颜色、导航或交互框架；
- 不补齐 NookMux 中与本目标无关的 GLM 活跃统计、账户报告、联系方式等页面；
- 不把厂商网页端指纹、无关浏览器行为或后台页面全部模拟出来。

## 3. 当前项目可复用基础

当前项目已经具备部分移植基础：

- `constant/channel.go` 已有 GLM、Kimi、豆包 CodingPlan 的特殊地址映射；
- `relay/channel/zhipu_4v/` 已有 GLM 特殊地址处理；
- `relay/channel/minimax/` 已有 MiniMax OpenAI/Claude 适配器；
- `controller/codex_usage.go` 和 `service/codex_wham_usage.go` 已提供“渠道操作接口调用上游、代理、超时、错误封装”的参考模式；
- `web/src/features/channels/` 已有渠道表、行操作、余额查询弹窗和渠道测试弹窗；
- 当前渠道路由已经使用 `ChannelRead`、`ChannelOperate` 等权限。

因此实施方式是补齐能力层和入口，不替换现有主干。

## 4. 计划标识和数据兼容

在现有 `model.ChannelInfo` 中增加非敏感字段：

```go
IsPlan   bool   `json:"is_plan"`
PlanName string `json:"plan_name"`
```

`PlanName` 使用内部稳定标识，例如：

- `glm-coding-plan`；
- `glm-coding-plan-international`；
- `kimi-coding-plan`；
- `minimax-coding-plan`；
- `minimax-coding-plan-international`。

字段只存入现有 `channel_info` JSON，不新增表和列。新增、修改、复制渠道时由后端根据渠道类型和特殊地址白名单计算，不能信任前端直接传入。旧渠道没有这两个字段时，应通过兼容检测补齐，不得导致普通渠道请求失败。

多 Key 渠道的计划查询暂不支持，查询凭证只取当前渠道的主 Key；响应、日志和错误信息不得泄露 Key、Token、Cookie 或完整上游响应中的凭证字段。

## 5. 转发和模型地址

### 5.1 地址解析

新增统一的计划地址解析函数，供以下路径共用：

- Claude 请求；
- OpenAI Chat Completions 请求；
- Responses 请求（厂商确实支持时才启用）；
- 上游模型列表获取；
- 渠道测试请求。

解析时统一处理尾部斜杠和协议前缀，避免重复拼接 `/v1`、`/api` 或厂商专用路径。普通渠道继续使用原来的 `ChannelBaseUrl`。

### 5.2 官方请求形态

只实现与实际转发有关的官方兼容行为：

- 正确的模型名和模型别名；
- 正确的厂商 endpoint；
- 必需的鉴权、Accept、Content-Type 和客户端标识请求头；
- Claude、OpenAI、Responses 的请求字段转换；
- 流式响应、错误响应和 usage 字段的兼容处理；
- 厂商返回 401、403、429、额度不足和风控拦截时保留可判断的错误分类。

不复制网页端专有的追踪字段，也不把不影响协议的浏览器指纹写死在公共转发逻辑中。

### 5.3 现有地址的处理原则

- 已存在的 GLM、Kimi、豆包特殊地址先保持兼容，再补充缺失的模型格式；
- MiniMax CodingPlan 使用独立的计划地址解析，不影响普通 MiniMax 渠道；
- Responses 只有在厂商和当前适配器均确认支持时才开放，未确认时返回明确的不支持错误；
- 模型列表获取必须和真实转发使用同一套地址解析，避免“能看到模型但实际请求失败”。

## 6. 后端管理接口

接口挂在现有管理员渠道路由下，沿用当前权限和错误封装。路径拟与 NookMux 的功能命名保持接近，最终以当前项目路由前缀为准。

### 6.1 额度

```text
GET /api/channel/plan/quota/:id
```

行为：

- 校验渠道存在、启用计划标识、计划名称在白名单内；
- 根据计划名称调用对应厂商查询器；
- 统一返回 `success`、`data.plan_name`、`data.quota_supported` 和厂商额度窗口；
- Kimi、MiniMax 返回各自可用的窗口、剩余量和重置时间；
- 未实现查询的计划返回 `quota_supported=false`，不伪造额度；
- 上游错误只返回可展示的错误分类，详细错误写服务端日志且脱敏。

### 6.2 GLM 风控

```text
GET /api/channel/plan/glm/risk/:id
```

只对 GLM CodingPlan 开放。返回统一的状态枚举和可展示说明，例如正常、需关注、已风控、查询失败；原始风控响应不直接透传到前端。

### 6.3 GLM 重置卡

```text
GET  /api/channel/plan/glm/reset_cards/:id
POST /api/channel/plan/glm/reset_cards/:id/use
```

规则：

- 查询接口只读；
- 使用接口只接收必要的卡片记录 ID 和重置类型；
- `requestId`、目标类型等字段由后端生成或固定，不能由前端注入；
- 使用重置卡前端必须显示二次确认；
- 成功后自动刷新额度和重置卡列表；
- 上游重复使用、卡片不存在、卡片过期、额度窗口变化等错误要分别展示。

该操作会改变上游账户状态，测试阶段只使用明确的测试账号和测试卡。

## 7. 前端入口

沿用现有 `web/src/features/channels/`：

- 在现有行操作菜单中，仅对 CodingPlan 渠道增加“查看额度”“风控状态”“重置卡”入口；
- 额度使用现有余额查询弹窗的交互风格，必要时拆出计划额度内容组件；
- 风控状态使用现有状态徽标和错误提示样式；
- 重置卡使用现有确认弹窗、列表和刷新机制；
- 不新增侧边栏页面，不调整渠道表列顺序，不改现有普通渠道入口；
- Kimi、MiniMax 和 GLM 共用页面结构，按后端返回字段显示不同额度窗口。

## 8. i18n

新增文本必须使用当前 i18n 机制，覆盖现有语言：

- English；
- 简体中文；
- 繁体中文；
- Français；
- 日本語；
- Русский；
- Tiếng Việt。

至少补齐以下文案类别：

- CodingPlan、计划类型和计划名称；
- 查看额度、刷新额度、下次重置；
- 风控正常、需关注、已风控、状态未知；
- 重置卡数量、卡片过期、确认使用、使用成功/失败；
- 计划不支持、凭证无效、上游超时、额度查询失败。

## 9. 测试计划

### 后端

- 计划识别和 `ChannelInfo` 旧 JSON 兼容测试；
- 各计划的 URL 拼接、模型地址和请求头测试；
- GLM、Kimi、MiniMax 额度响应 fixture 解析测试；
- GLM 风控状态和重置卡响应解析测试；
- 401、403、429、超时、空响应、错误 JSON 测试；
- 非计划渠道、多 Key 渠道、未知计划和无效渠道 ID 测试；
- 管理员权限和错误响应脱敏测试；
- 重置卡使用请求确认、参数校验和重复使用错误测试。

### 前端

- 普通渠道不显示 CodingPlan 专属入口；
- CodingPlan 渠道显示额度、风控、重置卡入口；
- 加载、成功、空数据和错误状态；
- 使用重置卡前确认，成功后刷新数据；
- 各语言 key 完整且没有未翻译的新增界面文本；
- 保持现有渠道管理页面布局和普通渠道回归测试通过。

### 真实上游验证

有可用测试凭证时，再执行各厂商最小请求验证：

- GLM CodingPlan：模型列表、一次最小请求、额度、风控、重置卡只读查询；
- Kimi CodingPlan：模型列表、一次最小请求、额度；
- MiniMax CodingPlan：模型列表、一次最小请求、额度；
- 重置卡使用只在用户明确指定的测试账号上执行，并记录前后额度状态。

没有真实凭证时，只能声明 fixture、handler 和前端测试通过，不能把上游可用性写成已验证。

## 10. 实施顺序和预计工期

| 阶段 | 内容 | 预计时间 |
| --- | --- | ---: |
| 1 | 计划识别、特殊地址、模型获取和转发 | 1.5–2.5 人日 |
| 2 | 额度、GLM 风控、重置卡后端接口 | 2.5–3.5 人日 |
| 3 | 现有渠道操作菜单、弹窗、状态展示和 i18n | 1.5–2.5 人日 |
| 4 | 后端/前端测试、上游 smoke test 和回归 | 1.5–2.5 人日 |
| **合计** | **不迁移整套 NookMux 架构** | **7–10 人日** |

如果暂时不做真实上游验证，可以先完成代码和 fixture 测试，但最终验收仍需用实际凭证验证地址、请求格式、额度字段和风控响应。

## 11. 验收标准

- 普通渠道的页面、转发和接口行为不变；
- CodingPlan 请求根据计划名称使用正确的模型地址和协议路径；
- GLM 能查询额度、风控和重置卡；
- Kimi、MiniMax 能查询额度；
- 重置卡使用前有确认，成功后额度和卡片状态会刷新；
- 不向前端或日志泄露密钥、Token、Cookie 和完整敏感上游响应；
- 后端测试、前端测试和 i18n 检查通过；
- 有真实凭证时完成至少一次每个厂商的最小请求验证；
- 不新增 NookMux 的整套目录和订阅业务，不改变现有页面整体样式。

## 12. 参考实现

- NookMux 仓库：[NookMux/NookMux](https://github.com/NookMux/NookMux)
- 当前项目计划地址：[constant/channel.go](../constant/channel.go)
- 当前项目渠道元数据：[model/channel.go](../model/channel.go)
- 当前项目渠道路由：[router/channel-router.go](../router/channel-router.go)
- 当前项目渠道前端：[web/src/features/channels](../web/src/features/channels)

NookMux 只作为功能和上游接口行为的参考，不直接迁移其内部目录结构或整套页面。

## 13. 本次实现记录（2026-09-19）

- 已在现有渠道模型、适配器和渠道路由上完成 CodingPlan 识别、GLM/Kimi/MiniMax 特殊地址解析、模型列表地址解析，以及 Claude、OpenAI Chat 和 GLM Responses 的已支持协议转发。
- 已加入额度、GLM 风控、GLM 重置卡列表和使用接口；计划名称由服务端根据渠道类型与白名单计算，旧 `channel_info` JSON 仍可读取。
- 已复用渠道表行操作和现有 Dialog 组件，普通渠道入口保持不变；重置卡使用由服务端校验卡片类型与可用性，前端增加二次确认并在成功后刷新。
- 已补齐 en、zh、zh-TW、fr、ja、ru、vi 文案，并运行 `bun run i18n:sync`。
- 当前 fixture/mock 验证覆盖：正常响应、旧 JSON、鉴权失败、403、429、超时、空响应、malformed JSON、重置卡服务端校验、普通渠道不进入计划逻辑。

## 14. 真实上游冒烟验证（2026-09-19）

使用用户提供的 GLM 国内 CodingPlan 凭证完成最小化验证；凭证未写入仓库、日志或文档：

- `GET /api/coding/paas/v4/models`：HTTP 200，返回 11 个模型；
- `POST /api/coding/paas/v4/chat/completions`：非流式 HTTP 200；流式 HTTP 200，返回 `text/event-stream`；
- `POST /api/v1/responses`：非流式和流式均 HTTP 200，流式返回事件流；
- `POST /api/anthropic/v1/messages`：非流式和流式均 HTTP 200，流式返回 `text/event-stream`；
- GLM 订阅、额度限制、风控状态和重置卡列表查询均 HTTP 200；风控返回正常状态，重置卡列表返回五小时卡和周卡各 1 条；
- 未调用重置卡使用接口，因此没有产生账户状态变更。

Kimi、MiniMax、GLM 国际站点以及完整生产环境仍未使用真实凭证验证；这些部分继续以 fixture/mock 和本地协议测试为依据。

本地容器验证：使用 `newapi-local-postgres-1`（PostgreSQL 15）创建临时数据库运行迁移稳定性、渠道模型组升级和模型调度测试，结果全部通过，临时数据库已删除；使用 `newapi-local-redis-1`（Redis 7）完成带认证的客户端连通性检查，返回 `PONG`。未修改现有业务数据库或 Redis 数据。

### 本地应用验证（2026-09-20）

已在上述 PostgreSQL 容器的独立验证库及 Redis DB 15 上启动当前代码，供手动验收。通过应用自身的登录、渠道新增/读取、模型获取、额度、风控及重置卡只读接口完成真实请求验证（均返回 `success: true`），未消耗重置卡。

本次应用验证发现 GLM 的 `nextResetTime` 实际可返回数字，原字符串字段会导致额度解析失败；已兼容 Unix 秒/毫秒时间戳、数字字符串及日期字符串，并补充 fixture 回归测试，`go test ./service/planquota -count=1` 通过。此前直连上游 HTTP 200 的记录只证明上游响应成功，不代表应用已正确解析；本次才补齐该应用路径验证。

### GLM 接入类型选择（2026-09-20）

GLM 渠道表单新增“接入类型”：普通 API、CodingPlan 国内版、CodingPlan 国际版。前端选择后自动填写现有 `base_url` 别名，无须手动输入 `glm-coding-plan`；后端渠道类型仍为 26，未新增后端类型、字段或数据库结构。已有别名（包括末尾斜杠、两侧空格）自动识别；保存继续使用原有地址规范化逻辑。

复用现有 `Select`、`Label` 和渠道表单。选择 CodingPlan 时隐藏手工地址输入，切回普通 API 可恢复本次编辑的自定义地址；切换提供商时清除 GLM 计划别名，插件仍按其默认地址填充。无敏感配置写权限时选择器禁用。Moonshot 和 MiniMax 复用同一选择器；其他提供商保持原有表单，计划操作权限和身份仍由后端计算。

模型获取沿用现有行为：新建时使用当前表单地址，编辑时使用已保存渠道配置，因此编辑接入类型后需先保存再获取对应模型。新增交互测试涵盖 GLM、Moonshot、MiniMax 计划的保存、新建时模型获取、旧别名、键盘切换及地址恢复、其他提供商/插件切换、权限禁用。渠道配置测试及现有 CodingPlan 弹窗/识别测试共 84 项通过（单 worker，单例超时 15 秒）；类型检查、修改文件 lint/格式检查、生产构建及七语言 i18n 同步通过。本次是前端配置交互验证，没有新增真实国际站上游验证。

新版前后端产物已构建。最初重启请求被执行工具策略拦截，未提供具体原因；用户调整执行权限后，已在原 3000 端口直接重启，沿用原 PostgreSQL 验证库和 Redis DB 15，未另开服务端口。已通过 HTTP 验证页面引用新版构建资源、应用健康检查、原 GLM 渠道识别和真实上游额度查询均正常；本轮未进行浏览器交互验收，也未使用重置卡。

### 额度时间显示修复（2026-09-20）

额度弹窗原来直接显示 `resets_at` 的 UTC 字符串。现复用项目日期格式化函数按浏览器本地时区显示，并明确标注“本地时间”；例如北京时间下 `2026-09-19T21:30:28Z` 显示为 `2026-09-20 05:30:28`。重置卡到期时间采用相同格式，无法解析的日期显示“未知”。后端时间值及上游重置周期不变。

共享弹窗中的 `five_hour`、`weekly_limit` 改为翻译后的“五小时额度”“每周额度”，补齐七语言。新增失败用例先复现日期原文展示问题，再验证修复；弹窗 9 项测试分别在 `Asia/Shanghai` 和 `America/New_York` 时区全部通过，涵盖 GLM/Kimi/MiniMax 额度、空/无效日期、到期时间、中文文案及原有加载/错误/刷新/二次确认行为。类型检查、修改文件 lint、格式化及生产构建通过，i18n 同步缺失和多余键均为 0（日语/俄语保留既有品牌词提示）。

范围说明：整体迁移包含 GLM、Kimi、MiniMax 的计划识别、对应支持协议与额度查询，以及豆包已有计划地址的识别/转发处理；豆包没有新增额度接口。接入类型下拉框现覆盖 GLM、Moonshot 和 MiniMax，并不改变火山普通渠道；豆包继续使用已有计划别名。Kimi/MiniMax/豆包及国际站尚未使用真实凭证验收。

已在原 3000 端口重启新版并使用现有测试账户完成浏览器验证：GLM 真实额度弹窗显示“五小时额度”“每周额度”，重置时间分别为 `2026-09-20 05:30:28（本地时间）`、`2026-09-24 22:09:41（本地时间）`，与上游 UTC 时间换算一致；截图确认原弹窗布局正常，未使用重置卡。

### 最终回归和发布前检查（2026-09-20）

回归期间修正了两个兼容性问题：额度客户端现在复制渠道 HTTP 客户端的传输配置但独立设置 15 秒超时，不再改变普通 relay 共享客户端；上游传输/解析错误不再把响应片段、代理凭证或密钥写入日志。MiniMax 的 CodingPlan Chat/Responses 未宣称支持时会明确返回不支持，普通 MiniMax 路径保持原有地址；火山普通 Chat、Bot、Responses、Embedding、Images 路径和模型列表地址均有回归断言。

最终通过：受影响后端包的计划、额度、控制器、模型和适配器测试；CodingPlan 定向后端测试；SQLite 3.50.4、MySQL 8.4.11、PostgreSQL 15.19 的 ChannelInfo 旧 JSON 读取/回写和迁移稳定性测试；独立 `relaykit` 构建；前端渠道配置、额度弹窗、计划工具测试共 85 项；前端类型检查、生产构建、修改文件 lint；i18n 同步报告七语言 `missingCount=0`、`extrasCount=0`。完整前端 lint 仍受仓库既有 lint 错误阻塞，修改文件本身无 lint 错误。

完整后端测试的失败来自当前仓库既有的 Windows SQLite 临时文件清理/认证 Passkey 测试及一次 HTTP/2 GoAway 重试测试；HTTP/2 用例随后单独复测通过，CodingPlan/普通渠道定向测试全部通过。独立基线对照同样复现认证和 SQLite 清理失败，因此没有证据表明本次迁移引入这些失败。完整前端测试单独复现 dashboard setup-guide 的既有可见性失败；该文件不在本次改动范围内。

真实上游仅验证了用户提供的 GLM 国内 CodingPlan：模型列表、Chat Completions 非流式/流式、Responses 非流式/流式、Anthropic 非流式/流式、额度、风控和重置卡列表均成功；没有调用重置卡使用。Kimi、MiniMax、GLM 国际、豆包真实凭证仍未验证，继续以 fixture/mock 和协议测试为依据。

### Moonshot 和 MiniMax 接入类型选择（2026-09-20）

Moonshot（渠道类型 25）和 MiniMax（渠道类型 35）现复用 GLM 使用的现有渠道表单选择器。Moonshot 选择“CodingPlan”后自动保存 `kimi-coding-plan`；MiniMax 提供国内版和国际版，分别保存 `minimax-coding-plan` 和 `minimax-coding-plan-international`。这些值继续写入原有 `base_url` 字段，不新增数据库字段或套餐表。

选择计划后隐藏手工 Base URL，切回标准 API 会恢复本次编辑保存的自定义地址；模型获取和保存沿用原有渠道接口。新增前端交互和别名解析测试均通过。普通火山渠道的模型列表、Chat、Bot、Responses、Embedding、Images 地址和鉴权回归测试继续通过；本次未使用 Kimi 或 MiniMax 真实凭证进行上游请求验证。
