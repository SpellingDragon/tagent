## ADDED Requirements

### Requirement: 远端声明必显式 context_window

乙门语义下（预算门开 ⟺ `context_window>0`，`replay_window` 不兼任预算窗缺省），远端部署声明 `examples/wechat-bot/tagent.yaml` MUST 显式声明 `context_window`，否则长会话失去预算保护（门闭、整包发送、无拒发）。回放窗 SHALL 以 `replay_window` 键声明（原 `max_tokens` 回放窗语义归位）；输出限按需以 `max_tokens` 键声明。

远端 yaml SHALL 经 `loadHostDocument` strict 解码（拒未知键）；旧键 `max_tokens` 作回放窗 SHALL 被拒启动。本 Requirement 只约束仓库内声明；远端部署（pull/rebuild/restart）须另行用户授权。

#### Scenario: 显式 context_window 门开

- **WHEN** 远端 yaml 声明 `replay_window:128000` + `context_window:128000`
- **THEN** strict 解码 exit 0，预算门开（context_window>0），长会话受预算保护

#### Scenario: 漏写 context_window 失保护

- **WHEN** 远端 yaml 只声明 `replay_window` 无 `context_window`
- **THEN** 预算门闭（乙语义），部署验收 SHALL 判预算保护缺失，D-04 不得勾选

#### Scenario: 旧键拒启动

- **WHEN** 远端 yaml 仍以 `max_tokens:128000` 作回放窗（未归位）
- **THEN** `loadHostDocument` SHALL 以 ErrUnknownConfigKey 拒启动（max_tokens 现为输出限键，回放窗须 replay_window）