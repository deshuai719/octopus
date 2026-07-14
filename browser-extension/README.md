# Octopus 登录助手

这是 Octopus 的 Chrome / Edge Manifest V3 本机扩展。它只在用户发起账号恢复后工作：从 Octopus 管理页读取一次性恢复会话，临时申请目标站点 origin 权限，等待用户亲自完成登录，再按平台白名单提取候选凭据并交给 Octopus 服务端验证。

## 安装

```powershell
pnpm --dir browser-extension install
pnpm --dir browser-extension test
pnpm --dir browser-extension build
```

然后打开浏览器扩展管理页，启用“开发者模式”，选择“加载已解压的扩展”，目录使用 `browser-extension/dist`。Chrome 与 Edge 使用同一构建产物。

仓库提交的是固定公开 manifest key（不含私钥），对应扩展 ID 为 `hcnomejlhhefpnhljgcclhggoljokimn`。重复构建和重新加载解压扩展时 ID 保持一致。

## 使用

1. 在 Octopus 站点账号卡片中选择“使用扩展修复登录”。
2. 创建扩展会话，并保持恢复弹窗打开。
3. 点击浏览器工具栏中的“Octopus 登录助手”图标；扩展从当前页读取一次性会话。
4. 在 side panel 中点击“授权并打开登录页”，亲自完成密码、验证码、二次验证和 Cloudflare。
5. 点击“提取并交给 Octopus 验证”。
6. 返回 Octopus 查看掩码摘要和身份提示，确认后才会保存并同步。

NewAPI 兼容站点如果没有可读取的完整系统访问令牌，扩展只会提示前往个人设置 / 安全设置手动创建或显示令牌；不会自动创建，也不会循环尝试。

## 权限边界

- 没有永久 `host_permissions`，也不申请 `<all_urls>`。
- 目标站点只通过 `optional_host_permissions` 在用户点击后按精确 origin 临时授权。
- 不使用 `debugger`，不读取浏览器密码管理器或登录密码。
- AnyRouter 只读取目标域名名为 `session` 的 Cookie 和必要用户 ID，不持久化 Cloudflare/WAF Cookie。
- 恢复状态只放在 `chrome.storage.session`；候选提交成功、用户清理或会话过期后移除。
- 扩展不持有 Octopus 管理 JWT；候选提交依靠 10 分钟、一次性的恢复 capability。
