# 保留的未通过尝试

- shared-git-objects-23574367：共享克隆的 Git 对象库在 Docker 挂载内不可读取，构建返回1；未规避 VCS stamping。
- superseded-23574367：补修保护带时间校验后，旧提交的组件测试被主动停止，结果137。
- superseded-0c18aab8：补修第二次 Agent 重启的未知用量重放后，旧提交的组件测试被主动停止，结果137。

各目录保留原始退出码和 acceptance_complete=false。它们都不是最终提交的通过证据。
最终实现提交995d7fca的完整producer见同级完整SHA目录；runtime_result=0，契约owner批准仍待完成。
