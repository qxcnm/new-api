# 渠道额外设置说明

该配置用于设置一些额外的渠道参数，可以通过 JSON 对象进行配置。主要包含以下三个设置项：

1. force_format
    - 用于标识是否对数据进行强制格式化为 OpenAI 格式
    - 类型为布尔值，设置为 true 时启用强制格式化

2. proxy
    - 用于配置网络代理
    - 类型为字符串，支持 `http`、`https`、`socks5` 和 `socks5h` 协议
    - 保存时必须包含协议和主机；仅允许空路径或根路径 `/`，不允许 query 或 fragment
    - SOCKS 代理未填写端口时，运行时使用默认端口 `1080`

3. thinking_to_content
   - 用于标识是否将思考内容`reasoning_content`转换为`<think>`标签拼接到内容中返回
   - 类型为布尔值，设置为 true 时启用思考内容转换

--------------------------------------------------------------

## JSON 格式示例

以下是一个示例配置，启用强制格式化并设置了代理地址：

```json
{
    "force_format": true,
    "thinking_to_content": true,
    "proxy": "socks5://proxy.example:1080"
}
```

--------------------------------------------------------------

通过调整上述 JSON 配置中的值，可以灵活控制渠道的额外行为，比如是否进行格式化以及使用特定的网络代理。

## 升级兼容性

旧版本会忽略代理地址中的 path、query 和 fragment。为避免升级后中断已有渠道流量，运行时会继续剥离这些遗留后缀，并对同一代理地址每个进程记录一次不含凭证和后缀的警告。该兼容逻辑不会改写数据库；再次保存渠道时必须按上述严格规则修正代理地址。

代理连接使用 30 秒 TCP 拨号超时和 30 秒 KeepAlive；TLS 握手超时为 10 秒。这些超时同样适用于未配置渠道代理的中转请求。


## 模型分组绑定

在渠道编辑页先选择渠道支持的模型和分组，再在「模型分组」中逐个模型选择分组。地址和密钥只需配置一次；未单独绑定的模型继承渠道分组，可用「使用渠道分组」恢复继承。

例如，配置 `economy` 分组倍率为 `0.1`、`premium` 为 `0.5`，同一个渠道的模型 A 绑定 `economy`、模型 B 绑定 `premium`。模型仍使用各自的基础定价，实际请求按选中的分组倍率计费。若一个令牌需要同时调用这两个模型，先在用户可用分组中允许 `auto` 和这两个实际分组，再将令牌分组设为 `auto`，并把这两个分组加入其允许自动选择的分组列表；指定分组的令牌只能调用该组已绑定的模型。一个模型绑定多个分组时，倍率取实际选中的分组，自动选择按已有分组顺序执行。

管理 API 使用渠道顶层字段 `model_groups`（不是额外设置 `setting` 中的字段），值为 JSON 字符串：

```json
{
  "models": "model-a,model-b",
  "group": "economy,premium",
  "model_groups": "{\"model-a\":[\"economy\"],\"model-b\":[\"premium\"]}"
}
```

新增或显式修改绑定时，模型必须属于渠道模型列表，分组必须属于渠道分组列表，每项至少选择一个分组。更新时不传 `model_groups` 会保留原绑定，传空字符串、`null` 或 `"{}"` 会清空绑定并恢复继承。旧渠道升级后默认继续继承原来的渠道分组。

自动同步、删除模型或批量调整渠道分组会保留原绑定，以免重新加入模型时意外放宽权限；实际路由只使用当前模型和分组的交集。再次编辑时，可恢复这些已移除模型的分组继承或在 JSON 模式删除对应绑定。配置格式错误会阻止保存或路由，不会自动回退到所有分组。


### 模型分组回归验证

2026-09-17 在真实 SQLite 3.50.4、MySQL 8.4.11、PostgreSQL 17.6 上验证通过。MySQL/PostgreSQL 使用本次测试创建的隔离数据库实例；未使用应用数据库。测试环境设置了 `CHANNEL_MODEL_GROUPS_MYSQL_DSN`、`CHANNEL_MODEL_GROUPS_POSTGRES_DSN`、`TEST_MYSQL_DSN` 和 `TEST_POSTGRES_DSN`。

```powershell
go test ./model -count=1
go test ./controller -run 'TestChannelModelGroupsManagementDatabaseMatrix|TestModelDeletionDatabaseMatrix|TestUpstreamModelUpdatesPreserveModelGroupsDatabaseMatrix|TestChannelFieldsAreClassified|TestValidateChannelModelGroupsRequiresKnownModelsAndGroups' -count=1
```

两条命令均退出 0。模型测试包括三库新建表及从最新发布 `v1.0.0-rc.37` 渠道结构升级的代表性旧数据，每种情况执行两次迁移，第二次无 schema DDL，原数据、索引和唯一约束保留。同步覆盖数据库/缓存路由、分组价格可见性、管理 API 保存及清空、自动同步模型、模型删除和标签批量更新回滚。本次新增字段只在主库的渠道表，未更改独立日志库结构。
