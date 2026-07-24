# Node 24 终端默认版本实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让新启动的交互式 zsh 默认使用现有 NVM Node `v24.15.0`，同时保留 Hermes 自带的 Node `v22.22.3`。

**Architecture:** 不修改 cc-connect 项目代码。复用现有 NVM `default -> v24.15.0`，在 `~/.zshrc` 最后一次 Node PATH 决策处选择默认版本，并将 `$NVM_BIN` 移到 zsh PATH 首位，避免硬编码 NVM 安装目录或改动 Hermes 管理文件。

**Tech Stack:** zsh、NVM、Node.js 24

---

## 文件与接口

- 修改：`/Users/liaochenliang/.zshrc`
- 项目代码、接口和依赖：无变更
- 自动化测试文件：不新增；此单行 shell 配置使用可执行登录 shell 检查

## 时间排期

- 基线检查、单行修改、验证：约 5 分钟

### Task 1: 切换 zsh 默认 Node

**Files:**
- Modify: `/Users/liaochenliang/.zshrc:81`
- Test: login shell smoke checks

- [ ] **Step 1: 运行失败检查，证明当前默认不是 Node 24**

```bash
zsh -lic 'actual=$(node -v); print "node=$actual"; [[ "$actual" == v24.* ]]'
```

Expected: 输出 `node=v22.22.3`，退出码为 `1`。

- [ ] **Step 2: 写入最小配置改动**

将：

```zsh
export PATH="/Users/liaochenliang/.hermes/node/bin:$PATH"
```

替换为：

```zsh
nvm use default --silent && path=("$NVM_BIN" ${path:#"$NVM_BIN"})
```

- [ ] **Step 3: 验证新的登录和非登录交互 shell 使用 Node 24**

```bash
env -i HOME="$HOME" USER="$USER" LOGNAME="$LOGNAME" SHELL=/bin/zsh TERM=xterm-256color PATH=/usr/bin:/bin:/usr/sbin:/sbin /bin/zsh -lic 'node -v; command -v node; npm -v; command -v npm'
env -i HOME="$HOME" USER="$USER" LOGNAME="$LOGNAME" SHELL=/bin/zsh TERM=xterm-256color PATH=/usr/bin:/bin:/usr/sbin:/sbin /bin/zsh -ic 'node -v; command -v node; npm -v; command -v npm'
```

Expected:

```text
v24.15.0
/Users/liaochenliang/.nvm/versions/node/v24.15.0/bin/node
11.12.1
/Users/liaochenliang/.nvm/versions/node/v24.15.0/bin/npm
```

- [ ] **Step 4: 验证 Hermes 运行时未被修改**

```bash
/Users/liaochenliang/.hermes/node/bin/node -v
```

Expected: 输出 `v22.22.3`。

- [ ] **Step 5: 检查目标配置行**

```bash
rg -n '^nvm use default --silent && path=' /Users/liaochenliang/.zshrc
```

Expected: 只输出新的 Node 默认版本选择配置行；`~/.zshrc` 位于仓库外，不创建实现提交。
