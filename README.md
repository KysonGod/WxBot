# WxBot

一个可独立运行的微信聊天机器人（Go 实现，微信操作走 `wxauto` 生态）。

## 1. 你能用它做什么

- 指定聊天对象并自动回复
- 支持多对象
- 长回复会自动拆成多条发送
- 支持图片/表情包
- 可联网

## 2. 运行前准备

仅支持 Windows（微信 PC 客户端）。

1. 打开并登录微信 PC 客户端
2. 安装 Python（建议 3.10+）
3. 安装微信自动化依赖：

```powershell
pip install wxauto
```

## 3. 启动方式

1. PowerShell 运行

```powershell
cd WxBot
go run .\cmd\bot
```

2. 运行已构建的 EXE（或直接点击dist下的exe文件）：

```powershell
cd WxBot
.\dist\WxBot.exe
```

说明：
- `bot` 默认会开启本地热重载通知端点 `http://127.0.0.1:19091/reload`（仅本机回环可访问）。
- 启动后会显示显眼的 UI 配置入口提示（`WxBot.exe -open-config-ui` / `WxBot.exe -config-mode ui` / UI 地址）。

## 4. 启动时配置检查

`bot` 会直接尝试启动。  
在进入运行态前会校验必要配置；如果配置不完整，会提示错误并退出（交互终端下会提示“按任意键退出”）。

当检测到配置不完善时，程序会自动尝试打开前端配置界面（默认 `http://127.0.0.1:19090`）。  
终端会同时提示你也可以直接编辑 `config.json` / `config.local.json`；配置完成后请重启程序。

当配置完整并进入运行态后，程序会默认保持配置界面地址可访问（`http://127.0.0.1:19090`），你可随时手动打开修改。

为避免双击窗口闪退，启动阶段遇到致命错误时会默认等待“按任意键退出”。  
若你在脚本中运行且不希望等待，可设置：

```powershell
$env:WECHATBOT_NO_PAUSE="1"
```

配置可通过以下三种方式维护：

- 配置文件（编辑 `config.json + config.local.json`）
- 命令行向导（`-config-mode cli`）
- UI 界面（`WxBot.exe -open-config-ui` 或 `-config-mode ui`）

说明（必要配置不完整时会拒绝启动）：

- 主模型必须完整（`base_url/api_key/model`）
- `listen_list` 必须至少有一个对象，且 `prompt` 文件存在
- 校验失败不会启动 bot，会提示你继续修正

### 4.1 配置文件方式

复制并填写本地敏感配置文件：

```powershell
cd WxBot
Copy-Item .\config.local.json.example .\config.local.json
```

示例：

```json
{
  "llm": { "api_key": "你的主模型Key" },
  "assistant_llm": { "api_key": "你的助手模型Key" },
  "vision_llm": { "api_key": "你的视觉模型Key" },
  "online_llm": { "api_key": "你的联网模型Key" }
}
```

也支持环境变量引用（示例）：

```json
{
  "llm": { "api_key": "env:MAIN_LLM_API_KEY" }
}
```

### 4.2 命令行向导方式（四步）

四步依次配置：

1. 主模型（必填）
2. 助手模型（可跳过）
3. 视觉模型（可跳过）
4. 联网模型（可跳过）

每步最少需要：

- `base_url`
- `api_key`
- `model`

并可选填写 `provider`（不填会自动推断）。

模型是否可跳过：

- 主模型：不能跳过
- 助手模型：可跳过（将复用主模型）
- 视觉模型：可跳过（图片/表情识别不可用）
- 联网模型：可跳过（联网检索不可用）

后续补充配置，停止当前应用：

```powershell
cd WxBot
go run .\cmd\bot -setup-models
```

或：

```powershell
cd WxBot
.\dist\WxBot.exe -setup-models
```

### 4.3 UI 配置方式

可直接启动配置界面：

```powershell
cd WxBot
.\dist\WxBot.exe -open-config-ui
```

或通过 `bot` 强制走 UI 配置：

```powershell
cd WxBot
.\dist\WxBot.exe -config-mode ui
```

默认地址：`http://127.0.0.1:19090`

## 5. 如何设置聊天对象（重点）

这里是最关键配置。机器人是否会对指定对象回复，取决于 `listen_list` 和提示词文件是否一一对应。

### 5.1 正确步骤

1. 在微信里确认聊天对象名称（必须精确一致）
2. 准备提示词文件（推荐两种方式）：
   - 直接使用 `prompts/default.md`（快速跑通）
   - 复制 `prompts/template_full.md` 为新文件并按模板填写（推荐长期使用）
3. 在 `config.json` 的 `listen_list` 中绑定对象与提示词
4. 若只改 `prompt` 内容，通常无需重启；若改配置文件，保存后会自动热重载
5. 在日志中确认：
   - `listen chat added: 对象名`
   - 收到消息后有 `[flow][chat=对象名] recv ...`

### 5.2 `listen_list` 绑定规则（务必看）

- `nickname`：微信会话名称，必须和微信里显示一致
- `prompt`：提示词文件名，不带 `.md`

示例（使用 `default.md`）：

```json
"listen_list": [
  { "nickname": "Zachary", "prompt": "default" }
]
```

示例（使用模板新建文件）：

```json
"listen_list": [
  { "nickname": "Zachary", "prompt": "zachary_companion" }
]
```

上面配置要求文件存在：`prompts/zachary_companion.md`

### 5.3 用模板 `template_full.md` 的推荐方式

1. 复制模板新建文件：

```powershell
Copy-Item .\prompts\template_full.md .\prompts\zachary_companion.md
```

2. 打开新文件，优先填写第 0 节“角色配置”
3. 将方括号占位符替换为你的实际内容
4. 不需要的模块可以删掉，保持提示词简洁
5. 回到 `config.json`，把 `prompt` 填成 `zachary_companion`

### 5.4 多对象配置示例

```json
"listen_list": [
  { "nickname": "Zachary", "prompt": "default" },
  { "nickname": "DJ", "prompt": "dj" },
  { "nickname": "项目群", "prompt": "default_work" }
]
```

对应文件必须存在：

- `prompts/default.md`
- `prompts/dj.md`
- `prompts/default_work.md`

只要缺一个，程序启动会失败。

### 5.5 常见错误（高频）

1. `nickname` 不匹配（多空格、符号、备注名差异）
2. `prompt` 写成了 `xxx.md`（错误，应写 `xxx`）
3. `prompt` 对应文件不存在
4. 修改后未重启程序
5. 目标对象不在 `listen_list` 里（不会回复）

## 6. 前端配置界面（推荐）

项目已内置本地配置 Web 界面，参考原项目配置编辑器流程做了精简版。

### 6.1 启动界面

```powershell
cd WxBot
.\dist\WxBot.exe -open-config-ui
```

浏览器打开：

`http://127.0.0.1:19090`

说明：从 `dist` 目录启动 `WxBot.exe` 时，程序会自动回溯到项目根目录读取 `config.json`。
保存配置时，界面会主动通知运行中的 `bot` 立即热重载；若通知失败，也有轮询兜底（约 1 秒内生效）。

可选参数（仍然只有一个入口 `WxBot.exe`）：

```powershell
.\dist\WxBot.exe -open-config-ui -config-ui-addr 127.0.0.1:19090
```

如果你修改了 bot 侧主动热重载地址，可加：

```powershell
.\dist\WxBot.exe -open-config-ui -reload-listen 127.0.0.1:19091
```

### 6.2 界面里该怎么配（正确顺序）

1. 先到“Prompt 文件管理”
   - 新手：直接用 `default`
   - 长期使用：基于 `template_full` 创建新 Prompt（例如 `zachary_companion`）
2. 再到“聊天对象配置（重点）”
   - `nickname` 填微信会话名（精确匹配）
   - `prompt` 选择刚才创建/已有的 Prompt 文件名（不带 `.md`）
3. 再到“模型配置”
   - 主模型必须完整
   - 视觉/联网模型按需配置
4. 点击“保存配置”
   - 非敏感配置写入 `config.json`
   - API Key 写入 `config.local.json`
5. 保存后会自动通知 bot 热重载（一般无需手动重启）

### 6.3 聊天对象与模板的配法（界面内）

- 想快速测试：对象直接绑 `default`
- 想精细人设：用 `template_full` 创建新 Prompt，再绑定到对象
- 多对象时每个对象可用不同 Prompt

### 6.4 常见误区

1. 把 `prompt` 写成 `xxx.md`（应写 `xxx`）
2. 聊天对象昵称和微信实际会话名不一致
3. 保存后看日志，确认出现 `config change detected`（表示已热重载）

## 7. 表情包目录（可选）

默认目录：`emojis/`

按分类建子目录，每个分类放对应表情包/图片 ：

```text
emojis/
  开心/
    1.gif
    2.png
  无语/
    a.jpg
```

## 8. 常用命令

源码运行：

```powershell
go run .\cmd\bot
```

强制使用配置文件模式：

```powershell
go run .\cmd\bot -config-mode file
```

强制使用命令行向导模式：

```powershell
go run .\cmd\bot -config-mode cli
```

强制使用 UI 配置模式：

```powershell
go run .\cmd\bot -config-mode ui
```

启动时自动打开配置页面（会尝试启动内置UI服务再打开浏览器）：

```powershell
go run .\cmd\bot -open-config-ui
```

修改 `bot` 启动提示中的配置地址（默认 `127.0.0.1:19090`）：

```powershell
go run .\cmd\bot -config-ui-addr 127.0.0.1:19090
```

设置主动热重载通知监听地址（默认 `127.0.0.1:19091`）：

```powershell
go run .\cmd\bot -reload-listen 127.0.0.1:19091
```

仅打开模型配置向导并退出：

```powershell
go run .\cmd\bot -setup-models
```

构建 EXE：

```powershell
go build -o .\dist\WxBot.exe .\cmd\bot
```

一键按规范名称构建（推荐）：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1
```

## 9. 常见问题

1. 报错 `main model config is incomplete`
   - 主模型缺少 `base_url/api_key/model`
   - 先运行 `-setup-models` 补配，或检查 `config.local.json`

2. 报错 `Invalid Authentication` / `401`
   - API Key 无效，或 Key 与 `base_url/model` 不匹配

3. 微信初始化失败
   - 确认微信已登录并在前台可访问
   - 确认 Python 环境已安装 `wxauto`

4. 报错 `go-build ... Access is denied`
   - 先在当前终端设置项目本地缓存再执行 Go 命令：
   - `$env:GOCACHE="$pwd\\.gocache"`

## 10. 文件说明（用户关心）

- `config.json`：公开配置
- `config.local.json`：本地敏感配置（API Key）
- `prompts/`：提示词
- `emojis/`：表情包目录
- `state/`：运行状态数据
- `python/wx_bridge.py`：微信桥接脚本
