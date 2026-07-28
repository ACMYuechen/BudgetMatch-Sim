# 开发改动日志（dev changelog）

> 用途：每次完成一段编码改动后，把“改了什么、为什么、影响面、怎么验证的”续写到本文档末尾，供组内同步与回溯。
> 约定：按时间顺序追加在文末，一次改动一节；标题格式 `YYYY-MM-DD 改动主题（分支名）`。
> 粒度：写到“能让没跟进这段工作的同事看懂”的程度即可，行级细节留给 git diff。

## 2026-07-24 Agent 文件工具工作区隔离与安全限制（main）

### 改了什么

- 新增 `services/rpc/agent/internal/filetools` 包，把 LLM 文件工具的配置、路径校验和实际文件读写从 Agent 编排代码中独立出来。
- 新增 `FileTools` 配置并接入 agent-rpc 依赖注入链路：
  - `Workspace`：文件工具工作目录，默认 `workspace/agent`；
  - `MaxReadBytes`：单文件最大读取大小，默认 `1048576` 字节（1 MiB）；
  - `WritableExtensions`：允许写入的文件后缀，默认 `.json`、`.md`、`.txt`。
- `read_file` / `write_file` 不再把模型传入的路径直接交给 `os.ReadFile` / `os.WriteFile`，而是统一通过受限 `Workspace` 执行。
- 增加路径安全校验：
  - 只接受相对于 Agent 工作目录的路径；
  - 拒绝空路径、绝对路径、Windows 盘符路径和包含 `..` 路径段的输入；
  - 同时识别 `/` 与 `\` 路径分隔符，避免跨平台路径绕过；
  - 解析文件和父目录软链，最终路径逃出工作目录时拒绝访问；
  - 写入子目录时逐级检查目录类型和软链目标，缺失的工作目录内子目录按需创建。
- `read_file` 仅允许读取普通文件，通过文件元数据和限长读取双重检查 `MaxReadBytes`，避免一次性读入超大文件。
- `write_file` 在写入前检查文件后缀，不在允许列表中的文件类型直接拒绝；返回值中的写入字节字段统一为 `BytesWritten`。
- 更新 LLM system prompt 和工具 schema 描述，明确工具参数必须是工作目录内的相对路径，不能携带 `workspace/agent` 前缀、绝对路径或 `..`；推荐文件约定调整为 `preferences.json` 和 `recommendations/latest.md`。
- 新增文件工具单元测试，并同步更新 LLM Agent 测试中的构造参数和工具数量断言（商品工具 2 个 + 文件工具 2 个）。

### 为什么

- 7 月 18 日加入的 `read_file` / `write_file` 直接信任 LLM 生成的路径，模型一旦生成绝对路径或目录穿越路径，就可能读取 `.env`、服务配置等工作目录外文件，或覆盖进程权限范围内的任意文件。
- 只做字符串前缀判断无法防住 `..`、Windows 路径和软链跳转，因此需要在统一文件工具层同时进行词法路径校验与软链解析后的真实路径校验。
- 文件读取需要明确资源上限，避免模型请求大文件造成不必要的内存占用；文件写入限制为业务所需的文本和结构化文件类型，可缩小误写可执行文件或服务源码的风险。
- 将安全边界封装在独立 `filetools.Workspace` 中，能保证所有 LLM 文件调用复用同一套规则，避免安全判断散落在 Eino 工具 handler 中。

### 影响面

- 仅影响 agent-rpc 的 LLM 推荐链路中 `read_file` / `write_file` 两个工具；`search_products`、`select_bundle`、确定性规则推荐和其他 RPC 服务不受影响。
- 文件路径行为有收紧：
  - 以前可传任意进程可访问路径，现在只能传相对于 `FileTools.Workspace` 的路径；
  - 例如工作目录为 `workspace/agent` 时，模型应传 `recommendations/demo.md`，不能传 `workspace/agent/recommendations/demo.md`；
  - 以前可以写任意后缀，现在默认只能写 `.json`、`.md`、`.txt`。
- 工作目录下的子目录会在首次写入时按需创建；读取不存在的工作目录或文件会返回明确错误。
- 默认配置可直接启动，不依赖额外外部服务；部署环境如需调整目录、读取上限或可写类型，可通过 agent-rpc 的 `FileTools` 配置覆盖。

### 怎么验证的

- 文件工具正常路径测试通过：向 `recommendations/demo.md` 写入内容后可成功回读，写入字节数正确。
- 拒绝路径测试通过：
  - 读取和写入 `../../.env` 均被拒绝；
  - `../outside.txt`、`nested/../outside.txt`、绝对路径和 Windows 盘符路径均被拒绝；
  - 非允许后缀写入被拒绝；
  - 指向工作目录外的目录软链无法用于读取或写入，且工作目录外没有产生文件。
- 超大文件测试通过：文件超过 `MaxReadBytes` 时返回包含 `maximum read size` 的明确错误。
- 执行 `go test -v ./services/rpc/agent/internal/filetools -run TestWorkspace`，全部文件工具场景通过。
- 执行 `go test ./services/rpc/agent/...`，agent-rpc 全量测试通过。

## 2026-07-24 Planner 意图解析扩展（feat/agent/hfs）

### 改了什么

- 扩展预算解析，支持“以内”“左右”“不超过”和 `3-5k` 等中英文、混合表达，区间预算取上限。
- 补充手机、耳机、平板、显示器、键鼠、宿舍、通勤等关键词，以及续航、轻薄、性能、品牌、耐用、静音等偏好。
- 增加数量和型号数字保护，避免把商品数量、`iPhone 15`、`4K`、`65W` 等误识别为预算；未识别预算时仍默认使用 3000 元。
- 新增 `planner_test.go`，覆盖中文、英文、混合表达和误识别场景。

### 为什么

- 原有规则覆盖有限，容易漏掉常见预算表达、商品类别和用户偏好，也可能把数量或型号数字当成预算。

### 影响面

- 仅影响 agent-rpc 的确定性 Planner 意图解析，不改变接口结构和默认预算逻辑。

### 怎么验证的

- 指定场景“预算 3-5k 买通勤耳机”“不超过 5000 买办公电脑”“买 iPhone 15 配件”测试通过。
- 执行 `go test ./services/rpc/agent/...`，agent-rpc 全量测试通过。
