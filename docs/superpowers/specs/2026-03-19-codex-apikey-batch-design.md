# Codex 渠道 API Key 批量创建与新增态一致性设计

## 背景

今天凌晨分支已经为 Codex 渠道补充了 API Key / OAuth 凭据兼容能力，但当前实现只在编辑态部分生效，新增态仍存在两类不一致：

1. 前端虽然已经存在 `inputs.type === 57` 的 Codex 专用表单分支，但提交逻辑仍直接阻止 Codex 批量创建。
2. 后端 `validateChannel()` 仍将 Codex `key` 强制校验为单个 OAuth JSON 对象，导致普通 API Key 和多行 API Key 批量创建都无法通过。

本次增强目标是在不破坏已有 Codex OAuth 能力的前提下，使新增态、编辑态、批量创建能力和后端校验规则保持一致。

## 目标

### 功能目标

1. 新增渠道时，选择 `Codex` 后支持显式选择凭据模式：
   - `API Key`
   - `OAuth JSON`
2. `API Key` 模式下，新增页支持像 OpenAI 一样的多行 API Key 批量创建。
3. `OAuth JSON` 模式下，仅支持单渠道创建，不支持批量。
4. 编辑已有 Codex 渠道时，根据已保存的密钥内容自动推断当前凭据模式。
5. 后端移除“Codex key 必须是 JSON”的强约束，改为兼容：
   - 普通字符串 API Key
   - 多行 API Key
   - OAuth JSON（仅在检测到 JSON 对象时做结构校验）

### 非目标

1. 不引入新的数据库字段持久化 Codex 凭据模式。
2. 不为 Codex 增加“批量 OAuth JSON 导入”能力。
3. 不调整已有 Codex OAuth 授权、刷新凭据、用量查询接口。
4. 不扩展到其他渠道类型的批量创建行为。

## 现状与根因

## 前端

文件：[web/src/components/table/channels/modals/EditChannelModal.jsx](/home/team/huyuwen/project/new-api/web/src/components/table/channels/modals/EditChannelModal.jsx)

当前已存在：

1. `parseCodexCredential()`，可识别空值、普通 API Key、非法 JSON、合法 OAuth JSON。
2. `inputs.type === 57` 的 Codex 专用密钥输入区域。
3. `CodexOAuthModal`、OAuth 刷新按钮、格式化按钮。

当前阻塞点：

1. `submit()` 中对 Codex 直接执行：
   - 若 `batch` 为 `true`，提示“Codex 渠道不支持批量创建”并返回。
2. `batchAllowed` 条件写死排除了 `inputs.type !== 57`，导致新增 Codex 时批量创建 UI 不可用。
3. 前端没有显式的 Codex 凭据模式切换，新增态只能依赖输入内容隐式判断，不利于明确区分批量 API Key 和单个 OAuth JSON。

## 后端

文件：[controller/channel.go](/home/team/huyuwen/project/new-api/controller/channel.go)

当前阻塞点：

1. `validateChannel()` 中对 `ChannelTypeCodex` 的校验逻辑要求：
   - `key` 必须以 `{` 开头
   - 必须能解析为 JSON 对象
   - JSON 必须包含 `access_token` 和 `account_id`
2. 这会直接拒绝：
   - 单个普通 API Key
   - 多行普通 API Key 批量创建
3. `AddChannel()` 本身的批量拆分逻辑已经支持按换行拆分 key，因此核心问题不在批量创建逻辑，而在前置校验过严。

## 设计方案

采用“前端双模式显式切换 + 后端兼容性校验”的方案。

## 前端设计

### 1. 增加 Codex 凭据模式状态

在 `EditChannelModal` 中增加仅前端使用的状态，例如：

- `codexCredentialMode: 'api_key' | 'oauth_json'`

该状态只用于控制 UI 与前端校验，不写入后端，不新增数据库字段。

### 2. 模式默认值与编辑态推断

#### 新增态

当用户选择 `Codex` 类型时：

- 默认模式设为 `api_key`
- 默认允许批量创建

#### 编辑态

加载已有 Codex 渠道时，通过 `parseCodexCredential(data.key)` 自动推断：

- `oauth` => `codexCredentialMode = 'oauth_json'`
- 其他情况 => `codexCredentialMode = 'api_key'`

说明：

1. 不新增后端字段存储模式，避免迁移和兼容成本。
2. 若极端情况下某个 API Key 以 `{` 开头且恰好像 JSON，将被误判为 JSON 模式；该风险很低，可接受。

### 3. 表单交互

#### `api_key` 模式

显示普通密钥输入框，支持：

1. 单个 API Key
2. 多行 API Key
3. 批量创建

行为要求：

1. 批量开关可见且可用。
2. 提示文案明确说明支持多行输入批量创建。
3. 提交时不做 JSON 校验。

#### `oauth_json` 模式

显示现有 Codex OAuth JSON 输入体验，保留：

1. 授权按钮
2. 刷新凭据按钮
3. JSON 格式化按钮
4. OAuth 字段说明

行为要求：

1. 批量开关隐藏或禁用。
2. 若用户从 `api_key` 模式切换到 `oauth_json` 模式：
   - 自动关闭 `batch`
   - 自动关闭 `multiToSingle`
3. 提交时要求内容是合法 JSON 对象，并包含 `access_token` 与 `account_id`。

### 4. 提交逻辑

#### Codex + `api_key`

在 `submit()` 中：

1. 不再阻止批量创建。
2. 将输入内容按普通密钥处理：
   - 单渠道创建：原样提交
   - 批量创建：沿用现有按换行拆分的后端逻辑
3. 如果新增态输入为空，仍提示“请输入密钥”。

#### Codex + `oauth_json`

在 `submit()` 中：

1. 保留当前 JSON 结构校验。
2. 若 `batch === true`，前端先自动关闭或阻止提交。
3. 若校验成功，使用 `JSON.stringify(credential.parsed)` 标准化后提交。

### 5. 批量开关判定规则

现有：

- `const batchAllowed = (!isEdit || isMultiKeyChannel) && inputs.type !== 57;`

调整为基于凭据模式判定：

- 新增 Codex 且 `codexCredentialMode === 'api_key'` 时允许批量
- Codex + `oauth_json` 时不允许批量
- 其他渠道继续沿用现有规则

### 6. 文案与提示

Codex 的密钥区域说明根据模式变化：

#### `api_key`

- 提示支持普通 API Key
- 提示支持多行输入进行批量创建

#### `oauth_json`

- 提示支持 OAuth JSON
- 提示必须包含 `access_token` 与 `account_id`
- 明确说明仅支持单渠道

## 后端设计

### 1. 调整 `validateChannel()` 中的 Codex 校验

现有逻辑：只允许 JSON OAuth。

调整后逻辑：

1. 取得 `trimmedKey := strings.TrimSpace(channel.Key)`
2. 在新增时或编辑时传入非空 key 的情况下：
   - 若 `trimmedKey` 以 `{` 开头：
     - 按 OAuth JSON 校验
     - 必须是合法 JSON 对象
     - 必须包含 `access_token`
     - 必须包含 `account_id`
   - 否则：
     - 视为普通 API Key 或多行 API Key
     - 不执行 JSON 校验

这样后端自然兼容三种输入：

1. 单个 API Key
2. 多行 API Key
3. OAuth JSON

### 2. `AddChannel()` 无需新增 Codex 特判

原因：

1. 批量模式下，普通渠道已经使用 `strings.Split(channel.Key, "\n")` 做拆分。
2. Codex 的 API Key 批量创建可以直接复用此逻辑。
3. 只要前端允许提交、后端放宽校验，现有批量创建流程即可工作。

## 数据流

### 新增 Codex API Key

1. 用户选择 `Codex`
2. 前端默认进入 `api_key` 模式
3. 用户输入单个或多行 API Key
4. 若开启批量，则以前端现有 `mode: batch` 提交
5. 后端将 `key` 按换行拆分为多个渠道
6. 每个渠道单独入库

### 新增 Codex OAuth JSON

1. 用户选择 `Codex`
2. 前端切换到 `oauth_json`
3. 用户手动输入或通过授权弹窗填入 JSON
4. 前端校验 JSON 结构并标准化
5. 后端再次校验 `access_token` 与 `account_id`
6. 单渠道入库

### 编辑 Codex 渠道

1. 前端加载渠道详情
2. 根据已保存 key 自动推断模式
3. 根据模式渲染对应输入区
4. 提交时沿用对应模式的校验规则

## 错误处理

### 前端

1. `api_key` 模式：
   - 空值拦截
   - 不做 JSON 格式提示
2. `oauth_json` 模式：
   - 非法 JSON => 明确提示
   - JSON 不是对象 => 明确提示
   - 缺少 `access_token` / `account_id` => 明确提示
3. 模式切换时自动清理互斥状态：
   - 切到 `oauth_json` 时关闭批量状态

### 后端

1. Codex + JSON 输入但结构非法时，返回明确错误信息。
2. Codex + 普通字符串输入时，不返回 JSON 校验错误。

## 测试与验证

### 前端手工验证

1. 新增 Codex，默认显示 `API Key` 模式。
2. 新增 Codex，`API Key` 模式下支持开启批量创建。
3. 新增 Codex，`API Key` 模式输入多行 key 后可成功批量创建。
4. 新增 Codex，切换到 `OAuth JSON` 后批量能力关闭。
5. 新增 Codex，`OAuth JSON` 输入合法 JSON 可成功创建。
6. 新增 Codex，`OAuth JSON` 输入缺少字段会被阻止提交。
7. 编辑已有 Codex API Key 渠道时，界面自动进入 `API Key` 模式。
8. 编辑已有 Codex OAuth 渠道时，界面自动进入 `OAuth JSON` 模式。

### 后端自动化验证

建议为 Codex 校验补充测试，至少覆盖：

1. `validateChannel()` 接受普通 API Key。
2. `validateChannel()` 接受多行 API Key。
3. `validateChannel()` 接受合法 OAuth JSON。
4. `validateChannel()` 拒绝缺少 `access_token` 的 OAuth JSON。
5. `validateChannel()` 拒绝缺少 `account_id` 的 OAuth JSON。

## 风险与兼容性

1. 不新增数据库字段，兼容历史数据与现有接口。
2. 不修改 Codex OAuth 接口协议，风险集中在渠道新增/编辑表单和后端校验。
3. 批量创建仅对 `API Key` 模式开放，避免引入 JSON 批量解析和命名规则复杂度。
4. 现有 OpenAI、Vertex、AWS 等渠道逻辑不应受影响，需控制改动边界仅限 Codex 分支与通用批量判定条件。

## 实施范围

主要文件：

1. [web/src/components/table/channels/modals/EditChannelModal.jsx](/home/team/huyuwen/project/new-api/web/src/components/table/channels/modals/EditChannelModal.jsx)
2. [controller/channel.go](/home/team/huyuwen/project/new-api/controller/channel.go)
3. 视测试落点补充对应 `*_test.go`

## 决策结论

本次采用：

1. 前端为 Codex 引入显式凭据模式切换
2. `API Key` 模式开放批量创建
3. `OAuth JSON` 模式继续单渠道
4. 后端改为“仅当 key 看起来像 JSON 对象时才执行 OAuth JSON 校验”

这样可以最小化对现有数据结构和后端接口的影响，同时把新增态、编辑态、批量能力和后端校验规则统一起来。
