// ============================================================
// Tailscale 覆写脚本 (FlClash / Mihomo JS Override)
//
// 前提：
//   1. ts-proxy Android App 已启动，SOCKS5 监听 127.0.0.1:1080
//   2. FlClash 已启用
//
// 使用方法：
//   FlClash → 配置 → 覆写 → 添加覆写脚本 → 粘贴此内容 → 保存
//   然后断开代理重新连接
//
// 核心原理：
//   Mihomo 的 DNS 在收到域名请求时，会先解析域名再匹配规则。
//   如果没有 fake-ip 或 DNS filter，.ts.net 域名会被解析成公共 IP，
//   然后流量走了其他代理节点，永远到不了 ts-proxy。
//
//   解决方案：
//   1. rules 用 no-resolve 阻止预解析（IP-CIDR 已经用）
//   2. 添加 dns filter，让 .ts.net 域名走 fake-ip 模式
//   3. SOCKS5 节点内部自行做 DNS 解析（ts-proxy 会用 tailscale DNS）
// ============================================================

const main = (config) => {
  console.log("[tailscale] 覆写开始");

  config.proxies ??= [];
  config["proxy-groups"] ??= [];
  config.rules ??= [];
  config.dns ??= {};
  config.dns["fake-ip-filter"] ??= [];

  // === 1. 添加 ts-proxy SOCKS5 节点 ===
  // 去重后重新添加到最前面
  config.proxies = config.proxies.filter(p => p.name !== "tailscale");
  config.proxies.unshift({
    name: "tailscale",
    type: "socks5",
    server: "127.0.0.1",
    port: 1080,
    udp: true,
  });
  console.log("[tailscale] 节点: socks5://127.0.0.1:1080");

  // === 2. 添加 Tailscale 策略组 ===
  let tsGroup = config["proxy-groups"].find(g => g.name === "Tailscale");
  if (!tsGroup) {
    tsGroup = { name: "Tailscale", type: "select", proxies: ["tailscale", "DIRECT"] };
    config["proxy-groups"].push(tsGroup);
  } else if (!tsGroup.proxies.includes("tailscale")) {
    tsGroup.proxies.unshift("tailscale");
  }
  console.log("[tailscale] 策略组: Tailscale");

  // === 3. 规则：unshift 到最前面，确保优先级最高 ===
  const tsRules = [
    // DERP 中继（连接 tailscale 必需）
    "DOMAIN-SUFFIX,derp.tailscale.com,Tailscale",
    // Tailscale 域名（node1.ts.net 等）
    "DOMAIN-SUFFIX,ts.net,Tailscale",
    // Tailscale 网段（no-resolve 阻止 DNS 预解析）
    "IP-CIDR,100.64.0.0/10,Tailscale,no-resolve",
    "IP-CIDR,fd7a:115c:a1e0::/48,Tailscale,no-resolve",
  ];

  // 去重
  const existingSet = new Set(config.rules.map(r => r.trim()));
  const newRules = tsRules.filter(r => !existingSet.has(r));
  for (let i = newRules.length - 1; i >= 0; i--) {
    config.rules.unshift(newRules[i]);
  }
  console.log(`[tailscale] 规则: 新增${newRules.length}条, 总计${config.rules.length}条`);

  // === 4. DNS: 配置 fake-ip 和 filter ===
  // 让 ts.net 域名走 fake-ip，这样流量不会在 DNS 阶段被预解析
  // SOCKS5 节点（ts-proxy）内部会做 tailscale DNS 解析
  if (!config.dns.mode) {
    config.dns.mode = "fake-ip";
  }
  // 确保 fake-ip-filter 包含 ts.net
  if (!Array.isArray(config.dns["fake-ip-filter"])) {
    config.dns["fake-ip-filter"] = [];
  }
  const tsFilters = ["DOMAIN-SUFFIX,ts.net", "DOMAIN-SUFFIX,derp.tailscale.com"];
  for (const f of tsFilters) {
    if (!config.dns["fake-ip-filter"].some(e => e === f)) {
      config.dns["fake-ip-filter"].push(f);
    }
  }
  console.log("[tailscale] DNS fake-ip-filter 已配置");

  console.log("[tailscale] 覆写完成");
  return config;
};
