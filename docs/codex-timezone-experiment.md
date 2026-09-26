# Codex 时区改写实验

后台入口：系统设置 → 网关设置 → 网关转发 → Codex 时区改写（实验性）。

- `codex_timezone_rewrite_enabled`：默认 `false`。
- `codex_timezone_rewrite_target`：默认 `Asia/Singapore`，接受有效 IANA 时区（也可用 `UTC`）；拒绝 `Local` 和文件路径。
- 从管理后台保存即可生效，无需重启；配置存储在已有 settings 表，无新增数据库迁移。

## 作用范围

只作用于 OpenAI 账号的 Responses 请求（OAuth 与 API Key，包含透传中转账号）。覆盖 HTTP、WebSocket 上下文池、WebSocket 透传的首轮及后续 `response.create`、WS → HTTP 桥接路径。

在 `input` 中选择最后一个满足 `role=user`、`content[].type=input_text`、文本以 `<environment_context>` 开头且包含结束标签的块。只替换这个块里的首个 `<timezone>` 和完整的 `<current_date>YYYY-MM-DD</current_date>`。日期按目标时区计算；历史环境块、不可用日期标签、普通对话、工具输出、未知 JSON 字段保持不变。没有目标标签时不补造。

改写发生在既有请求体上：JSON 解码后的文本可识别 WS 桥接的 Unicode 转义；只通过 sjson 替换一个文本字段，不将整个会话解码成对象，也不新增整包缓冲的插件层。关闭时原字节切片直接返回。配置读取失败、非法时区或非法 JSON 均不执行改写。IANA tzdata 随服务端二进制内嵌。

此功能只用于对照实验，不修改模型、reasoning.effort、账号权限、代理出口，也不声称能改善模型能力。当前不包含出口 IP 查询、随机时区或 ModelTrace 探测。

## 验证

- 后端：`go test -tags=unit ./internal/service ./internal/handler/admin -run CodexTimezone -count=1`。
- 前端：SettingsView 与 localeKeyCompleteness 测试、vue-tsc、Vite build。
- 发布：先构建 frontend，再以 `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags embed` 构建服务；保留旧二进制以便回退。
