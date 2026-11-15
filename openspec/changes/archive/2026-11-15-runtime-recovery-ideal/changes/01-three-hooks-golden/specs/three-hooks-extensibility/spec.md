## ADDED Requirements

### Requirement: 装配根无具体特性硬编码

装配根 SHALL 不认识任何特性名：对插件管道集合的成员增补 SHALL 只来自两个来源——转发已解析的插件集合，或取声明名集与注册表的解析结果。装配根 SHALL NOT 直接构造具体特性组件再挂进插件管道，SHALL NOT 以复合字面量直接挂进插件管道。展开转发已解析集合、以及把解析结果整赋值给管道集合 SHALL 视为合规形态。

「插件管道集合」SHALL 由类型为插件实例切片的容器定义，由源码推导而非登记名单：新增容器 SHALL 自动落入判据范围，推导面为空 SHALL 红并具名说明失锚，SHALL NOT 静默通过。

该不变量 SHALL 由守护门机械判定并以退出码表达，SHALL NOT 依赖评审自觉。判据 SHALL NOT 裁决「哪些名字算特性」，只裁决装配根是否按名对插件管道做了具体增补；判据 SHALL NOT 持有特性名单的第二副本。

#### Scenario: 具体特性组件被构造并挂进管道

- **WHEN** 装配根出现 `r.compress = NewCompressionConsumer(...)`（assembly/run.go:596）随后 `r.plugins = append(r.plugins, r.compress)`（assembly/run.go:615），或 `instances = append(instances, driver)`（assembly/host.go:709）
- **THEN** 守护门 SHALL 红，并逐行点名该挂载所在文件与行
- **AND** 该挂载只有改成经声明或注册表解析后才 SHALL 转绿，SHALL NOT 通过白名单放行

#### Scenario: 展开转发与整赋值保持绿

- **WHEN** 装配根以 `append(elements, r.plugins...)`（assembly/tools.go:248）转发已解析集合，或以 `r.plugins = instances`（assembly/run.go:612，`instances` 来自 :607-611 的 `cfg.plugins(r)`）取声明解析结果
- **THEN** 守护门 SHALL 绿
- **AND** 登记表 SHALL NOT 要求为这两种形态逐站点登记例外

#### Scenario: 复合字面量挂进管道

- **WHEN** 装配根把具体组件字面量直接挂进管道，如 `append(elements, observePlugin{...})`（assembly/tools.go:256-258）
- **THEN** 守护门 SHALL 红，SHALL NOT 因该组件不是声明式插件而豁免

### Requirement: 特性经插件注册表动态挂载

特性组件 SHALL 经插件注册表与声明名集动态挂载：名字 SHALL 在注册表登记，装配根 SHALL 只按名字解析出实例，SHALL NOT 把「先具体构造、再无条件挂进管道」当作挂载方式。挂载动态性 SHALL 是 golden 不变量，其文本 SHALL 登记于架构登记文件，登记缺失 SHALL 红。

登记面 SHALL 承载突破通道：确需突破本不变量时，例外 SHALL 带裁决来源、理由与复审期限登记在案；例外陈旧 SHALL 红，SHALL NOT 以放宽判据或加白名单代替登记。

#### Scenario: 动态化落地后同一门禁转绿

- **WHEN** 压缩消费与世代驱动改为经声明名集与代际注册表解析挂载
- **THEN** 同一守护门对同一批挂载点 SHALL 转绿，转绿前该门禁的所有者 SHALL 已登记例外或已改形态
- **AND** 判据 SHALL NOT 因转绿而被稀释，SHALL NOT 保留只打印不判红的通道

#### Scenario: 架构登记缺失

- **WHEN** 架构登记文件中没有本不变量的登记节，或登记的声明键与代码常量及声明面词表不一致
- **THEN** 守护门 SHALL 红并点名登记面，SHALL NOT 以脚本自带文案替代契约登记

#### Scenario: 例外条目陈旧或元数据不全

- **WHEN** 登记的例外所对应的挂载点已不存在，或例外缺裁决来源、理由、复审期限之一
- **THEN** 守护门 SHALL 红并具名点名该条目，SHALL NOT 静默接受

### Requirement: plugins 声明有生产消费方

挂载声明面 SHALL 是活的：声明键 SHALL 被声明词表接受，SHALL 在宿主文档方言中有声明的键，SHALL 映射进配置声明的对应字段，且 SHALL 存在按名逐项迭代求值的生产消费点。仅由配置包自身的编解码、克隆与指纹映射读到的字段 SHALL NOT 计为消费方；仅出现在变更提示中的引用 SHALL NOT 计为消费方。

判据 SHALL 只判「声明名集被逐项求值」这一形态，名字到注册实例的解析正确性 SHALL 由挂载能力的契约与其测试承担。

#### Scenario: 声明键被接受却无解析点

- **WHEN** 声明词表接受该键（api/config/decode.go:157-161）且解码写进配置声明字段（同文件 :190），但宿主文档方言无该键（assembly/host_document.go:91-113）、映射不写该字段（同文件 :182-221）、无按名迭代点
- **THEN** 守护门 SHALL 红并逐条点名缺失的条件
- **AND** 「声明侧的次序意图，装配不解析」这一现状 SHALL NOT 因文档自认而降级为可接受

#### Scenario: 日志型引用不算消费方

- **WHEN** 声明字段仅出现在变更提示中，如 `reflect.DeepEqual(before.Plugins, after.Plugins)`（assembly/host_reload.go:717），或仅出现在配置包自身的编解码与编译期断言中
- **THEN** 该引用 SHALL NOT 满足消费条件，守护门 SHALL 保持红

#### Scenario: 按名迭代成立即绿

- **WHEN** 生产源码存在对声明名集的逐项迭代求值，并把名字解析为插件实例
- **THEN** 消费条件 SHALL 满足，该条 SHALL 转绿

### Requirement: 守护门硬编码即红

本条不变量 SHALL 由独立守护规则承载，SHALL 接入仓库的统一 lint 入口与提交前链路，经其覆盖持续集成。守卫 SHALL 附正反样例自测：每条规则 SHALL 至少一条正例与一条负例，规则退化为只打印不判红时自测 SHALL 红。守卫 SHALL NOT 提供跳过开关，SHALL NOT 提供自动写基线的通道——新增挂载点必须走「改形态」或「登记例外」这一可见动作。

门禁在现树红 SHALL 被当作机器证词而非障碍：红集 SHALL 逐行可点名，其恰好等于判据枚举的挂载点，SHALL NOT 因红而放宽判据或预先登记全量例外。

#### Scenario: 规则退化为只打印

- **WHEN** 某条规则的负例不再以非零退出码拒绝
- **THEN** 自测 SHALL 红并点名该规则与用例名

#### Scenario: 现树红集等于枚举

- **WHEN** 在判据落地后对现树运行门禁
- **THEN** 退出码 SHALL 为非零，且点名 assembly/run.go:615、assembly/run.go:618、assembly/tools.go:250、assembly/tools.go:253、assembly/tools.go:256、assembly/host.go:709、assembly/host.go:728 七处与声明面三项缺失
- **AND** 该红集 SHALL 作为交接清单交给挂载能力修复，不得在本能力内自行修复特性代码

#### Scenario: 判据被要求为绿而让路

- **WHEN** 出现「先登记全部现树红点为例外以求 lint 绿」的诉求
- **THEN** 该做法 SHALL 被拒绝：例外只接受带裁决来源、理由与复审期限的突破条目
