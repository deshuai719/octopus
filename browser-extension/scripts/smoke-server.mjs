import http from "node:http";

const apiPort = 4177;
const sitePort = 4178;
const extensionOrigin = "chrome-extension://hcnomejlhhefpnhljgcclhggoljokimn";

let candidateResult = { received: false };

function listen(port, handler) {
  const server = http.createServer(handler);
  server.listen(port, "127.0.0.1", () => {
    process.stdout.write(`smoke server ready on http://127.0.0.1:${port}\n`);
  });
  return server;
}

function json(response, status, payload, headers = {}) {
  response.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
    "Cache-Control": "no-store",
    ...headers,
  });
  response.end(JSON.stringify(payload));
}

const apiServer = listen(apiPort, (request, response) => {
  const corsHeaders = {
    "Access-Control-Allow-Origin": extensionOrigin,
    "Access-Control-Allow-Headers": "Content-Type, X-Octopus-Recovery-Capability",
    "Access-Control-Allow-Methods": "GET, POST, OPTIONS",
  };
  if (request.method === "OPTIONS") {
    response.writeHead(204, corsHeaders);
    response.end();
    return;
  }
  if (request.method === "GET" && request.url === "/result") {
    json(response, 200, candidateResult, corsHeaders);
    return;
  }
  if (request.method === "GET" && request.url === "/octopus") {
    const packet = {
      version: 1,
      api_base_url: `http://127.0.0.1:${apiPort}`,
      session_id: "smoke-session-0001",
      capability: "smoke-capability-not-a-secret-0001",
      account_id: 1,
      site_id: 2,
      origin: `http://127.0.0.1:${sitePort}`,
      platform: "new-api",
      expires_at: new Date(Date.now() + 10 * 60 * 1000).toISOString(),
      auth: {
        compatible_family: "new-api",
        required_fields: ["access_token", "platform_user_id"],
        extractable_fields: ["access_token", "platform_user_id"],
        recovery_guide: {
          title: "本地 NewAPI 模拟恢复",
          steps: ["授权回环站点", "打开模拟登录页", "提取合成候选凭据"],
          manual_fallback: "本地冒烟不需要真实账号或凭据。",
        },
      },
    };
    response.writeHead(200, {
      "Content-Type": "text/html; charset=utf-8",
      "Cache-Control": "no-store",
    });
    response.end(`<!doctype html>
<html lang="zh-CN">
  <head><meta charset="utf-8"><title>Octopus Extension Smoke</title></head>
  <body>
    <main><h1>Octopus 扩展本地冒烟</h1><p>点击 Chrome 工具栏中的“Octopus 登录助手”开始。</p></main>
    <script type="application/json" data-octopus-recovery="true">${JSON.stringify(packet).replaceAll("<", "\\u003c")}</script>
  </body>
</html>`);
    return;
  }
  if (request.method === "POST" && request.url === "/api/v1/site/auth-recovery/smoke-session-0001/candidate") {
    let body = "";
    request.setEncoding("utf8");
    request.on("data", (chunk) => { body += chunk; });
    request.on("end", () => {
      const payload = JSON.parse(body);
      candidateResult = {
        received: true,
        account_id: payload.account_id,
        origin: payload.origin,
        platform: payload.platform,
        platform_user_id: payload.platform_user_id,
        has_access_token: typeof payload.access_token === "string" && payload.access_token.length >= 16,
        capability_present: Boolean(request.headers["x-octopus-recovery-capability"]),
      };
      json(response, 200, { code: 200, message: "success", data: { phase: "candidate_ready" } }, corsHeaders);
    });
    return;
  }
  json(response, 404, { code: 404, message: "not found" }, corsHeaders);
});

const siteServer = listen(sitePort, (request, response) => {
  if (request.url === "/api/user/self") {
    json(response, 200, { success: true, data: { id: 42, username: "smoke-user" } });
    return;
  }
  response.writeHead(200, {
    "Content-Type": "text/html; charset=utf-8",
    "Cache-Control": "no-store",
  });
  response.end(`<!doctype html>
<html lang="zh-CN">
  <head><meta charset="utf-8"><title>NewAPI Smoke Site</title></head>
  <body>
    <main><h1>NewAPI 模拟登录完成</h1><p>此页面仅用于 Octopus 扩展本地冒烟。</p></main>
    <script>localStorage.setItem("system_access_token", "octopus-smoke-not-a-secret-0001");</script>
  </body>
</html>`);
});

function shutdown() {
  apiServer.close();
  siteServer.close();
}

process.on("SIGINT", shutdown);
process.on("SIGTERM", shutdown);
