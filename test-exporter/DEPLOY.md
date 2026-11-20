# Ops Exporter Linux 部署指南

目标环境：Linux 虚拟机（k3s 集群节点同网段），Prometheus 为 Rancher Monitoring 内置实例。

## 1. 传输与安装

```bash
# Windows 侧传到 Linux（用户名/路径按实际改）
scp dist/ops-exporter-linux-amd64 user@<LINUX_IP>:/tmp/

# Linux 侧
sudo install -m 0755 /tmp/ops-exporter-linux-amd64 /usr/local/bin/ops-exporter
sudo mkdir -p /etc/ops-exporter /var/lib/ops-exporter/results
```

## 2. 配置文件 `/etc/ops-exporter/config.json`

```json
{
  "listen_addr": ":9105",
  "result_dir": "/var/lib/ops-exporter/results",
  "refresh_interval": "10s",
  "log_level": "info"
}
```

## 3. 样例任务结果（验证用，可先放一条）

```bash
cat > /var/lib/ops-exporter/results/backup.json <<'EOF'
{"job_name":"database_backup","status":"success","start_time":"2026-11-20T10:00:00+08:00","end_time":"2026-11-20T10:05:30+08:00","exit_code":0,"message":"backup completed"}
EOF
```

## 4. systemd 服务 `/etc/systemd/system/ops-exporter.service`

```ini
[Unit]
Description=Ops Exporter (task results to Prometheus metrics)
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/ops-exporter -config /etc/ops-exporter/config.json
Restart=on-failure
RestartSec=5
# 配置非法/端口占用等启动失败会 exit(1)，由 Restart 策略兜底重试
DynamicUser=yes
StateDirectory=ops-exporter

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now ops-exporter
curl -s localhost:9105/healthz   # {"status":"ok"}
curl -s localhost:9105/metrics | grep ops_
```

## 5. Rancher Monitoring 抓取配置

集群 → Monitoring → 定制化（或 rancher-monitoring 应用 values）→
**Additional Scrape Configs**，`<LINUX_IP>` 换成运行 exporter 的虚拟机 IP：

```yaml
- job_name: ops-exporter
  scrape_interval: 15s
  static_configs:
    - targets:
        - <LINUX_IP>:9105
      labels:
        env: lab
```

保存后等 1~2 分钟让 Prometheus 重新加载。

## 6. Grafana Dashboard 导入

Rancher 集群 → Monitoring → Grafana → Dashboards → **New → Import**，
上传 `grafana-dashboard.json`，数据源选 `PROMETHEUS`（Rancher 内置）。

Dashboard 包含（全部支持任务变量 `$job` 过滤、30s 自动刷新）：

| 区块 | 内容 | 异常表现 |
|---|---|---|
| 顶部统计 | 失败任务数 / 任务总数 / exporter 新鲜度 / 抓取状态 | 失败>0 红色；新鲜度>60s 红；抓取中断红 |
| 各任务状态 | 每个任务一个格子：成功(绿)/失败(红) | 失败一目了然 |
| 距上次成功 | 绿→黄(12h)→红(24h)；"从未成功"红色 | 连续失败被掩盖时此处仍会变红 |
| 距上次执行 | 绿→黄(12h)→红(24h) | 长时间未执行/停跑任务直接红 |
| 任务总览表 | 四指标按 job_name 合并巡检 | 一行一任务 |
| 趋势图 | last_run 年龄曲线 + 耗时阶梯图 | 锯齿=正常；只涨不跌=停跑 |

> 阈值（60s 新鲜度 / 12h·24h 陈旧）与 promql.md 告警规则同源，改告警阈值时同步改面板。
>
> 导入注意：数据源变量默认 `prometheus`（Rancher 内置 Prometheus 的 uid）。若你的实例 uid 不同，
> 导入时在上方"数据源"下拉重新选择一次即可，面板会自动跟随。重复导入同一 uid 会覆盖旧版本。
>
> 表格面板用了 `labelsToFields → merge → organize` 变换链（Prometheus 数据源把标签放在字段元数据里，
> 不先转列就没有 job_name 列）。若列名有出入，只需在面板 Transform 里调整 rename 规则。

## 7. 从 Windows 迁移到 Linux 时

当前 scrape 目标可能是 `192.168.193.1:9105`（Windows）。Linux 部署完成后：

1. 改 Additional Scrape Configs 的 target 为 Linux 虚拟机 IP，保存
2. 在 Prometheus → Targets 确认 `ops-exporter` 变 up
3. Dashboard 的「采集目标」格子自动恢复绿色，"任务总数/状态"即刻有数据（`instance` 标签会变成新地址，无需改面板）

## 7. 验证（Prometheus 查询接口已验证可用）

```bash
# 抓取目标已 up
curl -sk -H "Authorization: Bearer <TOKEN>" \
  "https://rancher.my.org/api/v1/namespaces/cattle-monitoring-system/services/http:rancher-monitoring-prometheus:9090/proxy/api/v1/query?query=up%7Bjob%3D%22ops-exporter%22%7D"

# 逐条验证 promql.md 中的查询（浏览器打开表达式浏览器更直观）：
# ops_job_last_status
# time() - ops_job_last_run_timestamp_seconds
# time() - ops_job_last_success_timestamp_seconds
# time() - ops_exporter_last_refresh_timestamp_seconds
# absent(ops_job_last_status{job_name="database_backup"})
```

## 备注

- 二进制为静态编译（CGO_ENABLED=0），无 libc 依赖，Alpine/容器内也可用
- Docker 方式（可选）：`FROM scratch` + COPY 二进制与 config，EXPOSE 9105
- 任务方写结果文件时建议原子写入（先写临时文件再 rename），避免 exporter 读到半截 JSON（当前会记文件级错误，不影响其他任务）
