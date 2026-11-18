# k8s-inspector

Kubernetes 集群资源巡检命令行工具：只读采集节点与 Pod 状态，输出终端文本报告或机器可读的 JSON 报告。个人云原生运维工具，用于学习 Go 工程化、client-go 和 Kubernetes API。

## 功能

- 获取集群节点列表，识别节点 Ready / NotReady 及原因
- 获取 Pod 列表（全部 Namespace 或指定 Namespace）
- 异常 Pod 判断（分层，不把所有非 Running 都当故障）：
  - Phase 层：`Pending` / `Failed` / `Unknown`
  - 容器 Waiting 层：`CrashLoopBackOff` / `ImagePullBackOff` / `ErrImagePull` / `Pending`（大小写不敏感）
  - 容器 Terminated 层：`OOMKilled`（即使容器已被重启回 Running 也会记录）
- 统计 Pod 重启次数（主容器 + Init 容器累计）
- 过滤：指定 Namespace、指定节点、重启次数阈值、终端只显示异常
- 输出：终端文本报告（`text`）或 JSON（`json`）
- 正常资源与异常资源分开呈现，无异常时明确显示"巡检正常"

**所有 Kubernetes 操作均为只读**，不会修改、删除任何集群资源。

## 目录结构

```
k8s-inspector/
├── cmd/k8s-inspector/main.go   # 入口：启动与退出码
├── internal/
│   ├── app/      # 流程编排：参数 -> 配置 -> 客户端 -> 采集 -> 报告
│   ├── config/   # 命令行参数解析与 kubeconfig 加载
│   ├── kube/     # 创建 Kubernetes 客户端（返回接口类型）
│   ├── collect/  # 只读采集与异常判断（ListNodes / ListPods / Inspect*）
│   └── report/   # 报告统计与渲染（text / JSON）
├── go.mod / go.sum
```

## 运行

kubeconfig 查找顺序：`-kubeconfig` 参数 → `KUBECONFIG` 环境变量 → `~/.kube/config`。
K3s 用户常见用法（kubeconfig 默认在 `/etc/rancher/k3s/k3s.yaml`，且仅 root 可读）：

```bash
sudo k8s-inspector -kubeconfig /etc/rancher/k3s/k3s.yaml
```

### 全部参数

| 参数 | 默认值 | 说明 |
|---|---|---|
| `-kubeconfig` | 空 | kubeconfig 文件路径；留空按默认查找规则 |
| `-namespace` | 空 | 只巡检指定 Namespace 的 Pod；留空为全部 Namespace |
| `-node` | 空 | 只巡检指定名称的节点；节点不存在时报错 |
| `-restart-threshold` | 0 | 重启次数阈值；Pod 累计重启达到该值视为异常，0 不启用 |
| `-abnormal-only` | false | 终端报告只显示异常 Pod（JSON 输出始终全量） |
| `-output` | `text` | 输出格式：`text` 或 `json` |
| `-timeout` | 10s | 访问 apiserver 的超时时间 |

``
## 测试

单元测试全部使用 Fake Client，不需要真实集群、kubeconfig 或网络：

```bash
go test ./...
go test ./... -v      # 查看全部用例
```

覆盖内容：节点 Ready 判断、Pod 异常分层判断、重启统计、阈值过滤、节点过滤、报告统计与文本/JSON 渲染、参数解析校验、采集调用链（含错误注入）。

## 已知取舍与不支持的功能

- **Running 但就绪探针未通过的 Pod 不计入异常**（READY 列可见 0/1，是否单列"就绪异常"后续考虑加入）
- 容器 `Terminated` 只报 `OOMKilled`；`Evicted` / `DeadlineExceeded` 等暂不覆盖
- 只做节点与 Pod 的只读巡检，暂不支持：Controller / CRD / 自动修复 / 资源写操作 / Rancher API / 多集群管理 / Web UI / 数据库存储 / 历史数据对比。考虑后续加入。
- 无 CLI 子命令框架（标准库 flag）；参数增多时再考虑引入
