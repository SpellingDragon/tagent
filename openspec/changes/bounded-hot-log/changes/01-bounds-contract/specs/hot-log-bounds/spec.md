## ADDED Requirements

### Requirement: 增长构成须先实测再定界

任何有界机制 SHALL 基于实测的增长构成（按 journal 帧内键族的字节占比：事件/window 元/原文族/引用族/`checkpoint:` 切片/`quiet:` 标记/派生索引/节奏记录）设计；正文两处冗余 SHALL 先单一化再谈搬运。判据中的每个量 SHALL 有可复跑测量定义，MUST NOT 用单点读数或未归因残差充当界。

#### Scenario: 正文双份

- **WHEN** 实测发现同一正文存于帧与原文族两处
- **THEN** 计划 SHALL 先立单一化项
- **AND** 归档设计 MUST NOT 把双份带进去

### Requirement: 平台判据与归档声明分离

有界验收 SHALL 以跨代平台判据为准（N≥3 代、满窗后 `hot_bytes` 落带、分母＝窗口内提交量）；热字节构成 SHALL 按“恒热集合（最新 cut 段＋开段）＋配置保留旧段”记录，`hot_over_budget` 计数与最长超界时长 SHALL 随代记录；正常负载（无故障注入）下 `hot_over_budget` SHALL 为零，非零 SHALL 使平台锚失败。开档完整性校验字节 SHALL 有界于热窗（不随归档增长）；归档段 SHALL 读时校验，MUST NOT 由 boot 预扫归档卷。归档卷增长与跨档读延迟 SHALL 单列声明。MUST NOT 以归档增长替代热卷有界，也 MUST NOT 以单代数字宣称平台，MUST NOT 以调低 cut 频率凑平台。

#### Scenario: 平台未达

- **WHEN** 满窗后某代热字节越过平台带
- **THEN** 判据 SHALL 失败
- **AND** 报告 SHALL 指出增长来自窗口外滞留还是窗口参数

### Requirement: 归档定位契约以 manifest 为唯一权威

归档 SHALL 以 manifest（双文件 generation swap）为“哪些段在归档”的唯一权威，段 SHALL 自描述键→偏移索引。定位读序 SHALL 固定为热索引→manifest→段索引→段。失败语义 SHALL 区分：未陈述=`ErrFactAbsent`；陈述而缺失=`ErrArchiveMissing`；校验失败=`ErrArchiveCorrupt`。

#### Scenario: 三态区分

- **WHEN** 按身份读一个键
- **THEN** 三态 SHALL 各按上述错误/结果返回
- **AND** 任一态 MUST NOT 被折叠为“不存在”

### Requirement: 配置门与回退零差异

归档开启 SHALL 只由受校验声明字段控制（`archive_root`、`hot_log_max_bytes`、可选 `archive_max_age`）；未声明 `archive_root` 时行为 SHALL 与现状一致。`hot_log_max_bytes` 低于恒热集合（最新 cut 段＋开段）的结构下限时 SHALL 以下限为准并计入 `hot_over_budget` 观测，MUST NOT 因此改变切割时点。关闭归档或回退构建后，已归档段 SHALL 仍按身份可读。

#### Scenario: 配置小于结构下限

- **WHEN** `hot_log_max_bytes` 小于新一代的恒热集合字节
- **THEN** 最新 cut 段与开段 SHALL 仍不被搬迁
- **AND** `hot_over_budget` SHALL 记录超界

#### Scenario: 未声明归档

- **WHEN** 部署没有 `archive_root`
- **THEN** 轮转与搬迁 SHALL NOT 发生
- **AND** 字节行为与今日一致
