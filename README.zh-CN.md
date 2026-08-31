<div align="center">
  <img src="scripts/icon/appicon.png" alt="InvestGo icon" width="128" />
</div>

<h1 align="center">InvestGo</h1>

[English](./README.md) | [简体中文](./README.zh-CN.md) | [License](./LICENSE)

面向自选、持仓、组合分析、行情数据与价格提醒的轻量桌面投资工作台。

![Light](assets/light.png)

![Dark](assets/dark.png)

## 技术栈

- Go 1.24 与 Wails v3 alpha.54
- Vue 3、TypeScript、PrimeVue、Vite 和 Chart.js
- 多个行情数据 provider，以及用于汇率数据的 Frankfurter

## 快速开始

前置要求：Node.js 20+、pnpm 11+、Go 1.24+。macOS 构建需要 macOS 13+；Windows 构建需要 WebView2 Runtime。

```bash
pnpm install
pnpm dev
```

执行检查并构建前端：

```bash
pnpm typecheck
env GOCACHE=/tmp/go-build-cache go test ./...
pnpm build
```

## 打包与发布

推送和 Pull Request 会跑类型检查和 Go 测试。要出安装包时，打一个版本 tag：

```bash
git tag v0.3.0
git push origin v0.3.0
```

GitHub Actions 会构建 Apple Silicon DMG、Intel DMG 和 Windows `.exe`，并挂到对应的 GitHub Release 上。也可以在 Actions 页手动跑 **Release**、不打 tag，产物留在那次运行的 Artifacts 里。

若要在本地打包：

```bash
VERSION=1.0.0 ./scripts/package-darwin-aarch64.sh   # Apple Silicon
VERSION=1.0.0 ./scripts/package-darwin-x86_64.sh    # Intel
```

```powershell
.\scripts\build-windows-amd64.ps1
```

需要 Web Inspector 时，给 macOS 脚本加上 `--dev`。产物写入 `build/bin/`。Windows 当前只生成可运行的 `.exe`，暂未提供安装程序。

## 说明

- Wails v3 仍处于 alpha 阶段，API 和打包细节可能变化。
- 公开的 macOS 构建未签名。可信应用首次被 macOS 拦截时，先到“系统设置 > 隐私与安全性”选择“仍要打开”。如果仍然无法打开，可执行：

    ```bash
    xattr -dr com.apple.quarantine /Applications/InvestGo.app
    ```

    如果提示权限不足，使用 `sudo xattr -dr com.apple.quarantine /Applications/InvestGo.app`。只对你信任的应用执行此操作。

- 本项目主要用于个人使用和学习。行情数据可能延迟、不完整或不可用，本软件不构成投资建议。

## 许可证

[MIT License](./LICENSE)
