# Octopus 登录助手

这是 Octopus 的 Chrome / Edge Manifest V3 本机扩展，支持两条互不替代的流程：

- 从 Octopus 为既有账号发起一次性登录恢复；
- 在已登录的受支持中转站页面点击扩展，直接预览并确认创建或更新 Octopus 站点账号。

扩展不会在页面加载、标签切换或后台轮询时自动采集。读取、生成令牌和回传都必须由用户点击触发。

## 安装

```powershell
pnpm --dir browser-extension install --frozen-lockfile
pnpm --dir browser-extension typecheck
pnpm --dir browser-extension test
pnpm --dir browser-extension build
```

打开浏览器扩展管理页，启用“开发者模式”，选择“加载已解压的扩展”，目录使用 `browser-extension/dist`。Chrome 与 Edge 使用同一构建产物。

仓库提交的是固定公开 manifest key（不含私钥），对应扩展 ID 为 `hcnomejlhhefpnhljgcclhggoljokimn`。重复构建和重新加载解压扩展时 ID 保持一致。

## 首次初始化

1. 使用管理员 JWT 登录唯一的 Octopus 实例。
2. 确认 Octopus 使用有效 HTTPS 证书，然后在该页面点击扩展图标。
3. 扩展从精确的 `auth-storage` 结构读取管理员 JWT 和到期时间，并通过 `GET /api/v1/user/status` 验证管理员身份与直接捕获协议版本。
4. 验证成功后，扩展在 `chrome.storage.local` 保存唯一 Octopus origin 和完整管理员 JWT，并持续保留该精确 origin 的主机权限。

扩展拒绝 HTTP Octopus、API Key 登录、过期 JWT、跨站重定向和缺少 `X-Octopus-Direct-Capture-Version: 1` 的旧后端。检测到另一个 Octopus origin 时必须在 side panel 明确确认，不能静默换绑。

## 从中转站直接创建或更新

1. 打开已登录的受支持中转站并点击扩展图标。
2. 扩展先验证 Octopus 绑定，再为当前精确 origin 申请临时权限。
3. 浏览器和 Octopus 服务端分别验证平台结构、登录身份和候选凭据；标题、Logo、`system_name` 或域名关键词不能单独决定平台。
4. side panel 展示站点、账号、平台用户 ID、脱敏凭据摘要和创建/更新动作。
5. 只有点击“确认创建或更新”后才会写入数据库。保存成功与完整同步结果分别显示；同步失败可以单独重试，不会再次覆盖凭据。

首版支持 NewAPI、OneAPI、OneHub、DoneHub、Sub2API 与 AnyRouter。普通 API Key 站点和无法可靠确定具体变种的站点继续手动添加。

NewAPI 兼容族已确认登录但没有完整系统访问令牌时，可选择：

- 再次点击“生成系统令牌并继续”；该操作可能覆盖旧系统令牌，同一捕获最多执行一次；
- 在临时输入框粘贴该平台推荐的完整访问令牌；输入只存在于当前 side panel，会在提交、失败、取消或关闭后清空。

## 既有账号恢复

1. 在 Octopus 站点账号卡片中选择“使用扩展修复登录”。
2. 创建扩展会话，并保持恢复弹窗打开。
3. 点击扩展图标读取一次性恢复包。
4. 在 side panel 中授权并打开目标站点，亲自完成密码、验证码、二次验证和 Cloudflare。
5. 点击提取并验证；返回 Octopus 核对掩码摘要后确认保存。

这条流程仍使用 10 分钟、一次性的 recovery capability，不改为管理员 JWT，也不与直接捕获候选会话混用。

## 权限与敏感信息边界

- Manifest 没有永久 `host_permissions`、`<all_urls>` 或 `debugger`。
- Octopus 只保留唯一、已验证的精确 HTTPS origin 权限；中转站权限按 origin 临时申请，并在完成、取消、失败或超时后撤销。
- 管理员 JWT 只保存在 `chrome.storage.local`，只允许发送到绑定 origin 的 `/api/v1/user/status` 和 `/api/v1/site/direct-capture/*`。
- 管理员 JWT、候选 Token、Cookie 和密码不得进入 URL、日志、通知、诊断、测试快照或 `chrome.storage.sync`。
- `chrome.storage.session` 只保存非敏感捕获索引、阶段、到期时间和单次生成锁，不保存原始候选。
- AnyRouter 只读取目标域名名为 `session` 的 Cookie 和必要用户 ID，不持久化 Cloudflare/WAF Cookie。
- 本地只保留最近 20 条、最长 7 天的脱敏错误；side panel 可复制或清空诊断。

## 故障排查

- “请先升级 Octopus”：后端未声明直接捕获协议版本，先升级后端，旧恢复流程仍可用。
- “无法确认具体平台”：当前只有品牌或兼容家族弱证据，本次不会读取或上传凭据。
- `401` / JWT 过期：回到已登录 Octopus 页面重新点击扩展刷新绑定。
- “站点 origin 冲突”：Octopus 中已有相同规范化 origin、但平台不兼容，必须人工处理，扩展不会覆盖。
- 同步失败：账号已经保存，可使用“重试同步”，无需再次生成或提交令牌。

复制诊断只包含时间、`operation_id`、错误码、阶段、HTTP 状态、origin、平台、可重试标记和脱敏摘要。
