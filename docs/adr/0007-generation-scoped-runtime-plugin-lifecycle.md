# 代际作用域的运行期插件生命周期扩展 0004 的装配期注册表

**Status**: accepted（2026-11-08 用户裁定，hot-update-and-purity-closure Q6）

0004 把特性挂载定为装配期解析的自由注册插件清单，并明确不引入运行期动态装卸；但热更新要求 Boot 一次消费者（exec/storage/governance/telemetry/宿主工具）能在运行中按代替换，装配期注册表不足以承载。定案：把注册表扩展为代际作用域的运行期生命周期——每个新代际绑定经通用注册表重建其消费者实例并登记独占资源，复用整代切换/借还 drain/退役台账；装配根仍只认通用注册表不认特性名（0006 R2 保持绿）。

- **Considered Options**: 每特性 typed per-gen builder（否：犯 0006 R2，须逐特性突破协议留 N 条例外与 N 条热换路径）；挂点感知事件自释放（否：每 hook 背代际状态机，与退役台账重复）。
- **Consequences**: 反转 0004"装配期-only"scope，须更新其 r15 三道钉登记；Boot 一次消费者须先插件化归位（冥想已归位为范式）；注册表须三分资源（进程私有每代随代生灭／外部耐久跨代重探不重建／共享 natives 复用不重建）并承担排序与 introspection；有状态消费者（meditation/governance）状态须链派生或持久以交接。