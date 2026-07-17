# Octopus 登录助手

这是 Octopus 的 Chrome / Edge Manifest V3 本机扩展。首次在 Octopus 管理员页面完成绑定后，可以在已登录的受支持中转站页面直接预览并确认创建或更新 Octopus 站点账号。

扩展不会在页面加载、标签切换或后台轮询时自动采集。读取、生成令牌和回传都必须由用户点击触发。

扩展采用便携 ZIP + Native Messaging 自动更新，不依赖 Chrome Web Store。更新检查与登录凭据流程完全隔离：更新助手不会读取或接收 Octopus JWT、Cookie、站点 Token 或浏览器登录数据。

## 安装

```powershell
pnpm --dir browser-extension install --frozen-lockfile
pnpm --dir browser-extension typecheck
pnpm --dir browser-extension test
pnpm --dir browser-extension build
```

构建机还需要 Go 1.25.x。`pnpm build` 会交叉编译 Windows/amd64 更新助手并将其放入 `browser-extension/dist`，因此最终 `dist` 可直接作为 Chrome/Edge 的已解压扩展目录。

打开浏览器扩展管理页，启用“开发者模式”，选择“加载已解压的扩展”，目录使用 `browser-extension/dist`。Chrome 与 Edge 使用同一构建产物。

仓库提交的是固定公开 manifest key（不含私钥），对应扩展 ID 为 `hcnomejlhhefpnhljgcclhggoljokimn`。重复构建和重新加载解压扩展时 ID 保持一致。

## 首次安装和后续更新

1. 从 GitHub 的 `extension-v<version>` Release 下载 `octopus-extension-<version>.zip`。
2. 解压到任意普通用户目录；Chrome 和 Edge 可以加载同一个目录，也可以分别加载不同目录。
3. 在 `chrome://extensions` 或 `edge://extensions` 启用开发者模式，选择“加载已解压的扩展程序”。
4. 后续打开侧边栏即可看到“扩展更新”。扩展最多每 24 小时后台检查一次，侧边栏检查结果缓存 6 小时；只提示，不会后台安装。
5. 第一次点击“下载更新助手”时，扩展会校验内置 updater，并保存为下载记录中的 `octopus-extension-helper.exe`。扩展不会代为运行；用户从浏览器下载记录中手动打开一次，完成后返回侧边栏点击“检测助手”。
6. helper 内嵌 `asInvoker` Windows application manifest，以当前用户权限安装到 `%LOCALAPPDATA%\Octopus\ExtensionUpdater`，并为 Chrome 与 Edge 注册 Native Messaging；不请求管理员权限、不写 HKLM。
7. 以后发现新版本时点击“立即更新”；助手会验证 Ed25519 签名和 SHA-256，备份、替换并回滚失败事务，成功后扩展自动重新加载。

首次初始化固定为“扩展只下载、用户手动运行、返回后手动检测”，不调用 `chrome.downloads.open()`，也不进行无限 Host 轮询。未购买 Windows 代码签名证书时，SmartScreen 仍可能显示“未知发布者”；这是文件信誉提示，不应出现管理员权限申请，更新清单签名也不能替代 Windows Authenticode 代码签名。

如果自动识别 unpacked 目录失败，点击“选择扩展目录”，在 Windows 文件窗口中选择目标目录里的 `manifest.json`。助手只接受固定 manifest key 和扩展 ID，不提供任意目录写入。

Chrome 与 Edge 加载同一目录时只替换一次；加载不同目录时会更新所有已验证目标。更新失败后可使用“回滚上一版本”。

## 发布扩展

扩展更新只接受同一仓库中 tag 为 `extension-v<version>` 的正式 GitHub Release，并忽略普通服务端 `v<version>` Release、draft 和 prerelease。固定资产为：

- `octopus-extension-update.json`
- `octopus-extension-update.json.sig`
- `octopus-extension-<version>.zip`
- `octopus-extension-updater-windows-amd64.exe`

发布清单使用独立 Ed25519 私钥签名。GitHub Actions secret 名为 `OCTOPUS_EXTENSION_SIGNING_KEY_BASE64`，内容是 PKCS#8 PEM 私钥文件的 base64；仓库和 Release 只包含公钥、签名与哈希，不包含私钥。

发布 tag 必须与 `browser-extension/manifest.json` 版本一致，例如 `extension-v0.3.1`。独立工作流 `.github/workflows/extension-release.yml` 会运行 Go/TypeScript 测试、构建、签名和资产上传，并设置 `make_latest=false`。

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
6. 完成后可以直接切换到下一站，或在同站点击“继续导入或重新读取”；不需要返回 Octopus 或清空扩展数据。

首版支持 NewAPI、OneAPI、OneHub、DoneHub、Sub2API 与 AnyRouter。普通 API Key 站点和无法可靠确定具体变种的站点继续手动添加。

NewAPI 兼容族已确认登录但没有完整系统访问令牌时，可选择：

- 再次点击“生成系统令牌并继续”；该操作可能覆盖旧系统令牌，同一捕获最多执行一次；
- 在临时输入框粘贴该平台推荐的完整访问令牌；输入只存在于当前 side panel，会在提交、失败、取消或关闭后清空。

## 权限与敏感信息边界

- Manifest 没有永久 `host_permissions`、`<all_urls>` 或 `debugger`。
- Octopus 只保留唯一、已验证的精确 HTTPS origin 权限；中转站权限按 origin 临时申请，并在完成、取消、失败或超时后撤销。
- 管理员 JWT 只保存在 `chrome.storage.local`，只允许发送到绑定 origin 的 `/api/v1/user/status` 和 `/api/v1/site/direct-capture/*`。
- 管理员 JWT、候选 Token、Cookie 和密码不得进入 URL、日志、通知、诊断、测试快照或 `chrome.storage.sync`。
- `chrome.storage.session` 只保存非敏感捕获索引、阶段、到期时间和单次生成锁，不保存原始候选。
- AnyRouter 只读取目标域名名为 `session` 的 Cookie 和必要用户 ID，不持久化 Cloudflare/WAF Cookie。
- 本地只保留最近 20 条、最长 7 天的脱敏错误；side panel 可复制或清空诊断。

## 故障排查

- “请先升级 Octopus”：后端未声明直接捕获协议版本，需要先升级 Octopus 后端。
- “无法确认具体平台”：当前只有品牌或兼容家族弱证据，本次不会读取或上传凭据。
- `401` / JWT 过期：回到已登录 Octopus 页面重新点击扩展刷新绑定。
- “站点 origin 冲突”：Octopus 中已有相同规范化 origin、但平台不兼容，必须人工处理，扩展不会覆盖。
- 同步失败：账号已经保存，可使用“重试同步”，无需再次生成或提交令牌。

复制诊断只包含时间、`operation_id`、错误码、阶段、HTTP 状态、origin、平台、可重试标记和脱敏摘要。
