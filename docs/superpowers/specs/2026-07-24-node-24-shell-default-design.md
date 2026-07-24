# Node 24 终端默认版本设计

## 目标

让新启动的交互式 zsh 默认使用已安装的 NVM Node `v24.15.0`，不修改 Hermes 自带的 Node `v22.22.3`。

## 根因

NVM 的 `default` 别名已指向 Node 24，但 `~/.zshrc` 后续将 `~/.hermes/node/bin` 放到 `PATH` 首位，覆盖了 NVM 的选择。

## 实现

将 `~/.zshrc` 中最后覆盖 Node 选择的 Hermes PATH 设置替换为 `nvm use default --silent && path=("$NVM_BIN" ${path:#"$NVM_BIN"})`。复用现有 NVM 默认版本，并将 NVM 已选择的 bin 目录移到 zsh PATH 首位；不硬编码 Node 安装目录，也不修改 Hermes 管理的文件或软链接。

## 验证

启动新的登录式和非登录式交互 shell，确认：

- `node -v` 输出 `v24.15.0`。
- `command -v node` 指向 `~/.nvm/versions/node/v24.15.0/bin/node`。
- `npm` 来自同一 NVM Node 24 安装目录。
- `~/.hermes/node/bin/node -v` 仍输出 `v22.22.3`。
