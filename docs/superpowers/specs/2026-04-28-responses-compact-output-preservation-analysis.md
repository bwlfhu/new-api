# Responses Compact 输出字段丢失问题分析与修复说明

## 背景

本仓库在 Codex / OpenAI Responses 兼容改造中，为 `/v1/responses/compact` 增加了对流式上游响应的聚合处理：当上游返回 SSE 时，网关读取 `response.completed` 事件，将其中的最终 `response` 重新组装成普通 JSON 响应返回给客户端。

OpenAI 官方文档对 `/v1/responses/compact` 的返回契约是：接口返回一个 compacted response object，`output` 数组中可以包含 compaction item；该 item 的关键字段包括 `type: "compaction"` 和 `encrypted_content`。Responses 流式接口的官方示例也显示，流式响应最终会通过 `response.completed` 事件携带完整 `response` 对象。

参考：

- OpenAI API Reference: [Compact a response](https://developers.openai.com/api/reference/resources/responses/methods/compact)
- OpenAI API Reference: [Create a model response - streaming](https://developers.openai.com/api/reference/resources/responses/methods/create)

## 问题现象

当 `/v1/responses/compact` 的上游响应为 SSE，并且最终 `response.completed` 中的 `response.output` 包含 compact 专用输出项时，网关返回给客户端的 JSON 可能丢失 `encrypted_content` 等 compact 专用字段。

典型输入事件：

```text
event: response.completed
data: {"type":"response.completed","response":{"id":"resp_123","object":"response","created_at":1774243072,"output":[{"type":"compaction_summary","encrypted_content":"encrypted-summary"}],"usage":{"input_tokens":12,"output_tokens":6,"total_tokens":18}}}
```

期望：客户端收到的 `output` 中保留 `encrypted_content`。

风险：如果字段被丢弃，客户端无法把 compact 结果继续作为后续 Responses 请求的上下文输入，长上下文压缩链路会在网关层被破坏。

## 根因分析

问题只发生在流式聚合路径，非流式路径会直接透传上游原始 body。

原流式聚合逻辑使用 `dto.ResponsesStreamResponse` 解码 `response.completed` 事件。该结构中的 `Response` 类型是通用 `OpenAIResponsesResponse`，其 `Output` 字段是 `[]ResponsesOutput`。

`ResponsesOutput` 当前覆盖的是普通 Responses 输出项，例如 message、tool call、image generation 等，但不包含 compact 专用字段 `encrypted_content`。JSON 解码到该结构后，Go 会忽略结构体中没有声明的字段；随后再次 marshal 返回客户端时，未声明字段已经不可恢复。

也就是说，问题不是上游未返回 compact 内容，而是网关在流式聚合过程中把 compact 输出项过早结构化成了不完整 DTO。

## 官方修复方案

从 OpenAI 官方契约看，正确处理方式应当满足两点：

1. `/v1/responses/compact` 的 `output` 必须支持 compact item，并保留 `encrypted_content`。
2. 对流式 Responses，消费者应以最终 `response.completed` 事件中的完整 `response` 为最终结果来源。

因此官方语义下的修复方向是：compact 响应的 `output` 不能使用缺少 compact item 字段的普通 Responses 输出结构做有损解码。可选实现有两类：

1. 为 compact item 补齐正式 DTO，确保 `type: "compaction"` 和 `encrypted_content` 被结构化保留。
2. 在网关聚合层把 compact `output` 作为原始 JSON 片段保留，避免网关因 SDK/DTO 滞后而丢弃官方或新模型新增的输出字段。

对于 API 网关来说，第二种方案更稳妥：网关不需要理解 compact 输出项的内部业务含义，只需要保证上游返回的合法字段被完整转发。

## 我们的修复方案

本仓库采用最小改动的透传保真方案，只调整 `/v1/responses/compact` 的流式聚合解码结构：

1. 保留现有非流式路径不变，继续直接透传原始 body。
2. 流式路径不再把 `response.completed.response` 解码为通用 `OpenAIResponsesResponse`。
3. 为 compact 流式聚合定义局部 envelope 结构，仅结构化读取必要字段：
   - `id`
   - `object`
   - `created_at`
   - `usage`
   - `error`
4. 将 `response.output` 解码为 `json.RawMessage`，并原样写入 `OpenAIResponsesCompactionResponse.Output`。
5. 错误处理继续使用 `dto.GetOpenAIError()` 从动态 `error` 字段中提取 OpenAI 错误。
6. 增加回归测试，覆盖 compact 输出项携带 `encrypted_content` 时，网关响应仍保留该字段。

相关文件：

- `relay/channel/openai/relay_responses_compact.go`
- `relay/channel/openai/relay_responses_compact_test.go`

## 方案取舍

选择 `json.RawMessage` 而不是补齐所有 compact 输出 DTO，原因如下：

1. compact 输出项是上游 API 契约的一部分，网关当前没有消费其内部字段的业务需求。
2. Responses API 的输出项类型仍在扩展，网关使用不完整 DTO 容易在后续新增字段时再次发生有损转发。
3. RawMessage 方案改动范围小，只影响 compact 流式聚合路径，不改变普通 `/v1/responses` 流式处理逻辑。
4. 该方案同时兼容官方 `type: "compaction"` 和已观测到的兼容变体字段形态。

## 验证方式

目标测试：

```bash
go test ./relay/channel/openai -run 'TestOaiResponsesCompactionHandler_(AggregatesResponsesStreamBody|PreservesCompactionSummaryEncryptedContent|ReturnsStreamErrorBeforeWrite)' -count=1
```

验证要点：

1. 普通 compact 流式响应仍能聚合为 JSON。
2. `usage` 统计仍能正确返回给计费逻辑。
3. `response.error` / `response.failed` 仍在写响应前转换成网关错误。
4. compact 输出项中的 `encrypted_content` 不再丢失。

## 后续注意事项

如果以后需要在网关层读取 compact 输出内容，再考虑新增完整 compact item DTO；在没有明确业务消费需求前，应继续保持 compact `output` 原样透传，避免网关 DTO 滞后造成字段丢失。
