# ts-proxy-Android

在 Android 上运行 Tailscale userspace 代理，让 Clash 和 Tailscale 同时运行。

## 为什么需要这个

Android 只能同时跑一个 VPN。Tailscale 占了 VPN slot，Clash 就没法用 TUN 模式。

本项目：Tailscale 用 userspace 模式跑，输出本地 SOCKS5 代理（127.0.0.1:1080），不占 VPN slot。VPN slot 留给 Clash。覆写脚本把 Tailscale 流量路由到 ts-proxy。

## 安装

1. 从 [Releases](../../releases) 下载最新 APK
2. 打开 App，点「启动」，浏览器完成 Tailscale 授权
3. 在 FlClash 添加覆写脚本（见下方）

## FlClash 覆写脚本

工具 → 进阶配置 → 脚本 → 添加 → 粘贴：

```js
const main = (config) => {
  config.proxies = (config.proxies || []).filter(p => p.name !== "tailscale");
  config.proxies.unshift({
    name: "tailscale", type: "socks5",
    server: "127.0.0.1", port: 1080, udp: true,
  });

  let g = (config["proxy-groups"] || []).find(g => g.name === "Tailscale");
  if (!g) {
    g = { name: "Tailscale", type: "select", proxies: ["tailscale", "DIRECT"] };
    config["proxy-groups"].push(g);
  } else if (!g.proxies.includes("tailscale")) {
    g.proxies.unshift("tailscale");
  }

  const rules = [
    "DOMAIN-SUFFIX,derp.tailscale.com,Tailscale",
    "DOMAIN-SUFFIX,ts.net,Tailscale",
    "IP-CIDR,100.64.0.0/10,Tailscale,no-resolve",
    "IP-CIDR,fd7a:115c:a1e0::/48,Tailscale,no-resolve",
  ];
  const existing = new Set(config.rules.map(r => r.trim()));
  for (const r of rules.filter(r => !existing.has(r)).reverse()) {
    config.rules.unshift(r);
  }

  config.dns ??= {};
  config.dns["fake-ip-filter"] ??= [];
  for (const f of ["DOMAIN-SUFFIX,ts.net", "DOMAIN-SUFFIX,derp.tailscale.com"]) {
    if (!config.dns["fake-ip-filter"].includes(f)) {
      config.dns["fake-ip-filter"].push(f);
    }
  }

  return config;
};
```

然后：配置 → 右上角菜单 → 更多 → 覆写 → 脚本 → 选择此脚本

## 工作原理

```
App → Clash TUN (全局代理)
        └→ ts-proxy (127.0.0.1:1080, SOCKS5)
              └→ Tailscale 网络 (WireGuard)
```

## 开发

### 从源码编译

```bash
# 需要 Go 1.24+, Android NDK, gomobile
go install golang.org/x/mobile/cmd/gomobile@latest
go install golang.org/x/mobile/cmd/gobind@latest

git clone https://github.com/0xKrito/tailscale-socks5-Android.git
cd tailscale-socks5-Android
go get golang.org/x/mobile/bind@latest
go mod tidy
gomobile init
gomobile bind -ldflags="-checklinkname=0 -s -w" -target=android -androidapi=26 -o android-app/app/libs/tsproxy.aar ./mobile/

# 编译 APK
cd android-app && ./gradlew assembleDebug
```

### GitHub Actions

CI 自动编译 AAR + APK。触发方式：push tag `v*` 或手动 dispatch。

### Tailscale Android 补丁

CI 编译时自动打以下补丁：
- netmon/state.go: `net.Interfaces()` → `anet.Interfaces()` (修复 Android SDK≥30 netlink 权限)
- tsnet/tsnet.go: 添加 `os.Executable()` Android fallback
- ipn/localapi: 启用 ACME 证书功能

## 致谢

- [ts-proxy](https://github.com/ge9/ts-proxy) by ge9
- [txthinking/socks5](https://github.com/txthinking/socks5)
- [wlynxg/anet](https://github.com/wlynxg/anet) (Android net 接口修复)
- [tailscale.com](https://github.com/tailscale/tailscale)

## License

见 [LICENSE](LICENSE) 文件。Android App 和修改部分为 0xKrito 原创。
