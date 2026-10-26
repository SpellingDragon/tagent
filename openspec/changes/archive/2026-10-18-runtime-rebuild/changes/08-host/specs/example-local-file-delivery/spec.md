# example-local-file-delivery delta（08-host）

> 本 delta 只承载**新增条款**；既有 Requirement（`ExtractFilePaths` 六规则、按扩展名选发送接口、投递文件并保留原文、单文件失败不阻断、`workspace_dir` 配置、`FileSender` 接口与 mock 可测、prompt 资源引导）语义不变，**继承**（见 `../proposal.md` §继承）。

## ADDED Requirements

### Requirement: 媒体发送与大结果副本复用既有外置生命周期

wechat-bot 投递层 SHALL 复用 workspace 唯一副本与外置生命周期（先持久原文、副本可清理、原文有效时可重物化）投递大结果，MUST NOT 自建第二套副本/落盘/清理机制；可执行文件（权限位或拒绝扩展名）MUST NOT 被投递（硬规则，不可配置）。

单文件发送失败 SHALL 仅记录并继续，不阻断原始文本与其余文件；被拒/失败 SHALL 以具名回执可见，宿主 MUST NOT 静默丢弃。

#### Scenario: 大结果走外置副本

- **WHEN** agent 产出超出内联阈值的本地文件路径并投递
- **THEN** 原文先持久化、投递副本可清理、原文有效时可重物化；副本生命周期由 workspace 唯一机制管理（无第二套副本路径）

#### Scenario: 可执行文件不被投递

- **WHEN** 回复文本引用存在的 `/tmp/script.sh` 或带可执行权限位的文件
- **THEN** 该文件不被投递（扩展名拒绝表或权限位检查命中，规则不可配置）

#### Scenario: 失败不静默

- **WHEN** 某文件 `Send*FromPath` 返回错误
- **THEN** 错误被记录且以具名回执可见；原始文本与其余文件照常发送
