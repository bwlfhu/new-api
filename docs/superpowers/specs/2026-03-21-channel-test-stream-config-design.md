# 渠道测试配置持久化与自动测试一致性设计

## 背景

当前渠道“模型测试”弹窗支持手动选择：

1. 测试模型
2. 端点类型
3. 是否使用流式

但这三者里，只有 `test_model` 已经是渠道持久化字段；`endpoint_type` 与 `stream` 仍是前端弹窗的临时状态。

这导致两个问题：

1. 手动测试可以通过打开“流式”开关测通某些渠道，但 15 分钟自动测试固定使用非流式请求，行为不一致。
2. 对于“必须以流式方式测试才能通过”的渠道，自动测试可能因为非流式测试失败而误触发自动禁用。

本次设计目标是把“渠道测试配置”建模为渠道自身属性，并让手动测试与自动测试共用同一组默认测试参数。

## 目标

### 功能目标

1. 为渠道新增可持久化的默认测试配置：
   - `test_model`
   - `test_endpoint_type`
   - `test_stream`
2. 渠道编辑页支持编辑上述默认测试配置。
3. “渠道的模型测试”弹窗打开时：
   - 默认模型取渠道配置
   - 默认端点类型取渠道配置
   - 默认“流式”开关展示取渠道配置
4. 手动单模型测试与批量测试默认使用这组渠道测试配置，但允许用户在弹窗内临时覆盖。
5. 15 分钟自动测试读取同一组渠道测试配置，而不再固定 `isStream=false`。
6. 修正文案，使测试页面说明与真实行为一致。

### 非目标

1. 不改变运行时真实转发请求的流式行为，只影响“渠道测试”链路。
2. 不引入全局级“按渠道类型默认流式测试”配置。
3. 不改变现有自动禁用/自动启用的总体策略，只修正测试输入参数来源。
4. 不在本次设计中引入健康检查熔断算法或基于历史成功率的选路策略。

## 现状与根因

### 手动测试

文件：[web/src/hooks/channels/useChannelsData.jsx](/home/team/huyuwen/project/new-api/web/src/hooks/channels/useChannelsData.jsx)

现状：

1. `isStreamTest` 是前端临时状态。
2. 发起测试请求时，仅在本次 URL 上追加 `&stream=true`。
3. 关闭弹窗或刷新页面后，该状态丢失。

这意味着：

1. 用户可以“本次测试时手动打开流式”。
2. 但系统无法把这个行为记为该渠道的默认测试方式。

### 自动测试

文件：[controller/channel-test.go](/home/team/huyuwen/project/new-api/controller/channel-test.go)

现状：

1. 自动测试由 `AutomaticallyTestChannels()` 定时调用 `testAllChannels(false)`。
2. `testAllChannels()` 遍历渠道时固定执行 `testChannel(channel, "", "", false)`。
3. 因此自动测试固定使用：
   - 空测试模型（最终回退到渠道默认/首个模型）
   - 自动检测端点
   - 非流式

这意味着：

1. 自动测试和手动测试根本不是同一套输入。
2. 某些“只在流式测试下可通过”的渠道，会在自动测试中出现假失败。

### 自动禁用风险

自动测试失败后，只要满足以下条件，就可能触发禁用：

1. 开启“失败时自动禁用通道”
2. 渠道本身 `auto_ban=true`
3. 错误码命中自动禁用范围，或错误被识别为渠道错误

因此在当前实现下，自动测试参数不准确会直接放大成误禁用风险。

## 方案对比

### 方案 A：新增渠道显式字段

新增渠道字段：

1. `test_endpoint_type`
2. `test_stream`

优点：

1. 语义清晰，与已有 `test_model` 一致。
2. 前后端读取简单，排查方便。
3. 自动测试、手动测试、后续接口返回都可以直接复用。

缺点：

1. 需要数据库迁移。

### 方案 B：写入 `channel_info` JSON

把测试配置写入 `channel_info`。

优点：

1. 不需要新增表字段。

缺点：

1. `channel_info` 当前主要承载多 Key 运行态信息。
2. 将测试默认配置塞进去会混淆“运行态状态”和“静态配置”边界。
3. 后续维护和排查不如显式字段直观。

### 方案 C：直接复用弹窗本地状态并持久化

保留当前前端弹窗结构，仅在关闭/保存时写回某个隐藏配置。

优点：

1. 前端表面改动少。

缺点：

1. 语义模糊，不利于后端自动测试直接读取。
2. 容易出现“当前测试参数”和“渠道默认测试配置”混淆。

### 推荐方案

采用方案 A。

理由：

1. `test_model` 已经是显式字段，继续新增 `test_endpoint_type`、`test_stream` 最一致。
2. 这组配置本质上属于渠道静态元数据，不属于运行态 `channel_info`。
3. 自动测试是后端能力，直接读取显式字段比解析嵌套 JSON 更稳健。

## 设计方案

## 数据模型

在 `Channel` 中新增两个可持久化字段：

1. `TestEndpointType *string  json:"test_endpoint_type"`
2. `TestStream       *bool    json:"test_stream"`

行为定义：

1. `test_endpoint_type`
   - 空值：沿用现有自动检测逻辑
   - 非空：使用指定端点测试
2. `test_stream`
   - 空值或 `false`：默认非流式测试
   - `true`：默认流式测试

兼容策略：

1. 老渠道未设置时，不影响现有行为。
2. 老数据读取时：
   - `test_endpoint_type = nil`
   - `test_stream = nil`

## 后端设计

### 1. 渠道结构与入参

更新后端渠道结构、序列化与编辑接口，使新增字段可被创建/编辑接口接收并返回。

影响范围：

1. `model.Channel`
2. 渠道增删改查接口 DTO / JSON 绑定
3. 相关前端详情加载接口

### 2. 自动测试读取渠道默认测试配置

当前：

```go
result := testChannel(channel, "", "", false)
```

调整后：

1. 默认模型读取 `channel.TestModel`
2. 默认端点读取 `channel.TestEndpointType`
3. 默认流式读取 `channel.TestStream`

等价行为：

```go
result := testChannel(
  channel,
  "",
  lo.FromPtr(channel.TestEndpointType),
  lo.FromPtr(channel.TestStream),
)
```

说明：

1. `testChannel()` 本身已经支持 `endpointType` 与 `isStream` 参数，无需改其核心测试协议。
2. 这次只需要把自动测试调用参数从常量改成读取渠道配置。

### 3. 手动测试接口保持兼容

当前手动测试接口已经支持：

1. `model`
2. `endpoint_type`
3. `stream`

本次不需要改变接口协议。

保留规则：

1. 手动测试请求若显式传参，优先使用用户当前测试参数。
2. 自动测试则读取渠道默认配置。

## 前端设计

### 1. 编辑渠道页新增默认测试配置

文件：[web/src/components/table/channels/modals/EditChannelModal.jsx](/home/team/huyuwen/project/new-api/web/src/components/table/channels/modals/EditChannelModal.jsx)

新增可编辑项：

1. 默认测试模型
2. 默认测试端点类型
3. 默认测试使用流式

设计原则：

1. 与已有 `test_model` 放在同一测试配置区域。
2. `test_endpoint_type` 复用当前模型测试弹窗的端点选项枚举。
3. `test_stream` 使用布尔开关。

### 2. 模型测试弹窗默认值来自渠道配置

文件：[web/src/components/table/channels/modals/ModelTestModal.jsx](/home/team/huyuwen/project/new-api/web/src/components/table/channels/modals/ModelTestModal.jsx)

文件：[web/src/hooks/channels/useChannelsData.jsx](/home/team/huyuwen/project/new-api/web/src/hooks/channels/useChannelsData.jsx)

调整为：

1. 打开某个渠道的模型测试弹窗时：
   - `selectedEndpointType` 默认取 `currentTestChannel.test_endpoint_type`
   - `isStreamTest` 默认取 `currentTestChannel.test_stream`
2. 这正是用户提出的期望：
   - “渠道测试页面，流式开关的默认展示，根据这个配置来展示”

说明：

1. 这是“默认展示”与“默认测试行为”的统一。
2. 它不会强制锁定用户操作，用户仍可在本次弹窗内临时调整。

### 3. 弹窗内临时改动不自动回写渠道配置

本次保持以下边界：

1. 编辑渠道页负责保存默认测试配置
2. 测试弹窗负责临时发起测试

这样可以避免：

1. 用户只想临时试一次流式，却无意中修改渠道默认测试配置

### 4. 批量测试行为

批量测试使用弹窗当前参数，而不是强制每个模型都回退到渠道默认值。

也就是说：

1. 打开弹窗时，先用渠道默认配置初始化 UI
2. 用户若手动切换流式/端点类型
3. 当前弹窗内的单测与批量测试都使用当前 UI 值

这样行为更符合直觉。

## 文案调整

当前文案存在误导：

> 本页测试为非流式请求；若渠道仅支持流式返回，可能出现测试失败，请以实际使用为准。

但页面实际上已经有“流式”开关。

调整建议：

1. 改为说明“默认测试参数来自渠道配置，可在弹窗中临时覆盖”
2. 在流式开关旁增加提示：
   - “默认值取自渠道配置”

建议文案：

1. 说明 banner：
   - `本页测试默认使用渠道配置中的测试参数；你可以在当前弹窗内临时修改端点类型和流式开关。`
2. 流式开关提示：
   - `默认值取自渠道默认测试配置`

## 数据流

### 编辑渠道默认测试配置

1. 管理员打开编辑渠道弹窗
2. 修改：
   - `test_model`
   - `test_endpoint_type`
   - `test_stream`
3. 提交渠道编辑
4. 后端持久化到 `channels` 表

### 手动测试

1. 管理员打开“渠道的模型测试”弹窗
2. 前端用渠道默认测试配置初始化：
   - 模型
   - 端点
   - 流式开关
3. 用户可临时修改
4. 当前测试请求使用弹窗当前参数

### 自动测试

1. 定时任务读取渠道列表
2. 对每个渠道读取默认测试配置
3. 调用 `testChannel()` 执行测试
4. 测试结果进入现有自动禁用/自动启用逻辑

## 风险与兼容性

### 风险 1：数据库迁移

新增字段需要做 schema 迁移。

缓解：

1. 允许字段为空
2. 空值回退到现有自动检测/非流式逻辑

### 风险 2：前端默认值与后端默认值不一致

缓解：

1. 前端初始化时严格读取渠道详情返回值
2. 空值统一回退：
   - `test_endpoint_type = ''`
   - `test_stream = false`

### 风险 3：用户误以为弹窗改动会自动保存

缓解：

1. 明确区分“默认配置”和“本次测试参数”
2. 将默认配置编辑入口保留在编辑渠道页

## 验证方案

### 功能验证

1. 编辑一个普通 OpenAI 渠道：
   - 设置 `test_endpoint_type=openai-response`
   - 设置 `test_stream=true`
   - 保存后重新打开编辑页，值应正确回显
2. 打开模型测试弹窗：
   - 端点类型默认选中 `openai-response`
   - 流式开关默认开启
3. 在弹窗中临时关闭流式并测试：
   - 本次请求使用非流式
   - 关闭弹窗重开后仍恢复为渠道配置默认值
4. 自动测试执行时：
   - 后端日志能体现该渠道测试使用了持久化的 `endpoint_type` 与 `stream`

### 回归验证

1. 未设置新字段的老渠道仍可正常测试
2. 批量测试仍可工作
3. 自动测试对不支持流式的端点仍保持现有限制
4. 自动禁用/自动启用逻辑不因本次改动失效

## 推荐实施顺序

1. 后端 `Channel` 模型新增字段与迁移
2. 渠道增删改查接口透传新字段
3. 编辑渠道页增加默认测试配置 UI
4. 模型测试弹窗初始化逻辑改为读取渠道配置
5. 自动测试改为读取渠道默认测试配置
6. 调整文案并补充验证
