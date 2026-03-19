# Codex API Key 批量创建实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 Codex 渠道在新增态支持 API Key 模式与 OAuth JSON 模式，且 API Key 模式支持像 OpenAI 一样的批量创建，同时放宽后端对 Codex key 的 JSON 强校验。

**Architecture:** 后端先通过测试锁定 Codex key 校验的正确边界，只在 `key` 看起来像 OAuth JSON 时执行结构校验；前端在单一表单组件内新增一个仅 UI 使用的 Codex 凭据模式状态，借此切换交互、批量开关和提交流程，不引入新的持久化字段。整体改动限定在现有渠道新增/编辑主表单与后端统一校验入口，避免影响 Codex 授权和已有渠道行为。

**Tech Stack:** Go, Gin, Testify, React 18, Semi UI, Bun, ESLint

---

## 文件结构

### 需要修改

- [controller/channel.go](/home/team/huyuwen/project/new-api/controller/channel.go)
  责任：统一的渠道新增/编辑校验入口；调整 Codex key 校验逻辑。
- [controller/channel_upstream_update_test.go](/home/team/huyuwen/project/new-api/controller/channel_upstream_update_test.go) 或新增同目录测试文件
  责任：补充 Codex key 校验测试，覆盖普通 API Key、多行 API Key、合法/非法 OAuth JSON。
- [web/src/components/table/channels/modals/EditChannelModal.jsx](/home/team/huyuwen/project/new-api/web/src/components/table/channels/modals/EditChannelModal.jsx)
  责任：Codex 新增/编辑 UI、凭据模式切换、批量开关判定、提交流程与文案。

### 不应修改

- [controller/codex_oauth.go](/home/team/huyuwen/project/new-api/controller/codex_oauth.go)
  原因：本次不改 Codex OAuth 协议或授权流程。
- [web/src/components/table/channels/modals/CodexOAuthModal.jsx](/home/team/huyuwen/project/new-api/web/src/components/table/channels/modals/CodexOAuthModal.jsx)
  原因：授权弹窗能力已存在，仅由主表单决定何时展示。

### 验证命令

- 后端定向测试：`go test ./controller -run 'TestValidateChannelCodex' -count=1`
- 后端相关回归：`go test ./controller -count=1`
- 前端静态检查：`cd web && bun run eslint`
- 前端构建验证：`cd web && bun run build`

说明：

1. 当前仓库未发现现成的 `EditChannelModal` 前端单测基建，本计划不引入新的测试框架。
2. 前端行为通过受控手工回归验证补充。

## Task 1: 后端测试先行锁定 Codex key 校验边界

**Files:**
- Modify: [controller/channel_upstream_update_test.go](/home/team/huyuwen/project/new-api/controller/channel_upstream_update_test.go) 或新建 `controller/channel_validation_test.go`
- Modify: [controller/channel.go](/home/team/huyuwen/project/new-api/controller/channel.go)

- [ ] **Step 1: 写失败测试，覆盖普通 API Key 与 OAuth JSON 两类输入**

在测试文件中新增以下场景：

```go
func TestValidateChannelCodexAcceptsPlainAPIKey(t *testing.T) {
	channel := &model.Channel{
		Type:   constant.ChannelTypeCodex,
		Key:    "sk-codex-plain",
		Models: "codex-mini-latest",
	}

	err := validateChannel(channel, true)
	require.NoError(t, err)
}

func TestValidateChannelCodexAcceptsMultiLineAPIKeys(t *testing.T) {
	channel := &model.Channel{
		Type: constant.ChannelTypeCodex,
		Key:  "sk-codex-a\nsk-codex-b",
		Models: "codex-mini-latest",
	}

	err := validateChannel(channel, true)
	require.NoError(t, err)
}

func TestValidateChannelCodexAcceptsOAuthJSON(t *testing.T) {
	channel := &model.Channel{
		Type: constant.ChannelTypeCodex,
		Key:  `{\"access_token\":\"token\",\"account_id\":\"acct_123\"}`,
		Models: "codex-mini-latest",
	}

	err := validateChannel(channel, true)
	require.NoError(t, err)
}

func TestValidateChannelCodexRejectsOAuthJSONWithoutAccessToken(t *testing.T) {
	channel := &model.Channel{
		Type: constant.ChannelTypeCodex,
		Key:  `{\"account_id\":\"acct_123\"}`,
		Models: "codex-mini-latest",
	}

	err := validateChannel(channel, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "access_token")
}

func TestValidateChannelCodexRejectsOAuthJSONWithoutAccountID(t *testing.T) {
	channel := &model.Channel{
		Type: constant.ChannelTypeCodex,
		Key:  `{\"access_token\":\"token\"}`,
		Models: "codex-mini-latest",
	}

	err := validateChannel(channel, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "account_id")
}
```

- [ ] **Step 2: 运行定向测试，确认当前实现按预期失败**

Run: `go test ./controller -run 'TestValidateChannelCodex' -count=1`

Expected:
- 普通 API Key / 多行 API Key 用例失败
- 失败信息表明当前实现仍将 Codex key 强制当作 JSON 对象

- [ ] **Step 3: 最小化修改 `validateChannel()`**

将 [controller/channel.go](/home/team/huyuwen/project/new-api/controller/channel.go) 的 Codex 分支改为：

```go
if channel.Type == constant.ChannelTypeCodex {
	trimmedKey := strings.TrimSpace(channel.Key)
	if isAdd || trimmedKey != "" {
		if strings.HasPrefix(trimmedKey, "{") {
			var keyMap map[string]any
			if err := common.Unmarshal([]byte(trimmedKey), &keyMap); err != nil {
				return fmt.Errorf("Codex key must be a valid JSON object")
			}
			if v, ok := keyMap["access_token"]; !ok || v == nil || strings.TrimSpace(fmt.Sprintf("%v", v)) == "" {
				return fmt.Errorf("Codex key JSON must include access_token")
			}
			if v, ok := keyMap["account_id"]; !ok || v == nil || strings.TrimSpace(fmt.Sprintf("%v", v)) == "" {
				return fmt.Errorf("Codex key JSON must include account_id")
			}
		}
	}
}
```

实现要求：

1. 非 JSON 开头的 Codex key 直接视为普通 API Key，不做 JSON 校验。
2. JSON 开头时仍使用 `common.Unmarshal`，不要直接调用 `encoding/json`。
3. 不修改其他渠道校验逻辑。

- [ ] **Step 4: 重新运行定向测试，确认修复通过**

Run: `go test ./controller -run 'TestValidateChannelCodex' -count=1`

Expected:
- 5 个 Codex 校验测试全部通过

- [ ] **Step 5: 运行 controller 包回归**

Run: `go test ./controller -count=1`

Expected:
- controller 包已有测试继续通过

- [ ] **Step 6: 提交后端校验改动**

```bash
git add controller/channel.go controller/channel_upstream_update_test.go
git commit -m "test: cover codex channel key validation"
```

如果新建独立测试文件，则将命令中的测试文件路径替换为实际文件名。

## Task 2: 前端为 Codex 增加显式凭据模式

**Files:**
- Modify: [web/src/components/table/channels/modals/EditChannelModal.jsx](/home/team/huyuwen/project/new-api/web/src/components/table/channels/modals/EditChannelModal.jsx)

- [ ] **Step 1: 先抽出 Codex 模式判定的最小辅助逻辑**

在组件顶部已有 `parseCodexCredential()` 的基础上，仅增加一个简单的模式推断 helper，避免把 JSX 条件判断写散：

```jsx
function inferCodexCredentialMode(raw) {
  const credential = parseCodexCredential(raw);
  return credential.mode === 'oauth' ? 'oauth_json' : 'api_key';
}
```

要求：

1. 不新增独立文件。
2. 不重构现有 `parseCodexCredential()` 行为。

- [ ] **Step 2: 新增组件状态并在加载/切类型时同步**

在组件 state 中增加：

```jsx
const [codexCredentialMode, setCodexCredentialMode] = useState('api_key');
```

然后补两处同步：

1. 新增态选择 `Codex` 类型时，默认切到 `api_key`
2. 编辑态加载已有 Codex 渠道后，根据 `data.key` 调用 `inferCodexCredentialMode(data.key)` 回填

额外要求：

1. 切到 `oauth_json` 时，自动执行：
   - `setBatch(false)`
   - `setMultiToSingle(false)`
   - `setMultiKeyMode('random')`
2. 不改变 Vertex/AWS 等其他类型切换逻辑。

- [ ] **Step 3: 更新批量创建可用条件**

将当前：

```jsx
const batchAllowed = (!isEdit || isMultiKeyChannel) && inputs.type !== 57;
```

调整为等价但更细的判定：

```jsx
const isCodexOAuthMode =
  inputs.type === 57 && codexCredentialMode === 'oauth_json';
const batchAllowed = (!isEdit || isMultiKeyChannel) && !isCodexOAuthMode;
```

要求：

1. Codex `api_key` 模式下允许批量。
2. Codex `oauth_json` 模式下禁用批量。
3. 编辑态已有多 key 渠道行为保持不变。

- [ ] **Step 4: 在 Codex 表单区域增加模式切换 UI**

在 Codex 专用输入区前增加一个轻量切换器，例如：

```jsx
<Form.Select
  field='codex_credential_mode'
  label={t('凭据模式')}
  value={codexCredentialMode}
  optionList={[
    { label: 'API Key', value: 'api_key' },
    { label: 'OAuth JSON', value: 'oauth_json' },
  ]}
  onChange={(value) => {
    setCodexCredentialMode(value);
    if (value === 'oauth_json') {
      setBatch(false);
      setMultiToSingle(false);
      setMultiKeyMode('random');
    }
  }}
/>
```

说明：

1. 该字段仅用于 UI，不要求提交到后端。
2. 不需要把它塞进 `settings` 或 `setting`。

- [ ] **Step 5: 按模式拆分 Codex 输入 UI**

在现有 `inputs.type === 57` JSX 分支中：

1. `api_key` 模式：
   - 使用普通 `Form.Input` 或保留 `Form.TextArea`
   - 文案改为支持单个/多行 API Key
   - 保留 `batchExtra`
   - 不显示格式化 JSON 按钮
   - 不显示“刷新凭证”按钮
2. `oauth_json` 模式：
   - 保留当前 `Form.TextArea`
   - 保留授权、刷新、格式化按钮
   - 保留 JSON 结构说明
   - 不显示 `batchExtra`

注意：

1. 编辑态“查看密钥”按钮两种模式都保留。
2. 不改动 Codex 免责声明 Banner。

- [ ] **Step 6: 更新 `submit()` 中的 Codex 提交流程**

将当前 Codex 分支改成按 `codexCredentialMode` 走不同逻辑：

```jsx
if (localInputs.type === 57) {
  const rawKey = (localInputs.key || '').trim();
  if (!isEdit && rawKey === '') {
    showInfo(t('请输入密钥！'));
    return;
  }

  if (codexCredentialMode === 'oauth_json') {
    const credential = parseCodexCredential(rawKey);
    // 保留现有 invalid_json / invalid_json_object / invalid_oauth_fields 分支
    // 成功后 localInputs.key = JSON.stringify(credential.parsed)
  } else {
    // api_key 模式：不做 JSON 校验，也不再阻止 batch
  }
}
```

要求：

1. 删除“Codex 渠道不支持批量创建”的前端拦截。
2. `api_key` 模式只校验非空，不校验 JSON。
3. `oauth_json` 模式继续执行当前 JSON 结构校验。

- [ ] **Step 7: 清理临时 UI 字段，防止多余字段提交**

如果把 `codex_credential_mode` 放入了表单值，需要在提交前显式：

```jsx
delete localInputs.codex_credential_mode;
```

要求：

1. 不把前端临时字段传给后端。
2. 不污染 `channel` 数据模型。

- [ ] **Step 8: 运行前端静态检查**

Run: `cd web && bun run eslint`

Expected:
- `EditChannelModal.jsx` 无 ESLint 错误

- [ ] **Step 9: 运行前端构建**

Run: `cd web && bun run build`

Expected:
- 构建成功

- [ ] **Step 10: 提交前端改动**

```bash
git add web/src/components/table/channels/modals/EditChannelModal.jsx
git commit -m "feat: support codex apikey batch creation"
```

## Task 3: 联调与手工回归验证

**Files:**
- Modify: 无代码修改为目标
- Verify: [web/src/components/table/channels/modals/EditChannelModal.jsx](/home/team/huyuwen/project/new-api/web/src/components/table/channels/modals/EditChannelModal.jsx)
- Verify: [controller/channel.go](/home/team/huyuwen/project/new-api/controller/channel.go)

- [ ] **Step 1: 启动本地前后端所需服务**

根据项目常规方式启动：

```bash
go run main.go
cd web && bun run dev
```

如果本地已有开发环境脚本，优先使用项目既有方式。

- [ ] **Step 2: 手工验证新增 Codex + API Key 单渠道**

检查点：

1. 选择 `Codex` 后默认是 `API Key` 模式
2. 可见批量创建开关
3. 输入单个 API Key 可成功创建渠道

- [ ] **Step 3: 手工验证新增 Codex + API Key 批量创建**

输入示例：

```text
sk-codex-a
sk-codex-b
```

检查点：

1. 开启批量创建后可正常提交
2. 后端创建出两个渠道
3. 不出现“必须是合法 JSON”之类错误

- [ ] **Step 4: 手工验证新增 Codex + OAuth JSON**

输入示例：

```json
{
  "access_token": "token",
  "account_id": "acct_123"
}
```

检查点：

1. 切到 `OAuth JSON` 模式后批量能力关闭
2. 授权按钮、格式化按钮可见
3. 合法 JSON 可成功创建单渠道

- [ ] **Step 5: 手工验证 Codex OAuth JSON 错误提示**

输入缺字段 JSON：

```json
{
  "account_id": "acct_123"
}
```

检查点：

1. 前端阻止提交
2. 错误提示明确指出缺少 `access_token`

- [ ] **Step 6: 手工验证编辑态模式回填**

检查点：

1. 编辑已有 Codex API Key 渠道时，自动回到 `API Key` 模式
2. 编辑已有 Codex OAuth 渠道时，自动回到 `OAuth JSON` 模式

- [ ] **Step 7: 运行最终回归命令**

Run:

```bash
go test ./controller -count=1
cd web && bun run eslint
cd web && bun run build
```

Expected:

1. controller 包测试通过
2. 前端 lint 通过
3. 前端 build 通过

- [ ] **Step 8: 提交联调后的最终整理**

```bash
git status --short
```

确认只包含本需求相关变更后，再按团队习惯整理提交。

## 备注

1. 当前工作区已有 [web/src/components/table/channels/modals/EditChannelModal.jsx](/home/team/huyuwen/project/new-api/web/src/components/table/channels/modals/EditChannelModal.jsx) 的未提交改动，执行计划时必须先读清现有 diff，不要覆盖用户今天凌晨已做的 Codex/OpenAI 相关增强。
2. 后端业务代码中的 JSON 反序列化必须继续使用 `common.Unmarshal`，不要在新增逻辑中直接调用 `encoding/json`。
3. 如果在实现过程中发现 `EditChannelModal.jsx` 继续膨胀导致条件分支难以维护，可以在不改变行为的前提下做极小范围本地 helper 抽取，但不要展开与本需求无关的大重构。
