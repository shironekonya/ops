# Ops Exporter 告警查询集（PromQL）

对应 TODO 7。第一版不在 exporter 内实现告警引擎，
以下查询供 Prometheus 告警规则（`alerting rules`）和 Grafana Dashboard（TODO 12）直接使用。

## 1. 任务超时未执行（核心查询）

```promql
time() - ops_job_last_run_timestamp_seconds > 86400
```

- `ops_job_last_run_timestamp_seconds` 是任务最近一次**结束**时间的 Unix 秒数
- 阈值按任务周期调整：日备任务用 86400（1 天），小时级任务改 7200 等
- 配合 `for: 10m` 防抖，避免刷新周期内的瞬时波动触发告警

**为什么任务文件消失后依然有效**：exporter 对"曾经出现过、后来文件没了"的任务，
保留序列最后一次的值不清理（`TestStaleSeriesPersistAfterFileRemoval` 锁定了该行为）。
文件停止更新后，`time() - last_run` 会随时间持续增长，告警照常触发。

## 2. exporter 自身失效（前置保障）

```promql
time() - ops_exporter_last_refresh_timestamp_seconds > 60
```

- 若 exporter 卡死、任务目录持续读不了，所有第 1 类查询会同时失真
- 该指标只在整轮刷新成功时更新，阈值取刷新间隔（默认 10s）的数倍即可
- **阈值应与配置联动**：配置文件里 `refresh_interval` 改成多少，本条阈值就应设为
  它的 3~6 倍（如 10s 间隔配 60s 阈值，30s 间隔配 180s）。写在告警规则注释里，
  改配置时同步检查
- 建议优先级高于任何单任务告警：它失效意味着所有任务数据都不可信

## 3. 任务执行失败

```promql
ops_job_last_status == 0
```

- 1 = 最近一次成功，0 = 最近一次失败
- 注意这是"最近一次"的状态：连续失败不会累积，需要趋势时用 `avg_over_time` 补充

## 4. 任务从未成功过

```promql
ops_job_last_success_timestamp_seconds == 0
```

- exporter 明确约定：从未成功过的任务该值为 0（失败不会覆盖它，也不会留空）
- `0` 是合法的告警条件，不是缺数据

## 5. 任务从 exporter 中消失 / 从未执行过

任务如果**从未**产生过结果文件，exporter 一无所知，不会为它伪造任何序列
（`TestUnknownJobNeverFaked` 锁定了该行为）。这类任务必须用 `absent()` 盯住：

```promql
absent(ops_job_last_run_timestamp_seconds{job_name="database_backup"})
```

- 每个关键任务写一条，任务清单来自运维配置，而不是 exporter 自动发现
- 与第 1 条互补：第 1 条管"跑过但停了"，本条管"根本没跑过"

## 6. 组合示例：完整告警规则片段

```yaml
groups:
  - name: ops-exporter
    rules:
      - alert: OpsJobStale
        expr: time() - ops_job_last_run_timestamp_seconds > 86400
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "任务 {{ $labels.job_name }} 超过 24 小时未执行"

      - alert: OpsJobFailed
        expr: ops_job_last_status == 0
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "任务 {{ $labels.job_name }} 最近一次执行失败"

      - alert: OpsJobNeverSucceeded
        expr: ops_job_last_success_timestamp_seconds == 0
        for: 30m
        labels:
          severity: info
        annotations:
          summary: "任务 {{ $labels.job_name }} 从未成功过"

      - alert: OpsExporterStale
        expr: time() - ops_exporter_last_refresh_timestamp_seconds > 60
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Ops Exporter 超过 60 秒未成功刷新任务结果"
```
